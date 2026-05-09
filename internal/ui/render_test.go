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

	if !strings.Contains(out, "[8:help]") {
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
