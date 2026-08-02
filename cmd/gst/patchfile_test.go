package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lef237/gst/internal/gitstate"
)

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		branch string
		want   string
	}{
		{"main", "main"},
		{"release_v1.2", "release_v1.2"},
		{"feature/fix", "feature-fix"},
		{"feature//a  b", "feature-a-b"},
		{"///main", "main"},
		{"main/", "main"},
		{"-.main.-", "main"},
		{"", ""},
		{"///", ""},
		{"ブランチ", ""},
		{strings.Repeat("a", 60), strings.Repeat("a", patchBranchLimit)},
	}
	for _, tc := range cases {
		if got := sanitizeFileName(tc.branch); got != tc.want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", tc.branch, got, tc.want)
		}
	}
}

func TestPatchFileBase(t *testing.T) {
	now := time.Date(2026, 8, 2, 14, 30, 22, 0, time.UTC)
	cases := []struct {
		branch string
		target diffTarget
		want   string
	}{
		{"main", worktreeDiffTarget, "gst-main-worktree-20260802-143022"},
		{"feature/fix", stagedDiffTarget, "gst-feature-fix-staged-20260802-143022"},
		{"main", fullDiffTarget, "gst-main-full-20260802-143022"},
		// A detached HEAD reports no branch, and a branch whose name survives
		// sanitizing as nothing must not be mistaken for one.
		{"", worktreeDiffTarget, "gst-detached-worktree-20260802-143022"},
		{"ブランチ", worktreeDiffTarget, "gst-branch-worktree-20260802-143022"},
	}
	for _, tc := range cases {
		if got := patchFileBase(tc.branch, tc.target, now); got != tc.want {
			t.Errorf("patchFileBase(%q, %v) = %q, want %q", tc.branch, tc.target, got, tc.want)
		}
	}
}

// TestWritePatchFileNeverOverwrites pins the promise that saving twice keeps
// both patches. The timestamp only resolves to the second, so two keystrokes in
// a row land on the same base name.
func TestWritePatchFileNeverOverwrites(t *testing.T) {
	dir := t.TempDir()

	first, err := writePatchFile(dir, "gst-main-worktree-20260802-143022", "first\n")
	if err != nil {
		t.Fatal(err)
	}
	if first != "gst-main-worktree-20260802-143022.patch" {
		t.Fatalf("first name = %q", first)
	}
	second, err := writePatchFile(dir, "gst-main-worktree-20260802-143022", "second\n")
	if err != nil {
		t.Fatal(err)
	}
	if second != "gst-main-worktree-20260802-143022-2.patch" {
		t.Fatalf("second name = %q", second)
	}

	for name, want := range map[string]string{first: "first\n", second: "second\n"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s holds %q, want %q", name, got, want)
		}
	}
}

// TestSavePatchWritesPayloadVerbatim is what ties the file to the clipboard: the
// bytes on disk are the same payload gitstate already proves git apply accepts,
// so the patch stays appliable without this package rebuilding a repository.
func TestSavePatchWritesPayloadVerbatim(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	state := gitstate.State{
		Branch: "feature/fix",
		WorktreeDiff: []string{
			"diff --git a/file.txt b/file.txt",
			"--- a/file.txt",
			"+++ b/file.txt",
			"@@ -1 +1 @@",
			"-one",
			"+two",
		},
	}
	_, want := diffPayload(state, worktreeDiffTarget)

	notice := savePatch(state, worktreeDiffTarget)
	if !strings.HasPrefix(notice, "saved worktree diff to gst-feature-fix-worktree-") {
		t.Fatalf("notice = %q", notice)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("working directory holds %d entries, want 1", len(entries))
	}
	name := entries[0].Name()
	if !strings.HasSuffix(notice, name) {
		t.Fatalf("notice %q does not name the file it wrote (%s)", notice, name)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("file holds %q, want %q", got, want)
	}
}

func TestSavePatchEmptyDiff(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	notice := savePatch(gitstate.State{Branch: "main"}, stagedDiffTarget)
	if notice != "nothing to save: staged diff is empty" {
		t.Fatalf("notice = %q", notice)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("an empty diff wrote %d files", len(entries))
	}
}
