package main

import (
	"bufio"
	"context"
	"strings"
	"testing"

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

func TestParseKeyDecodesArrowKeys(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "\x1b[C", want: "right"},
		{input: "\x1b[D", want: "left"},
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
