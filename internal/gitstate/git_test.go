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

func TestParseRefsKeepsLocalBranchNamedOrigin(t *testing.T) {
	refs := parseRefs("refs/remotes/origin/HEAD\torigin\t1111111\t2 days ago\t\nrefs/heads/origin\torigin\t2222222\t1 day ago\t\n", "origin")
	if len(refs) != 1 {
		t.Fatalf("refs mismatch: %#v", refs)
	}
	if refs[0].Name != "origin" || refs[0].Remote || !refs[0].Current {
		t.Fatalf("local branch named origin should be kept: %#v", refs[0])
	}
}

func TestParseTagsPeelsAnnotatedTags(t *testing.T) {
	tags := parseTags("v1.0.0\t9999999\t1111111\ttag\t2 days ago\trelease v1\nv0.9.0\t2222222\t\tcommit\t3 days ago\tinitial\n")
	if len(tags) != 2 {
		t.Fatalf("tags mismatch: %#v", tags)
	}
	if tags[0].Name != "v1.0.0" || tags[0].Hash != "1111111" || !tags[0].Annotated || tags[0].Subject != "release v1" {
		t.Fatalf("annotated tag parse failed: %#v", tags[0])
	}
	if tags[1].Name != "v0.9.0" || tags[1].Hash != "2222222" || tags[1].Annotated || tags[1].Subject != "initial" {
		t.Fatalf("lightweight tag parse failed: %#v", tags[1])
	}
}

func TestCollectIncludesTags(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "file.txt")
	runGit(t, root, "commit", "-qm", "initial")
	runGit(t, root, "tag", "v0.1.0")
	runGit(t, root, "tag", "-a", "v1.0.0", "-m", "release v1")

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Tag{}
	for _, tag := range state.Tags {
		byName[tag.Name] = tag
	}
	if len(byName) != 2 {
		t.Fatalf("tags mismatch: %#v", state.Tags)
	}
	if tag := byName["v0.1.0"]; tag.Name == "" || tag.Annotated || tag.Hash != state.Head {
		t.Fatalf("lightweight tag mismatch: %#v, head %q", tag, state.Head)
	}
	if tag := byName["v1.0.0"]; tag.Name == "" || !tag.Annotated || tag.Hash != state.Head || tag.Subject != "release v1" {
		t.Fatalf("annotated tag mismatch: %#v, head %q", tag, state.Head)
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
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("fresh\n"), 0o644); err != nil {
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
	headDiff := strings.Join(state.HeadDiff, "\n")
	for _, want := range []string{"-one", "+three", "new file mode", "+++ b/new.txt", "+fresh"} {
		if !strings.Contains(headDiff, want) {
			t.Fatalf("head diff missing %q:\n%s", want, headDiff)
		}
	}
}

func TestCollectHeadDiffIncludesUntrackedOnlyFiles(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	headDiff := strings.Join(state.HeadDiff, "\n")
	for _, want := range []string{"diff --git a/untracked.txt b/untracked.txt", "new file mode", "+++ b/untracked.txt", "+new"} {
		if !strings.Contains(headDiff, want) {
			t.Fatalf("head diff missing untracked %q:\n%s", want, headDiff)
		}
	}
}

func TestCollectWorktreeDiffIncludesUntrackedFiles(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	worktreeDiff := strings.Join(state.WorktreeDiff, "\n")
	for _, want := range []string{"+changed", "diff --git a/untracked.txt b/untracked.txt", "new file mode", "+++ b/untracked.txt", "+new"} {
		if !strings.Contains(worktreeDiff, want) {
			t.Fatalf("worktree diff missing %q:\n%s", want, worktreeDiff)
		}
	}
}

func TestCollectHeadDiffOmitsUntrackedBinaryContents(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0x00, 0x01, 0x02, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	headDiff := strings.Join(state.HeadDiff, "\n")
	if !strings.Contains(headDiff, "# binary file omitted: image.bin") {
		t.Fatalf("head diff should note the omitted binary file:\n%s", headDiff)
	}
	for _, unwanted := range []string{"Binary files", "GIT binary patch", "literal ", "diff --git a/image.bin"} {
		if strings.Contains(headDiff, unwanted) {
			t.Fatalf("head diff should omit binary section %q:\n%s", unwanted, headDiff)
		}
	}
}

func TestCollectOmitsTrackedBinaryFilesFromEveryDiff(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "text.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0x00, 0x01, 0x02, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "text.txt", "image.bin")
	runGit(t, root, "commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0x00, 0x03, 0x04, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "image.bin")
	if err := os.WriteFile(filepath.Join(root, "text.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for name, diff := range map[string][]string{
		"staged":   state.StagedDiff,
		"worktree": state.WorktreeDiff,
		"head":     state.HeadDiff,
	} {
		text := strings.Join(diff, "\n")
		if strings.Contains(text, "Binary files") || strings.Contains(text, "diff --git a/image.bin") {
			t.Fatalf("%s diff still carries the binary section:\n%s", name, text)
		}
	}
	staged := strings.Join(state.StagedDiff, "\n")
	if !strings.Contains(staged, "# binary file omitted: image.bin") {
		t.Fatalf("staged diff should note the omitted binary file:\n%s", staged)
	}
	worktree := strings.Join(state.WorktreeDiff, "\n")
	if strings.Contains(worktree, "# binary file omitted") {
		t.Fatalf("worktree diff has no binary change to note:\n%s", worktree)
	}
	if !strings.Contains(worktree, "+two") {
		t.Fatalf("worktree diff lost its text change:\n%s", worktree)
	}
}

func TestCollectKeepsHunklessTextSections(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(root, "mode.sh"), []byte("echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("stable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "mode.sh", "old.txt")
	runGit(t, root, "commit", "-qm", "initial")
	if err := os.Chmod(filepath.Join(root, "mode.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "mv", "old.txt", "new.txt")
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	staged := strings.Join(state.StagedDiff, "\n")
	// None of these carry a hunk, so they must not be mistaken for binary.
	for _, want := range []string{"new mode 100755", "rename to new.txt", "diff --git a/empty.txt b/empty.txt"} {
		if !strings.Contains(staged, want) {
			t.Fatalf("staged diff dropped hunkless section %q:\n%s", want, staged)
		}
	}
	if strings.Contains(staged, "# binary file omitted") {
		t.Fatalf("staged diff has no binary change to note:\n%s", staged)
	}
}

func TestCollectDiffsApplyCleanly(t *testing.T) {
	root := initTestRepo(t)
	runGit(t, root, "config", "user.email", "a@example.com")
	runGit(t, root, "config", "user.name", "a")
	// Config a user might plausibly have set, each of which reshapes diff
	// output into something git apply would reject.
	runGit(t, root, "config", "color.ui", "always")
	runGit(t, root, "config", "diff.mnemonicPrefix", "true")
	runGit(t, root, "config", "diff.noprefix", "true")
	runGit(t, root, "config", "core.autocrlf", "true")
	if err := os.WriteFile(filepath.Join(root, "text.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0x00, 0x01, 0x02, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, "text.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0x00, 0x03, 0x04, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "added.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	patch := strings.Join(state.HeadDiff, "\n") + "\n"
	if strings.Contains(patch, "\x1b[") {
		t.Fatalf("patch carries ANSI color escapes:\n%q", patch)
	}
	if strings.Contains(patch, "warning:") {
		t.Fatalf("patch carries a git warning from stderr:\n%s", patch)
	}
	if !strings.Contains(patch, "--- a/text.txt") || !strings.Contains(patch, "+++ b/text.txt") {
		t.Fatalf("patch does not use a/ and b/ prefixes:\n%s", patch)
	}

	// Apply into a clone of the base commit; the untracked file must not
	// already exist there or git apply would reject it as a duplicate.
	target := initTestRepo(t)
	runGit(t, target, "config", "user.email", "a@example.com")
	runGit(t, target, "config", "user.name", "a")
	runGit(t, target, "fetch", "-q", root, "HEAD")
	runGit(t, target, "checkout", "-q", "FETCH_HEAD")

	patchFile := filepath.Join(t.TempDir(), "gst.patch")
	if err := os.WriteFile(patchFile, []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "apply", "--check", patchFile)
	cmd.Dir = target
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply --check rejected the yanked diff: %v\n%s\n--- patch ---\n%s", err, out, patch)
	}
}

func TestStripBinaryDiffsKeepsTextSections(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/image.png b/image.png",
		"index 20f982d..df1cf1d 100644",
		"Binary files a/image.png and b/image.png differ",
		"diff --git a/text.txt b/text.txt",
		"index b77b4eb..7061c57 100644",
		"--- a/text.txt",
		"+++ b/text.txt",
		"@@ -1,2 +1,2 @@",
		" x",
		"-y",
		"+Y",
		"diff --git a/added.bin b/added.bin",
		"new file mode 100644",
		"index 0000000..1939cfc",
		"Binary files /dev/null and b/added.bin differ",
		"diff --git a/gone.bin b/gone.bin",
		"deleted file mode 100644",
		"index 1939cfc..0000000",
		"Binary files a/gone.bin and /dev/null differ",
		"",
	}, "\n")

	text, binary := stripBinaryDiffs(raw)

	want := []string{"image.png", "added.bin", "gone.bin"}
	if len(binary) != len(want) {
		t.Fatalf("binary paths mismatch: %#v", binary)
	}
	for i, path := range want {
		if binary[i] != path {
			t.Fatalf("binary path %d: got %q want %q", i, binary[i], path)
		}
	}
	if strings.Contains(text, "Binary files") {
		t.Fatalf("binary sections survived:\n%s", text)
	}
	if !strings.Contains(text, "+Y") || !strings.Contains(text, "diff --git a/text.txt b/text.txt") {
		t.Fatalf("text section was dropped:\n%s", text)
	}
}

func TestCollectHeadDiffIncludesInitialStagedAndUntrackedFiles(t *testing.T) {
	root := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := Collect(context.Background(), root, Options{LogLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	headDiff := strings.Join(state.HeadDiff, "\n")
	for _, want := range []string{"diff --git a/staged.txt b/staged.txt", "+++ b/staged.txt", "+staged", "diff --git a/untracked.txt b/untracked.txt", "+++ b/untracked.txt", "+untracked"} {
		if !strings.Contains(headDiff, want) {
			t.Fatalf("initial head diff missing %q:\n%s", want, headDiff)
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
