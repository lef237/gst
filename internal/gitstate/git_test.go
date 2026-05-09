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

func TestParseRefsSortsCurrentFirst(t *testing.T) {
	refs := parseRefs("origin/main|1111111|2 days ago|\nmain|2222222|1 day ago|origin/main\n", "main")
	if len(refs) != 2 {
		t.Fatalf("refs mismatch: %d", len(refs))
	}
	if refs[0].Name != "main" || !refs[0].Current {
		t.Fatalf("current ref should be first: %#v", refs)
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
