package main

import (
	"context"
	"testing"
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
