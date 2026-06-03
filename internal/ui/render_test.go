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
	out := RenderTab(state, TabHelp, Options{Width: 80, Height: 22, Interactive: true})

	if !strings.Contains(out, "[? help]") {
		t.Fatalf("help tab was not active:\n%s", out)
	}
	if !strings.Contains(out, "?") || !strings.Contains(out, "q") {
		t.Fatalf("help text should include key bindings:\n%s", out)
	}
}

func TestRenderGraphAllMode(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/repo",
		Branch:   "main",
		Head:     "abcdef1",
		Graph:    []string{"* abcdef1 (HEAD -> main) one"},
	}

	normal := RenderTab(state, TabGraph, Options{Width: 130, Height: 12, Interactive: true})
	if !strings.Contains(normal, "press a to show detailed --all graph") {
		t.Fatalf("normal graph should advertise --all toggle:\n%s", normal)
	}

	detailed := RenderTab(state, TabGraph, Options{Width: 130, Height: 12, Interactive: true, GraphAll: true})
	if !strings.Contains(detailed, "commit graph --all") || !strings.Contains(detailed, "press a to return to normal branch graph") {
		t.Fatalf("detailed graph mode was not rendered:\n%s", detailed)
	}
}

func TestRenderDiffTab(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/repo",
		Branch:   "main",
		Head:     "abcdef1",
		StagedDiff: []string{
			"diff --git a/file.txt b/file.txt",
			"@@ -1 +1 @@",
			"-old",
			"+new",
		},
		WorktreeDiff: []string{
			"diff --git a/file.txt b/file.txt",
			"@@ -1 +1 @@",
			"-new",
			"+newer",
		},
	}

	out := RenderTab(state, TabDiff, Options{Width: 90, Height: 12, Interactive: true})
	if !strings.Contains(out, "[4:diff]") || !strings.Contains(out, "mode: worktree diff") || !strings.Contains(out, "+newer") || !strings.Contains(out, "copy: y worktree") {
		t.Fatalf("diff tab did not render worktree patch:\n%s", out)
	}

	staged := RenderTab(state, TabDiff, Options{Width: 90, Height: 12, Interactive: true, DiffStaged: true})
	if !strings.Contains(staged, "diff staged") || !strings.Contains(staged, "mode: staged diff") || !strings.Contains(staged, "+new") {
		t.Fatalf("diff tab did not render staged patch:\n%s", staged)
	}
}

func TestRenderNoticeInFooter(t *testing.T) {
	state := gitstate.State{RepoRoot: "/repo", Branch: "main", Head: "abcdef1"}
	out := RenderTab(state, TabDiff, Options{Width: 80, Height: 10, Interactive: true, Notice: "copied worktree diff"})
	if !strings.Contains(out, "copied worktree diff") {
		t.Fatalf("notice was not rendered:\n%s", out)
	}
}

func TestRenderTabFooterShowsCurrentKeys(t *testing.T) {
	state := gitstate.State{RepoRoot: "/repo", Branch: "main", Head: "abcdef1"}
	out := RenderTab(state, TabDiff, Options{Width: 120, Height: 12, Interactive: true})
	lines := strings.Split(out, "\n")
	if len(lines) != 12 {
		t.Fatalf("rendered %d lines, want exactly 12:\n%s", len(lines), out)
	}
	for _, want := range []string{"[keys]", "s:toggle", "y:copy-worktree", "i:copy-staged", "a:copy-full"} {
		if !strings.Contains(out, want) {
			t.Fatalf("footer missing %q:\n%s", want, out)
		}
	}
}

func TestRenderOverviewFooterUsesDescriptiveKeys(t *testing.T) {
	state := gitstate.State{RepoRoot: "/repo", Branch: "main", Head: "abcdef1"}
	out := RenderTab(state, TabOverview, Options{Width: 80, Height: 12, Interactive: true})
	for _, want := range []string{"left/right:tabs", "t:native"} {
		if !strings.Contains(out, want) {
			t.Fatalf("overview footer missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTabFooterWrapsOnNarrowWidth(t *testing.T) {
	state := gitstate.State{RepoRoot: "/repo", Branch: "main", Head: "abcdef1"}
	out := RenderTab(state, TabDiff, Options{Width: 30, Height: 10, Interactive: true})
	assertFits(t, out, 30, 10)
	for _, want := range []string{"[keys]", "y:wt", "i:stg", "a:full"} {
		if !strings.Contains(out, want) {
			t.Fatalf("narrow footer missing %q:\n%s", want, out)
		}
	}
}

func TestScrollablePanelUsesOffset(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/repo",
		Branch:   "main",
		Head:     "abcdef1",
		Graph: []string{
			"* 1111111 one",
			"* 2222222 two",
			"* 3333333 three",
			"* 4444444 four",
			"* 5555555 five",
			"* 6666666 six",
		},
	}

	out := RenderTab(state, TabGraph, Options{Width: 90, Height: 9, Interactive: true, Scroll: 4})
	if !strings.Contains(out, "commit graph 5-") || !strings.Contains(out, "2222222") {
		t.Fatalf("graph should render with scroll offset:\n%s", out)
	}
	if got := MaxScroll(state, TabGraph, Options{Width: 90, Height: 9, Interactive: true}); got != 5 {
		t.Fatalf("max graph scroll = %d, want 5", got)
	}
}

func TestFileTabScrollsLongLists(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/repo",
		Branch:   "main",
		Head:     "abcdef1",
		Files: []gitstate.File{
			{Status: ".M", Path: "one.go", Kind: "worktree"},
			{Status: ".M", Path: "two.go", Kind: "worktree"},
			{Status: ".M", Path: "three.go", Kind: "worktree"},
			{Status: ".M", Path: "four.go", Kind: "worktree"},
			{Status: ".M", Path: "five.go", Kind: "worktree"},
			{Status: ".M", Path: "six.go", Kind: "worktree"},
		},
	}

	out := RenderTab(state, TabFiles, Options{Width: 80, Height: 8, Interactive: true, Scroll: 2})
	if !strings.Contains(out, "changed files 3-") || !strings.Contains(out, "three.go") || strings.Contains(out, "one.go") {
		t.Fatalf("file tab should render with scroll offset:\n%s", out)
	}
	if got := MaxScroll(state, TabFiles, Options{Width: 80, Height: 8, Interactive: true}); got == 0 {
		t.Fatalf("file tab should be scrollable")
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
	if !strings.Contains(branches, "[5:branches]") || !strings.Contains(branches, "diverged") {
		t.Fatalf("branch tab did not render branch relationship:\n%s", branches)
	}

	stash := RenderTab(state, TabStash, Options{Width: 90, Height: 12, Interactive: true})
	if !strings.Contains(stash, "[6:stash]") || !strings.Contains(stash, "stash@{0}") {
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

func TestRenderNarrowOverviewKeepsMetersVisible(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/repo",
		Branch:   "feature",
		Head:     "abcdef1",
		Files: []gitstate.File{
			{Status: ".M", Path: "internal/ui/render.go", Kind: "worktree"},
		},
		Counts: gitstate.Counts{Modified: 1},
	}

	out := RenderTab(state, TabOverview, Options{Width: 50, Height: 18, Interactive: false})
	assertFits(t, out, 50, 18)
	for _, want := range []string{"changes    1", "worktree", "[##################]", "WORKTREE"} {
		if !strings.Contains(out, want) {
			t.Fatalf("narrow overview missing %q:\n%s", want, out)
		}
	}
}

func TestRenderNarrowTabsFitWidth(t *testing.T) {
	state := gitstate.State{RepoRoot: "/repo", Branch: "main", Head: "abcdef1"}
	for _, tab := range []Tab{TabGraph, TabFiles, TabDiff, TabBranches, TabStash, TabRefs, TabRemote, TabHelp} {
		out := RenderTab(state, tab, Options{Width: 35, Height: 10, Interactive: true})
		assertFits(t, out, 35, 10)
	}
}

func TestOverviewShowsConflictNextActions(t *testing.T) {
	state := gitstate.State{
		RepoRoot: "/repo",
		Branch:   "main",
		Upstream: "origin/main",
		Head:     "abcdef1",
		Files: []gitstate.File{
			{Status: "UU", Path: "conflict.go", Kind: "conflict"},
		},
		Counts: gitstate.Counts{Conflicted: 1},
		Operation: gitstate.Operation{
			Kind:            "rebase",
			InProgress:      true,
			ContinueCommand: "git rebase --continue",
			AbortCommand:    "git rebase --abort",
			SkipCommand:     "git rebase --skip",
		},
	}

	out := RenderTab(state, TabOverview, Options{Width: 100, Height: 24, Interactive: true})
	for _, want := range []string{"attention", "rebase in progress", "conflict.go", "git add <resolved files>", "git rebase --continue", "git rebase --abort", "press t"} {
		if !strings.Contains(out, want) {
			t.Fatalf("overview missing %q:\n%s", want, out)
		}
	}
}

func TestFileLinesPrioritizeConflicts(t *testing.T) {
	state := gitstate.State{
		Files: []gitstate.File{
			{Status: ".M", Path: "later.go", Kind: "worktree"},
			{Status: "UU", Path: "first.go", Kind: "conflict"},
		},
	}
	lines := fileLines(state, 80, Options{})
	if len(lines) < 2 || !strings.Contains(lines[0], "first.go") {
		t.Fatalf("conflict should be first: %#v", lines)
	}
	if !strings.Contains(lines[0], "CONFLICT") || !strings.Contains(lines[1], "WORKTREE") {
		t.Fatalf("file kind labels should be rendered: %#v", lines)
	}
}

func TestWorkspaceLinesRenderMeters(t *testing.T) {
	state := gitstate.State{Counts: gitstate.Counts{Staged: 2, Modified: 1, Untracked: 1}}
	lines := workspaceLines(state, Options{})
	out := strings.Join(lines, "\n")
	for _, want := range []string{"changes    4", "staged       2", "worktree     1", "untracked    1", "[#########.........]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace meters missing %q:\n%s", want, out)
		}
	}
}

func TestRenderNativeStatus(t *testing.T) {
	out := RenderNativeStatus("On branch main\nnothing to commit, working tree clean\n", Options{Width: 60, Height: 8})
	if !strings.Contains(out, "[git status]") || !strings.Contains(out, "On branch main") {
		t.Fatalf("native status did not render:\n%s", out)
	}
	assertFits(t, out, 60, 8)
}

func TestRenderNativeStatusExpandsTabs(t *testing.T) {
	status := "Changes to be committed:\n\tmodified:   README.md\n"
	out := RenderNativeStatus(status, Options{Width: 36, Height: 8})
	if strings.Contains(out, "\t") {
		t.Fatalf("native status should not contain raw tabs:\n%s", out)
	}
	assertFits(t, out, 36, 8)
}

func TestExpandTabsWithColor(t *testing.T) {
	line := "\x1b[32m\tmodified:\x1b[0m file"
	got := expandTabs(line, 8)
	if strings.Contains(got, "\t") {
		t.Fatalf("tab was not expanded: %q", got)
	}
	if !strings.Contains(got, "\x1b[32m") || !strings.Contains(got, "\x1b[0m") {
		t.Fatalf("ANSI color should be preserved: %q", got)
	}
	if visibleLen(got) != len("        modified: file") {
		t.Fatalf("visible width mismatch: got %d for %q", visibleLen(got), got)
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
	wide := tabBar(TabBranches, Options{Width: 140})
	if !strings.Contains(wide, "8:remote") || !strings.Contains(wide, "[5:branches]") {
		t.Fatalf("wide tab bar should include all tabs:\n%s", wide)
	}
	if visibleLen(wide) > 140 {
		t.Fatalf("wide tab bar overflowed: %d\n%s", visibleLen(wide), wide)
	}

	medium := tabBar(TabBranches, Options{Width: 70})
	if !strings.Contains(medium, "[5:branches]") || !strings.Contains(medium, "8:remote") {
		t.Fatalf("medium tab bar should include all tabs:\n%s", medium)
	}
	if visibleLen(medium) > 70 {
		t.Fatalf("medium tab bar overflowed: %d\n%s", visibleLen(medium), medium)
	}

	narrow := tabBar(TabBranches, Options{Width: 30})
	if !strings.Contains(narrow, "5/8") || !strings.Contains(narrow, "branches") {
		t.Fatalf("narrow tab bar should keep current tab:\n%s", narrow)
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
