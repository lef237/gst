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
