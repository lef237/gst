package gitstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Options struct {
	LogLimit int
	GraphAll bool
}

type State struct {
	RepoRoot string
	Head     string
	Branch   string
	Detached bool
	Upstream string
	Ahead    int
	Behind   int

	Files        []File
	Counts       Counts
	Diff         []string
	StagedDiff   []string
	WorktreeDiff []string
	HeadDiff     []string
	Graph        []string
	Refs         []Ref
	Tags         []Tag
	Remotes      []Remote
	Stashes      []Stash
	Warnings     []string
	Operation    Operation
}

type Counts struct {
	Staged     int
	Modified   int
	Untracked  int
	Conflicted int
}

type File struct {
	Status string
	Path   string
	Kind   string
}

type Ref struct {
	Name     string
	Hash     string
	Age      string
	Upstream string
	Remote   bool
	Current  bool
}

type Tag struct {
	Name      string
	Hash      string
	Age       string
	Subject   string
	Annotated bool
}

type Remote struct {
	Name string
	URL  string
	Kind string
}

type Stash struct {
	Name    string
	Age     string
	Message string
}

type Operation struct {
	Kind            string
	InProgress      bool
	ContinueCommand string
	AbortCommand    string
	SkipCommand     string
}

func Collect(ctx context.Context, dir string, opts Options) (State, error) {
	if opts.LogLimit <= 0 {
		opts.LogLimit = 18
	}

	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return State{}, errors.New("not a git repository")
	}
	root = strings.TrimSpace(root)

	state := State{RepoRoot: root}

	// Each git invocation spawns a subprocess, so the collection is dominated
	// by process startup latency rather than CPU. Run the independent queries
	// concurrently and parse their output after they all finish. Status and
	// refs are only captured here because parseRefs needs the branch name that
	// parseStatus extracts, so their parsing is serialized afterward.
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		statusOut string
		statusOK  bool
		refsOut   string
		refsOK    bool
	)
	warn := func(err error) {
		mu.Lock()
		state.Warnings = append(state.Warnings, err.Error())
		mu.Unlock()
	}
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	run(func() {
		if out, err := git(ctx, root, "status", "--porcelain=v2", "--branch", "-z"); err == nil {
			statusOut, statusOK = out, true
		} else {
			warn(err)
		}
	})
	run(func() {
		if out, err := collectGraph(ctx, root, opts.LogLimit, opts.GraphAll); err == nil {
			state.Graph = nonEmptyLines(out)
		} else {
			warn(err)
		}
	})
	run(func() {
		if staged, worktree, head, err := collectDiffs(ctx, root); err == nil {
			state.StagedDiff = diffLines(staged)
			state.WorktreeDiff = diffLines(worktree)
			state.HeadDiff = diffLines(head)
			state.Diff = combineDiffs(state.StagedDiff, state.WorktreeDiff)
		} else {
			warn(err)
		}
	})
	run(func() {
		if out, err := git(ctx, root, "for-each-ref", "refs/heads", "refs/remotes", "--format=%(refname)%09%(refname:short)%09%(objectname:short)%09%(committerdate:relative)%09%(upstream:short)"); err == nil {
			refsOut, refsOK = out, true
		} else {
			warn(err)
		}
	})
	run(func() {
		if out, err := git(ctx, root, "for-each-ref", "refs/tags", "--sort=-creatordate", "--format=%(refname:short)%09%(objectname:short)%09%(*objectname:short)%09%(objecttype)%09%(creatordate:relative)%09%(subject)"); err == nil {
			state.Tags = parseTags(out)
		} else {
			warn(err)
		}
	})
	run(func() {
		if out, err := git(ctx, root, "remote", "-v"); err == nil {
			state.Remotes = parseRemotes(out)
		} else {
			warn(err)
		}
	})
	run(func() {
		if out, err := git(ctx, root, "stash", "list", "--date=relative", "--pretty=format:%gd|%cr|%s"); err == nil {
			state.Stashes = parseStashes(out)
		} else {
			warn(err)
		}
	})
	run(func() {
		if op, err := detectOperation(ctx, root); err == nil {
			state.Operation = op
		} else {
			warn(err)
		}
	})

	wg.Wait()

	if statusOK {
		parseStatus(statusOut, &state)
	}
	if refsOK {
		state.Refs = parseRefs(refsOut, state.Branch)
	}

	return state, nil
}

func collectGraph(ctx context.Context, root string, limit int, all bool) (string, error) {
	hasHead, err := hasHeadCommit(ctx, root)
	if err != nil {
		return "", err
	}
	if !hasHead {
		return "", nil
	}

	if all {
		return git(ctx, root, "log", "--graph", "--decorate", "--oneline", "--all", "--date-order", "-n", strconv.Itoa(limit))
	}
	out, err := git(ctx, root, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return "", err
	}
	refs := append([]string{"HEAD"}, nonEmptyLines(out)...)
	args := []string{"log", "--graph", "--decorate", "--oneline", "--date-order", "-n", strconv.Itoa(limit)}
	args = append(args, refs...)
	return git(ctx, root, args...)
}

// hasHeadCommit reports whether the repository has a commit at HEAD. --quiet
// turns an unresolvable HEAD into a silent exit 1 instead of a "Needed a single
// revision" fatal, so the answer never depends on git's message catalog: that
// string is translated, and matching it would make a repository without commits
// look like a hard failure under any locale git ships a translation for. Only
// exit 1 counts as "no commit" -- a missing repository or a broken object store
// exits 128 and still surfaces as an error.
func hasHeadCommit(ctx context.Context, root string) (bool, error) {
	out, err := gitWithAllowedExitCodes(ctx, root, []int{1}, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// diffFlags pin the shape of every diff gst collects so the result stays a
// patch git apply accepts. The prefixes and color are fixed because a user's
// diff.noprefix, diff.mnemonicPrefix or color.ui=always would otherwise
// reshape the headers, and --no-textconv stops a textconv driver from
// substituting converted text for the file's real content (--no-ext-diff alone
// does not cover it). --ignore-submodules=dirty suppresses the "-dirty" suffix
// git appends to a submodule's commit when the submodule has uncommitted work,
// which is not a valid object id: git apply --check accepts such a section and
// then applying it only warns and leaves the gitlink untouched. --binary is
// deliberately absent: binary files are dropped by stripUnappliableSections
// rather than embedded, and so are the submodule sections that survive the
// flag.
var diffFlags = []string{
	"--no-ext-diff",
	"--no-textconv",
	"--no-color",
	"--ignore-submodules=dirty",
	"--src-prefix=a/",
	"--dst-prefix=b/",
	"--unified=3",
}

func diffArgs(args ...string) []string {
	return append(append([]string{}, args...), diffFlags...)
}

func collectDiffs(ctx context.Context, root string) (string, string, string, error) {
	cached, cachedDropped, err := collectDiff(ctx, root, "diff", "--cached")
	if err != nil {
		return "", "", "", err
	}
	tracked, trackedDropped, err := collectDiff(ctx, root, "diff")
	if err != nil {
		return "", "", "", err
	}
	untracked, untrackedDropped, err := collectUntrackedDiff(ctx, root)
	if err != nil {
		return "", "", "", err
	}
	staged := noteOmitted(joinRawDiffs(cached), cachedDropped)
	// Untracked files are unstaged working-tree changes, so surface their
	// contents alongside the tracked worktree diff. The worktree view and the
	// y/worktree clipboard copy share this value, keeping them consistent.
	worktree := noteOmitted(joinRawDiffs(tracked, untracked), trackedDropped, untrackedDropped)
	hasHead, err := hasHeadCommit(ctx, root)
	if err != nil {
		return "", "", "", err
	}
	if !hasHead {
		head := noteOmitted(joinRawDiffs(cached, tracked, untracked), cachedDropped, trackedDropped, untrackedDropped)
		return staged, worktree, head, nil
	}
	trackedHead, headDropped, err := collectDiff(ctx, root, "diff", "HEAD")
	if err != nil {
		return "", "", "", err
	}
	head := noteOmitted(joinRawDiffs(trackedHead, untracked), headDropped, untrackedDropped)
	return staged, worktree, head, nil
}

func collectDiff(ctx context.Context, root string, args ...string) (string, omitted, error) {
	out, err := git(ctx, root, diffArgs(args...)...)
	if err != nil {
		return "", omitted{}, err
	}
	text, dropped := stripUnappliableSections(out)
	return text, dropped, nil
}

func collectUntrackedDiff(ctx context.Context, root string) (string, omitted, error) {
	out, err := git(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", omitted{}, err
	}

	var diffs []string
	var dropped omitted
	for _, path := range nulSeparated(out) {
		args := append(diffArgs("diff", "--no-index"), "--", "/dev/null", path)
		diff, err := gitWithAllowedExitCodes(ctx, root, []int{1}, args...)
		if err != nil {
			return "", omitted{}, err
		}
		text, sectionDropped := stripUnappliableSections(diff)
		diffs = append(diffs, text)
		dropped = mergeOmitted(dropped, sectionDropped)
	}
	return joinRawDiffs(diffs...), dropped, nil
}

func NativeStatus(ctx context.Context, dir string, color bool) (string, error) {
	colorMode := "never"
	if color {
		colorMode = "always"
	}
	return git(ctx, dir, "-c", "color.status="+colorMode, "status")
}

func parseStatus(out string, state *State) {
	if strings.ContainsRune(out, '\x00') {
		parseStatusZ(out, state)
		return
	}

	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			parseHeader(line, state)
			continue
		}
		switch line[0] {
		case '1', '2':
			parseTracked(line, "", state)
		case 'u':
			parseUnmerged(line, state)
		case '?':
			path := unquotePath(strings.TrimPrefix(line, "? "))
			state.Files = append(state.Files, File{Status: "??", Path: path, Kind: "untracked"})
			state.Counts.Untracked++
		}
	}
}

func parseStatusZ(out string, state *State) {
	records := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "# ") {
			parseHeader(record, state)
			continue
		}
		switch record[0] {
		case '1':
			parseTracked(record, "", state)
		case '2':
			origPath := ""
			if i+1 < len(records) {
				origPath = records[i+1]
				i++
			}
			parseTracked(record, origPath, state)
		case 'u':
			parseUnmerged(record, state)
		case '?':
			path := strings.TrimPrefix(record, "? ")
			state.Files = append(state.Files, File{Status: "??", Path: path, Kind: "untracked"})
			state.Counts.Untracked++
		}
	}
}

func parseHeader(line string, state *State) {
	switch {
	case strings.HasPrefix(line, "# branch.oid "):
		oid := strings.TrimPrefix(line, "# branch.oid ")
		if oid != "(initial)" {
			state.Head = shortHash(oid)
		}
	case strings.HasPrefix(line, "# branch.head "):
		head := strings.TrimPrefix(line, "# branch.head ")
		if head == "(detached)" {
			state.Detached = true
			state.Branch = "DETACHED"
		} else {
			state.Branch = head
		}
	case strings.HasPrefix(line, "# branch.upstream "):
		state.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
	case strings.HasPrefix(line, "# branch.ab "):
		for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
			if strings.HasPrefix(f, "+") {
				state.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f, "+"))
			}
			if strings.HasPrefix(f, "-") {
				state.Behind, _ = strconv.Atoi(strings.TrimPrefix(f, "-"))
			}
		}
	}
}

func parseTracked(line, zOrigPath string, state *State) {
	parts := strings.SplitN(line, " ", 9)
	if len(parts) < 9 {
		return
	}
	xy := parts[1]
	if len(xy) != 2 {
		return
	}
	path := parts[8]
	if line[0] == '2' {
		renameParts := strings.SplitN(line, " ", 10)
		if len(renameParts) < 10 {
			return
		}
		path, zOrigPath = parseRenamePath(renameParts[9], zOrigPath)
	}
	path = unquotePath(path)
	zOrigPath = unquotePath(zOrigPath)
	if line[0] == '2' && zOrigPath != "" {
		path = zOrigPath + " -> " + path
	}

	kind := classifyXY(xy)
	state.Files = append(state.Files, File{Status: xy, Path: path, Kind: kind})
	if xy[0] != '.' {
		state.Counts.Staged++
	}
	if xy[1] != '.' {
		state.Counts.Modified++
	}
}

func parseUnmerged(line string, state *State) {
	parts := strings.SplitN(line, " ", 11)
	if len(parts) < 11 {
		return
	}
	state.Files = append(state.Files, File{Status: parts[1], Path: unquotePath(parts[10]), Kind: "conflict"})
	state.Counts.Conflicted++
}

func parseRenamePath(pathField, zOrigPath string) (string, string) {
	if zOrigPath != "" {
		return pathField, zOrigPath
	}
	path, origPath, found := strings.Cut(pathField, "\t")
	if !found {
		return pathField, ""
	}
	return path, origPath
}

func unquotePath(path string) string {
	if len(path) < 2 || path[0] != '"' {
		return path
	}
	unquoted, err := strconv.Unquote(path)
	if err != nil {
		return path
	}
	return unquoted
}

func classifyXY(xy string) string {
	if len(xy) != 2 {
		return "changed"
	}
	if strings.ContainsAny(xy, "U") {
		return "conflict"
	}
	if xy[0] != '.' && xy[1] != '.' {
		return "staged+worktree"
	}
	if xy[0] != '.' {
		return "staged"
	}
	if xy[1] != '.' {
		return "worktree"
	}
	return "clean"
}

func parseRefs(out, current string) []Ref {
	var refs []Ref
	for _, line := range nonEmptyLines(out) {
		parts := strings.SplitN(line, "\t", 5)
		for len(parts) < 5 {
			parts = append(parts, "")
		}
		fullName := parts[0]
		name := parts[1]
		hash := parts[2]
		age := parts[3]
		upstream := parts[4]
		remote := strings.HasPrefix(fullName, "refs/remotes/")
		if remote && strings.HasSuffix(fullName, "/HEAD") {
			continue
		}
		refs = append(refs, Ref{
			Name:     name,
			Hash:     hash,
			Age:      age,
			Upstream: upstream,
			Remote:   remote,
			Current:  !remote && name == current,
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Current != refs[j].Current {
			return refs[i].Current
		}
		if refs[i].Remote != refs[j].Remote {
			return !refs[i].Remote
		}
		return refs[i].Name < refs[j].Name
	})
	return refs
}

func parseTags(out string) []Tag {
	var tags []Tag
	for _, line := range nonEmptyLines(out) {
		parts := strings.SplitN(line, "\t", 6)
		for len(parts) < 6 {
			parts = append(parts, "")
		}
		name := parts[0]
		hash := parts[1]
		peeledHash := parts[2]
		objectType := parts[3]
		age := parts[4]
		subject := parts[5]
		annotated := objectType == "tag"
		if peeledHash != "" {
			hash = peeledHash
		}
		tags = append(tags, Tag{
			Name:      name,
			Hash:      hash,
			Age:       age,
			Subject:   subject,
			Annotated: annotated,
		})
	}
	return tags
}

func parseRemotes(out string) []Remote {
	seen := map[string]bool{}
	var remotes []Remote
	for _, line := range nonEmptyLines(out) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		key := fields[0] + "|" + fields[1] + "|" + strings.Trim(fields[2], "()")
		if seen[key] {
			continue
		}
		seen[key] = true
		remotes = append(remotes, Remote{Name: fields[0], URL: fields[1], Kind: strings.Trim(fields[2], "()")})
	}
	return remotes
}

func parseStashes(out string) []Stash {
	var stashes []Stash
	for _, line := range nonEmptyLines(out) {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		stashes = append(stashes, Stash{Name: parts[0], Age: parts[1], Message: parts[2]})
	}
	return stashes
}

func detectOperation(ctx context.Context, root string) (Operation, error) {
	checks := []struct {
		kind string
		path string
		dir  bool
	}{
		{kind: "rebase", path: "rebase-merge", dir: true},
		{kind: "rebase", path: "rebase-apply", dir: true},
		{kind: "cherry-pick", path: "CHERRY_PICK_HEAD"},
		{kind: "revert", path: "REVERT_HEAD"},
		{kind: "merge", path: "MERGE_HEAD"},
		{kind: "bisect", path: "BISECT_LOG"},
	}
	for _, check := range checks {
		path, err := gitPath(ctx, root, check.path)
		if err != nil {
			return Operation{}, err
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Operation{}, err
		}
		if check.dir && !info.IsDir() {
			continue
		}
		if !check.dir && info.IsDir() {
			continue
		}
		return operationFor(check.kind), nil
	}
	return Operation{}, nil
}

func operationFor(kind string) Operation {
	op := Operation{Kind: kind, InProgress: true}
	switch kind {
	case "merge":
		op.ContinueCommand = "git merge --continue"
		op.AbortCommand = "git merge --abort"
	case "rebase":
		op.ContinueCommand = "git rebase --continue"
		op.AbortCommand = "git rebase --abort"
		op.SkipCommand = "git rebase --skip"
	case "cherry-pick":
		op.ContinueCommand = "git cherry-pick --continue"
		op.AbortCommand = "git cherry-pick --abort"
		op.SkipCommand = "git cherry-pick --skip"
	case "revert":
		op.ContinueCommand = "git revert --continue"
		op.AbortCommand = "git revert --abort"
		op.SkipCommand = "git revert --skip"
	case "bisect":
		op.AbortCommand = "git bisect reset"
	}
	return op
}

func gitPath(ctx context.Context, root, name string) (string, error) {
	out, err := git(ctx, root, "rev-parse", "--git-path", name)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(out)
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Join(root, path), nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	return gitWithAllowedExitCodes(ctx, dir, nil, args...)
}

func gitWithAllowedExitCodes(ctx context.Context, dir string, allowed []int, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Keep stderr out of the captured output. Warnings such as the CRLF
	// conversion notice are written while the diff itself is being streamed,
	// so merging the two streams can drop a "warning:" line inside a hunk and
	// corrupt every patch built from it.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			for _, code := range allowed {
				if exitErr.ExitCode() == code {
					return stdout.String(), nil
				}
			}
		}
		return "", gitCommandError(args, stderr.Bytes(), err)
	}
	return stdout.String(), nil
}

func gitCommandError(args []string, out []byte, err error) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	} else {
		msg = err.Error() + ": " + msg
	}
	return fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
}

func nonEmptyLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func nulSeparated(out string) []string {
	if out == "" {
		return nil
	}
	var parts []string
	for _, part := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func diffLines(out string) []string {
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

const (
	binaryNotePrefix    = "# binary file omitted: "
	submoduleNotePrefix = "# submodule change omitted: "
	binaryMarkerPrefix  = "Binary files "
	binaryMarkerSuffix  = " differ"
	binaryPatchMarker   = "GIT binary patch"
	diffHeaderPrefix    = "diff --git "
	hunkHeaderPrefix    = "@@"
	gitlinkMode         = "160000"
)

// omitted groups the file sections a diff had to drop by the reason they were
// dropped, so each group can be named in its own words once the surviving
// sections have been joined.
type omitted struct {
	binary    []string
	submodule []string
}

func (o omitted) empty() bool {
	return len(o.binary) == 0 && len(o.submodule) == 0
}

func mergeOmitted(groups ...omitted) omitted {
	var all omitted
	for _, group := range groups {
		all.binary = append(all.binary, group.binary...)
		all.submodule = append(all.submodule, group.submodule...)
	}
	return all
}

// stripUnappliableSections drops the file sections git apply cannot replay into
// a plain working tree, either of which would sink the whole patch since git
// apply is all-or-nothing. Binary content is one: such a section carries no
// hunk, only a "Binary files ... differ" line. A submodule pointer is the
// other: its hunk reads as ordinary text but names a commit in a repository the
// patch does not carry, so applying it fails outright where the submodule is
// not checked out and silently does nothing where it is only uninitialized.
func stripUnappliableSections(raw string) (string, omitted) {
	if raw == "" {
		return "", omitted{}
	}
	var kept strings.Builder
	var dropped omitted
	for _, section := range diffSections(raw) {
		if path, ok := binarySection(section); ok {
			dropped.binary = append(dropped.binary, path)
			continue
		}
		if submoduleSection(section) {
			dropped.submodule = append(dropped.submodule, headerPath(firstLine(section)))
			continue
		}
		kept.WriteString(section)
	}
	return kept.String(), dropped
}

// diffSections splits a diff into one chunk per file. Each chunk keeps its own
// line terminators so the survivors concatenate back verbatim.
func diffSections(raw string) []string {
	var sections []string
	var current strings.Builder
	for _, line := range strings.SplitAfter(raw, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, diffHeaderPrefix) && current.Len() > 0 {
			sections = append(sections, current.String())
			current.Reset()
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		sections = append(sections, current.String())
	}
	return sections
}

// binarySection reports whether a file section holds binary content, and names
// the file to note in its place. The marker line is what identifies it: the
// absence of hunks is not enough, because mode changes, pure renames and empty
// new files have no hunks either and are all valid to apply.
func binarySection(section string) (string, bool) {
	lines := strings.Split(section, "\n")
	for _, line := range lines {
		if line == binaryPatchMarker {
			return headerPath(lines[0]), true
		}
		if strings.HasPrefix(line, binaryMarkerPrefix) && strings.HasSuffix(line, binaryMarkerSuffix) {
			return binaryPath(line, lines[0]), true
		}
	}
	return "", false
}

// submoduleSection reports whether a file section changes a submodule pointer.
// The gitlink mode on a header line is what identifies it: the "Subproject
// commit" hunk body cannot be trusted on its own, since an ordinary text file
// documenting submodules may carry that very line. Scanning stops at the first
// hunk header so no hunk content is ever read as a header.
func submoduleSection(section string) bool {
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, hunkHeaderPrefix) {
			return false
		}
		if !strings.HasSuffix(line, " "+gitlinkMode) {
			continue
		}
		for _, prefix := range []string{"index ", "new file mode ", "deleted file mode ", "old mode ", "new mode "} {
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
	}
	return false
}

// binaryPath reads the file name out of "Binary files a/x and b/x differ". The
// post-image side is preferred so an added file reads naturally, and a deleted
// one falls back to the pre-image. A path containing " and b/" could fool this,
// but a wrong guess only changes the note's text, never the patch.
func binaryPath(marker, header string) string {
	body := strings.TrimSuffix(strings.TrimPrefix(marker, binaryMarkerPrefix), binaryMarkerSuffix)
	if idx := strings.LastIndex(body, " and b/"); idx >= 0 {
		return body[idx+len(" and b/"):]
	}
	if idx := strings.LastIndex(body, " and "); idx >= 0 {
		return strings.TrimPrefix(body[:idx], "a/")
	}
	return headerPath(header)
}

// firstLine returns a section's "diff --git" header line.
func firstLine(section string) string {
	if idx := strings.IndexByte(section, '\n'); idx >= 0 {
		return section[:idx]
	}
	return section
}

// headerPath recovers the post-image path from a "diff --git a/x b/x" line.
func headerPath(header string) string {
	rest := strings.TrimPrefix(header, diffHeaderPrefix)
	if idx := strings.LastIndex(rest, " b/"); idx >= 0 {
		return rest[idx+len(" b/"):]
	}
	return strings.TrimPrefix(rest, "a/")
}

// noteOmitted records the files stripUnappliableSections removed. The notes go
// after the last hunk: git apply skips trailing text, whereas a note wedged
// between hunks would be read as hunk content and corrupt the patch.
func noteOmitted(diff string, groups ...omitted) string {
	all := mergeOmitted(groups...)
	if all.empty() {
		return diff
	}
	var b strings.Builder
	b.WriteString(diff)
	if diff != "" {
		b.WriteString("\n")
	}
	for _, path := range all.binary {
		b.WriteString(binaryNotePrefix)
		b.WriteString(path)
		b.WriteString("\n")
	}
	for _, path := range all.submodule {
		b.WriteString(submoduleNotePrefix)
		b.WriteString(path)
		b.WriteString("\n")
	}
	return b.String()
}

func joinRawDiffs(diffs ...string) string {
	var chunks []string
	for _, diff := range diffs {
		if strings.TrimSpace(diff) != "" {
			chunks = append(chunks, strings.TrimRight(diff, "\n"))
		}
	}
	if len(chunks) == 0 {
		return ""
	}
	return strings.Join(chunks, "\n") + "\n"
}

func combineDiffs(staged, worktree []string) []string {
	var lines []string
	if len(staged) > 0 {
		lines = append(lines, "staged diff")
		lines = append(lines, staged...)
	}
	if len(worktree) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "worktree diff")
		lines = append(lines, worktree...)
	}
	return lines
}

func shortHash(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
