package ui

import (
	"strings"
	"testing"

	"github.com/lef237/gst/internal/gitstate"
)

func TestRenderTabFitsHeight(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/repo",
		Branch:   "main",
		Upstream: "origin/main",
		Head:     "abcdef1",
		Graph: []string{
			"* abcdef1 (HEAD -> main) one",
			"* 2222222 two",
			"* 3333333 three",
			"* 4444444 four",
			"* 5555555 five",
		},
	}

	out := RenderTab(state, TabGraph, Options{Width: 70, Height: 8, Interactive: true})
	lines := strings.Split(out, "\n")
	if len(lines) > 8 {
		t.Fatalf("rendered %d lines, want at most 8:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "[2:graph]") {
		t.Fatalf("active tab was not rendered:\n%s", out)
	}
}

func TestRenderHelpTab(t *testing.T) {
	state := gitstate.State{RepoRoot: "/repo", Branch: "main", Head: "abcdef1"}
	out := RenderTab(state, TabHelp, Options{Width: 80, Height: 14, Interactive: true})

	if !strings.Contains(out, "[? help]") {
		t.Fatalf("help tab was not active:\n%s", out)
	}
	if !strings.Contains(out, "?") || !strings.Contains(out, "q") {
		t.Fatalf("help text should include key bindings:\n%s", out)
	}
}

func TestRenderBranchAndStashTabs(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/repo",
		Branch:   "main",
		Upstream: "origin/main",
		Head:     "abcdef1",
		Ahead:    1,
		Behind:   2,
		Refs:     []gitstate.Ref{{Name: "main", Hash: "abcdef1", Age: "now", Upstream: "origin/main", Current: true}},
		Stashes:  []gitstate.Stash{{Name: "stash@{0}", Age: "2 hours ago", Message: "WIP on main"}},
		Remotes:  []gitstate.Remote{{Name: "origin", Kind: "fetch", URL: "git@example.com:repo.git"}},
		Warnings: nil,
	}

	branches := RenderTab(state, TabBranches, Options{Width: 90, Height: 16, Interactive: true})
	if !strings.Contains(branches, "[4:branches]") || !strings.Contains(branches, "diverged") {
		t.Fatalf("branch tab did not render branch relationship:\n%s", branches)
	}

	stash := RenderTab(state, TabStash, Options{Width: 90, Height: 12, Interactive: true})
	if !strings.Contains(stash, "[5:stash]") || !strings.Contains(stash, "stash@{0}") {
		t.Fatalf("stash tab did not render stash entry:\n%s", stash)
	}
}

func TestRenderNarrowOverviewFitsWidth(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/very/long/repository/path/that/must/not/overflow",
		Branch:   "feature/very-long-branch-name",
		Upstream: "origin/feature/very-long-branch-name",
		Head:     "abcdef1",
		Ahead:    3,
		Files: []gitstate.File{
			{Status: ".M", Path: "a/very/long/path/to/a/changed/file/that/should/be/truncated.go", Kind: "worktree"},
		},
		Counts: gitstate.Counts{Modified: 1},
	}

	out := RenderTab(state, TabOverview, Options{Width: 40, Height: 14, Interactive: true})
	assertFits(t, out, 40, 14)
	if strings.Contains(out, "+  +") {
		t.Fatalf("narrow overview should not render side-by-side panels:\n%s", out)
	}
}

func TestRenderNarrowTabsFitWidth(t *testing.T) {
	state := gitstate.State{RepoRoot: "/repo", Branch: "main", Head: "abcdef1"}
	for _, tab := range []Tab{TabGraph, TabFiles, TabBranches, TabStash, TabRefs, TabRemote, TabHelp} {
		out := RenderTab(state, tab, Options{Width: 35, Height: 10, Interactive: true})
		assertFits(t, out, 35, 10)
	}
}

func TestFileStatusUsesGitLikeColors(t *testing.T) {
	opts := Options{Color: true}

	mm := colorFileStatus("MM", opts)
	if !strings.Contains(mm, string(green)+"M"+string(reset)) {
		t.Fatalf("index status should be green: %q", mm)
	}
	if !strings.Contains(mm, string(red)+"M"+string(reset)) {
		t.Fatalf("worktree status should be red: %q", mm)
	}

	worktreeOnly := colorFileStatus(".M", opts)
	if !strings.HasPrefix(worktreeOnly, " ") || !strings.Contains(worktreeOnly, string(red)+"M"+string(reset)) {
		t.Fatalf("worktree-only status should be blank then red: %q", worktreeOnly)
	}

	untracked := colorFileStatus("??", opts)
	if untracked != string(red)+"??"+string(reset) {
		t.Fatalf("untracked status should be red: %q", untracked)
	}

	stagedDelete := colorFileStatus("D.", opts)
	if !strings.Contains(stagedDelete, string(green)+"D"+string(reset)) {
		t.Fatalf("staged delete should be green: %q", stagedDelete)
	}

	conflict := colorFileStatus("UU", opts)
	if strings.Count(conflict, string(red)) != 2 {
		t.Fatalf("conflict status should color both columns red: %q", conflict)
	}

	if got := colorFileStatus("MM", Options{}); got != "MM" {
		t.Fatalf("no-color status mismatch: %q", got)
	}
}

func TestTruncateResetsActiveColor(t *testing.T) {
	out := truncate(color(Options{Color: true}, "this message is too long", yellow), 10)
	if !strings.Contains(out, string(yellow)) {
		t.Fatalf("truncated colored string lost its color: %q", out)
	}
	if !strings.HasSuffix(out, string(reset)) {
		t.Fatalf("truncated colored string must reset color to avoid bleed: %q", out)
	}

	plain := truncate("this message is too long", 10)
	if strings.Contains(plain, string(reset)) {
		t.Fatalf("plain truncated string should not gain reset code: %q", plain)
	}
}

func TestTabBarResponsiveModes(t *testing.T) {
	wide := tabBar(TabBranches, Options{Width: 100})
	if !strings.Contains(wide, "7:remote") || !strings.Contains(wide, "? help") || !strings.Contains(wide, "q quit") {
		t.Fatalf("wide tab bar should include all tabs and compact help:\n%s", wide)
	}
	if visibleLen(wide) > 100 {
		t.Fatalf("wide tab bar overflowed: %d\n%s", visibleLen(wide), wide)
	}

	medium := tabBar(TabBranches, Options{Width: 70})
	if !strings.Contains(medium, "[4:branches]") || !strings.Contains(medium, "7:remote") {
		t.Fatalf("medium tab bar should include all tabs:\n%s", medium)
	}
	if visibleLen(medium) > 70 {
		t.Fatalf("medium tab bar overflowed: %d\n%s", visibleLen(medium), medium)
	}

	narrow := tabBar(TabBranches, Options{Width: 30})
	if !strings.Contains(narrow, "4/7") || !strings.Contains(narrow, "? q") {
		t.Fatalf("narrow tab bar should keep current tab and quit hint:\n%s", narrow)
	}
	if visibleLen(narrow) > 30 {
		t.Fatalf("narrow tab bar overflowed: %d\n%s", visibleLen(narrow), narrow)
	}

	help := tabBar(TabHelp, Options{Width: 35})
	if !strings.Contains(help, "? help") || visibleLen(help) > 35 {
		t.Fatalf("help tab bar should fit and show help state:\n%s", help)
	}
}

func assertFits(t *testing.T, out string, width, height int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > height {
		t.Fatalf("rendered %d lines, want at most %d:\n%s", len(lines), height, out)
	}
	for i, line := range lines {
		if visibleLen(line) > width {
			t.Fatalf("line %d has width %d, want at most %d:\n%s", i+1, visibleLen(line), width, out)
		}
	}
}
