package gitstate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStatusPorcelainV2(t *testing.T) {
	out := `# branch.oid abcdef1234567890
# branch.head feature
# branch.upstream origin/feature
# branch.ab +2 -1
1 M. N... 100644 100644 100644 aaaaaaa bbbbbbb file.go
1 .M N... 100644 100644 100644 aaaaaaa bbbbbbb README.md
? scratch.txt
u UU N... 100644 100644 100644 100644 aaaaaaa bbbbbbb ccccccc ddddddd conflict.txt
`
	var state State
	parseStatus(out, &state)

	if state.Branch != "feature" || state.Upstream != "origin/feature" {
		t.Fatalf("branch parse failed: %#v", state)
	}
	if state.Ahead != 2 || state.Behind != 1 {
		t.Fatalf("ahead/behind parse failed: +%d -%d", state.Ahead, state.Behind)
	}
	if state.Counts.Staged != 1 || state.Counts.Modified != 1 || state.Counts.Untracked != 1 || state.Counts.Conflicted != 1 {
		t.Fatalf("counts mismatch: %#v", state.Counts)
	}
	if len(state.Files) != 4 {
		t.Fatalf("files mismatch: %d", len(state.Files))
	}
}

func TestParseStatusPorcelainV2KeepsFullPaths(t *testing.T) {
	out := strings.Join([]string{
		"# branch.oid abcdef1234567890",
		"# branch.head feature/with space",
		"1 .M N... 100644 100644 100644 aaaaaaa bbbbbbb file with spaces.txt",
		"2 R. N... 100644 100644 100644 aaaaaaa bbbbbbb R100 renamed file.txt",
		"file with spaces.txt",
		"? scratch file.txt",
		"",
	}, "\x00")
	var state State
	parseStatus(out, &state)

	if state.Branch != "feature/with space" {
		t.Fatalf("branch with space parse failed: %q", state.Branch)
	}
	if len(state.Files) != 3 {
		t.Fatalf("files mismatch: %#v", state.Files)
	}
	if state.Files[0].Path != "file with spaces.txt" {
		t.Fatalf("path with spaces was not preserved: %#v", state.Files[0])
	}
	if state.Files[1].Path != "file with spaces.txt -> renamed file.txt" {
		t.Fatalf("rename direction/path mismatch: %#v", state.Files[1])
	}
	if state.Files[2].Path != "scratch file.txt" {
		t.Fatalf("untracked path with spaces was not preserved: %#v", state.Files[2])
	}
}

func TestParseStatusPorcelainV2UnquotesNonZPaths(t *testing.T) {
	out := "# branch.oid (initial)\n" +
		"# branch.head main\n" +
		"1 .M N... 100644 100644 100644 aaaaaaa bbbbbbb \"quote\\npath.txt\"\n" +
		"2 R. N... 100644 100644 100644 aaaaaaa bbbbbbb R100 renamed.txt\t\"old\\tname.txt\"\n"
	var state State
	parseStatus(out, &state)

	if state.Head != "" {
		t.Fatalf("initial oid should render as no commits, got %q", state.Head)
	}
	if len(state.Files) != 2 {
		t.Fatalf("files mismatch: %#v", state.Files)
	}
	if state.Files[0].Path != "quote\npath.txt" {
		t.Fatalf("quoted path was not decoded: %#v", state.Files[0])
	}
	if state.Files[1].Path != "old\tname.txt -> renamed.txt" {
		t.Fatalf("quoted rename path was not decoded: %#v", state.Files[1])
	}
}

func TestParseRefsSortsCurrentFirst(t *testing.T) {
	refs := parseRefs("refs/remotes/origin/main\torigin/main\t1111111\t2 days ago\t\nrefs/heads/main\tmain\t2222222\t1 day ago\torigin/main\n", "main")
	if len(refs) != 2 {
		t.Fatalf("refs mismatch: %d", len(refs))
	}
	if refs[0].Name != "main" || !refs[0].Current {
		t.Fatalf("current ref should be first: %#v", refs)
	}
}

func TestParseRefsKeepsSlashLocalBranchesLocal(t *testing.T) {
	refs := parseRefs("refs/heads/feature/demo\tfeature/demo\t2222222\t1 day ago\torigin/feature/demo\nrefs/remotes/origin/main\torigin/main\t1111111\t2 days ago\t\n", "feature/demo")
	if len(refs) != 2 {
		t.Fatalf("refs mismatch: %#v", refs)
	}
	if refs[0].Name != "feature/demo" || refs[0].Remote || !refs[0].Current {
		t.Fatalf("slash local branch should be current local ref: %#v", refs[0])
	}
	if refs[1].Name != "origin/main" || !refs[1].Remote || refs[1].Current {
		t.Fatalf("remote ref parse failed: %#v", refs[1])
	}
}

func TestParseRefsAllowsPipeInRefNames(t *testing.T) {
	refs := parseRefs("refs/heads/feature|demo\tfeature|demo\t2222222\t1 day ago\t\n", "feature|demo")
	if len(refs) != 1 {
		t.Fatalf("refs mismatch: %#v", refs)
	}
	if refs[0].Name != "feature|demo" || refs[0].Remote || !refs[0].Current {
		t.Fatalf("pipe branch should parse as current local ref: %#v", refs[0])
	}
}

func TestDetectOperationMerge(t *testing.T) {
	root := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".git", "MERGE_HEAD"), []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	op, err := detectOperation(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !op.InProgress || op.Kind != "merge" {
		t.Fatalf("operation mismatch: %#v", op)
	}
	if op.ContinueCommand != "git merge --continue" || op.AbortCommand != "git merge --abort" {
		t.Fatalf("commands mismatch: %#v", op)
	}
}

func TestDetectOperationRebase(t *testing.T) {
	root := initTestRepo(t)
	if err := os.Mkdir(filepath.Join(root, ".git", "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}

	op, err := detectOperation(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !op.InProgress || op.Kind != "rebase" {
		t.Fatalf("operation mismatch: %#v", op)
	}
	if op.ContinueCommand != "git rebase --continue" || op.AbortCommand != "git rebase --abort" || op.SkipCommand != "git rebase --skip" {
		t.Fatalf("commands mismatch: %#v", op)
	}
}

func TestCollectGraphExcludesStashInternals(t *testing.T) {
	root := initRepoWithStash(t)

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	graph := strings.Join(state.Graph, "\n")
	if strings.Contains(graph, "refs/stash") || strings.Contains(graph, "index on") || strings.Contains(graph, "WIP on") {
		t.Fatalf("graph should hide stash internals:\n%s", graph)
	}
	if len(state.Stashes) == 0 {
		t.Fatal("stash list should still expose stash entries")
	}
}

func TestCollectGraphAllIncludesStashInternals(t *testing.T) {
	root := initRepoWithStash(t)

	state, err := Collect(context.Background(), root, Options{LogLimit: 20, GraphAll: true})
	if err != nil {
		t.Fatal(err)
	}
	graph := strings.Join(state.Graph, "\n")
	if !strings.Contains(graph, "refs/stash") && !strings.Contains(graph, "index on") && !strings.Contains(graph, "WIP on") {
		t.Fatalf("graph --all should expose stash internals:\n%s", graph)
	}
}

func TestCollectDiffIncludesStagedAndWorktreeDiffs(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "file.txt")
	runGit(t, root, "commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "file.txt")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	diff := strings.Join(state.Diff, "\n")
	for _, want := range []string{"staged diff", "worktree diff", "-one", "+two", "-two", "+three"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
}

func TestCollectEmptyRepoDoesNotWarnAboutMissingHead(t *testing.T) {
	root := initTestRepo(t)

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Graph) != 0 {
		t.Fatalf("empty repo should not have graph lines: %#v", state.Graph)
	}
	if len(state.Warnings) != 0 {
		t.Fatalf("empty repo should not warn: %#v", state.Warnings)
	}
}

func TestCollectHandlesPathsWithSpacesAndRenames(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "file with spaces.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "file with spaces.txt")
	runGit(t, root, "commit", "-qm", "initial")
	runGit(t, root, "mv", "file with spaces.txt", "renamed file.txt")

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 1 {
		t.Fatalf("files mismatch: %#v", state.Files)
	}
	if state.Files[0].Path != "file with spaces.txt -> renamed file.txt" {
		t.Fatalf("rename path mismatch: %#v", state.Files[0])
	}
}

func initRepoWithStash(t *testing.T) string {
	t.Helper()
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "file.txt")
	runGit(t, root, "commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "file.txt")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "stash", "push", "-q", "-m", "hide changes")
	return root
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
