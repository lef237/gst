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
	if out, err := git(ctx, root, "status", "--porcelain=v2", "--branch"); err == nil {
		parseStatus(out, &state)
	} else {
		state.Warnings = append(state.Warnings, err.Error())
	}

	if out, err := collectGraph(ctx, root, opts.LogLimit, opts.GraphAll); err == nil {
		state.Graph = nonEmptyLines(out)
	} else {
		state.Warnings = append(state.Warnings, err.Error())
	}

	if staged, worktree, err := collectDiffs(ctx, root); err == nil {
		state.StagedDiff = diffLines(staged)
		state.WorktreeDiff = diffLines(worktree)
		state.Diff = combineDiffs(state.StagedDiff, state.WorktreeDiff)
	} else {
		state.Warnings = append(state.Warnings, err.Error())
	}

	if out, err := git(ctx, root, "for-each-ref", "refs/heads", "refs/remotes", "--format=%(refname:short)|%(objectname:short)|%(committerdate:relative)|%(upstream:short)"); err == nil {
		state.Refs = parseRefs(out, state.Branch)
	} else {
		state.Warnings = append(state.Warnings, err.Error())
	}

	if out, err := git(ctx, root, "remote", "-v"); err == nil {
		state.Remotes = parseRemotes(out)
	} else {
		state.Warnings = append(state.Warnings, err.Error())
	}

	if out, err := git(ctx, root, "stash", "list", "--date=relative", "--pretty=format:%gd|%cr|%s"); err == nil {
		state.Stashes = parseStashes(out)
	} else {
		state.Warnings = append(state.Warnings, err.Error())
	}

	if op, err := detectOperation(ctx, root); err == nil {
		state.Operation = op
	} else {
		state.Warnings = append(state.Warnings, err.Error())
	}

	return state, nil
}

func collectGraph(ctx context.Context, root string, limit int, all bool) (string, error) {
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
			parseTracked(line, state)
		case 'u':
			parseUnmerged(line, state)
		case '?':
			path := strings.TrimSpace(strings.TrimPrefix(line, "?"))
			state.Files = append(state.Files, File{Status: "??", Path: path, Kind: "untracked"})
			state.Counts.Untracked++
		}
	}
}

func parseHeader(line string, state *State) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return
	}
	switch fields[1] {
	case "branch.oid":
		state.Head = shortHash(fields[2])
	case "branch.head":
		if fields[2] == "(detached)" {
			state.Detached = true
			state.Branch = "DETACHED"
		} else {
			state.Branch = fields[2]
		}
	case "branch.upstream":
		state.Upstream = fields[2]
	case "branch.ab":
		for _, f := range fields[2:] {
			if strings.HasPrefix(f, "+") {
				state.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f, "+"))
			}
			if strings.HasPrefix(f, "-") {
				state.Behind, _ = strconv.Atoi(strings.TrimPrefix(f, "-"))
			}
		}
	}
}

func parseTracked(line string, state *State) {
	fields := strings.Fields(line)
	if len(fields) < 9 {
		return
	}
	xy := fields[1]
	path := fields[len(fields)-1]
	if line[0] == '2' && len(fields) >= 10 {
		path = fields[len(fields)-2] + " -> " + fields[len(fields)-1]
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
	fields := strings.Fields(line)
	if len(fields) < 11 {
		return
	}
	state.Files = append(state.Files, File{Status: fields[1], Path: fields[len(fields)-1], Kind: "conflict"})
	state.Counts.Conflicted++
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
		parts := strings.Split(line, "|")
		for len(parts) < 4 {
			parts = append(parts, "")
		}
		name := parts[0]
		if strings.HasSuffix(name, "/HEAD") || name == "origin" {
			continue
		}
		refs = append(refs, Ref{
			Name:     name,
			Hash:     parts[1],
			Age:      parts[2],
			Upstream: parts[3],
			Remote:   strings.Contains(name, "/"),
			Current:  name == current,
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
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
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
