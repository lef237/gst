package main

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/lef237/gst/internal/gitstate"
	"github.com/lef237/gst/internal/ui"
)

func TestEnqueueLatestKeepsOnlyNewestKey(t *testing.T) {
	keys := make(chan string, 1)
	ctx := context.Background()

	if !enqueueLatest(ctx, keys, "\t") {
		t.Fatal("first enqueue failed")
	}
	if !enqueueLatest(ctx, keys, "\t") {
		t.Fatal("second enqueue failed")
	}
	if !enqueueLatest(ctx, keys, "q") {
		t.Fatal("third enqueue failed")
	}

	got := <-keys
	if got != "q" {
		t.Fatalf("got %q, want latest key q", got)
	}

	select {
	case extra := <-keys:
		t.Fatalf("unexpected queued key %q", extra)
	default:
	}
}

func TestEnqueueLatestStopsWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	keys := make(chan string, 1)
	if enqueueLatest(ctx, keys, "q") {
		t.Fatal("enqueue should stop after context cancellation")
	}
}

func TestPrintOnceReturnsFailureOutsideGitRepo(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	if got := printOnce(context.Background(), gitstate.Options{}, ui.Options{}); got != 1 {
		t.Fatalf("printOnce outside a git repository = %d, want 1", got)
	}
}

func TestParseKeyDecodesArrowKeys(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "\x1b[A", want: "up"},
		{input: "\x1b[B", want: "down"},
		{input: "\x1b[C", want: "right"},
		{input: "\x1b[D", want: "left"},
		{input: "\x1b[5~", want: "pageup"},
		{input: "\x1b[6~", want: "pagedown"},
		{input: "\x1bOC", want: "right"},
		{input: "\x1bOD", want: "left"},
		{input: "q", want: "q"},
	}

	for _, tt := range tests {
		reader := bufio.NewReader(strings.NewReader(tt.input))
		first, err := reader.ReadByte()
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseKey(reader, first)
		if err != nil {
			t.Fatalf("parseKey(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parseKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTabNavigationWraps(t *testing.T) {
	if got := nextTab(ui.TabOverview); got != ui.TabGraph {
		t.Fatalf("next overview = %v, want graph", got)
	}
	if got := previousTab(ui.TabOverview); got != ui.TabRemote {
		t.Fatalf("previous overview = %v, want remote", got)
	}
	if got := nextTab(ui.TabHelp); got != ui.TabOverview {
		t.Fatalf("next help = %v, want overview", got)
	}
	if got := previousTab(ui.TabHelp); got != ui.TabRemote {
		t.Fatalf("previous help = %v, want remote", got)
	}
}

func TestCanScrollListTabs(t *testing.T) {
	if canScroll(ui.TabOverview, false) {
		t.Fatal("overview should not scroll")
	}
	if !canScroll(ui.TabFiles, false) || !canScroll(ui.TabRefs, false) || !canScroll(ui.TabHelp, false) {
		t.Fatal("list-style tabs should scroll")
	}
	if canScroll(ui.TabFiles, true) {
		t.Fatal("native status mode keeps its own static view")
	}
}

func TestDiffClipboardPayloads(t *testing.T) {
	state := gitstate.State{
		StagedDiff: []string{
			"diff --git a/file.txt b/file.txt",
			"+staged",
		},
		WorktreeDiff: []string{
			"diff --git a/file.txt b/file.txt",
			"+worktree",
		},
		HeadDiff: []string{
			"diff --git a/file.txt b/file.txt",
			"+final",
		},
	}

	worktreeOnly := state
	worktreeOnly.StagedDiff = nil
	label, text := diffClipboardPayload(worktreeOnly, copyWorktreeDiff)
	if label != "worktree diff" || text != "diff --git a/file.txt b/file.txt\n+worktree\n" {
		t.Fatalf("worktree payload = %q, %q", label, text)
	}

	label, text = diffClipboardPayload(state, copyStagedDiff)
	if label != "staged diff" || text != "diff --git a/file.txt b/file.txt\n+staged\n" {
		t.Fatalf("staged payload = %q, %q", label, text)
	}

	label, text = diffClipboardPayload(state, copyAllDiffs)
	want := "diff --git a/file.txt b/file.txt\n+final\n"
	if label != "full diff" || text != want {
		t.Fatalf("all payload = %q, %q", label, text)
	}
}

func TestCopyWorktreeDiffRejectsIndexRelativePatch(t *testing.T) {
	state := gitstate.State{
		StagedDiff:   []string{"diff --git a/file.txt b/file.txt", "+staged"},
		WorktreeDiff: []string{"diff --git a/file.txt b/file.txt", "+worktree"},
	}

	got := copyDiff(context.Background(), state, copyWorktreeDiff)
	if !strings.Contains(got, "index-based") {
		t.Fatalf("copy worktree with staged changes = %q", got)
	}
}

func TestDiffClipboardPayloadEmpty(t *testing.T) {
	_, text := diffClipboardPayload(gitstate.State{}, copyAllDiffs)
	if text != "" {
		t.Fatalf("empty all-diff payload = %q, want empty", text)
	}
}
