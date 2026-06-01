package gitstate

import (
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
	Graph        []string
	Refs         []Ref
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
		if staged, worktree, err := collectDiffs(ctx, root); err == nil {
			state.StagedDiff = diffLines(staged)
			state.WorktreeDiff = diffLines(worktree)
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

func hasHeadCommit(ctx context.Context, root string) (bool, error) {
	_, err := git(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if strings.Contains(err.Error(), "Needed a single revision") {
		return false, nil
	}
	return false, err
}

func collectDiffs(ctx context.Context, root string) (string, string, error) {
	cached, err := git(ctx, root, "diff", "--cached", "--no-ext-diff", "--unified=3")
	if err != nil {
		return "", "", err
	}
	worktree, err := git(ctx, root, "diff", "--no-ext-diff", "--unified=3")
	if err != nil {
		return "", "", err
	}
	return cached, worktree, nil
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
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		} else {
			msg = err.Error() + ": " + msg
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
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

func diffLines(out string) []string {
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
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
