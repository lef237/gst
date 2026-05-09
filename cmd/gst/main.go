package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/lef237/gst/internal/gitstate"
	"github.com/lef237/gst/internal/ui"
)

func main() {
	var (
		watch    = flag.Bool("watch", false, "refresh the dashboard continuously")
		interval = flag.Duration("interval", 2*time.Second, "refresh interval for --watch")
		noColor  = flag.Bool("no-color", false, "disable ANSI colors")
		logLimit = flag.Int("log", 18, "number of commits to show in the graph")
		version  = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *version {
		fmt.Println("gst dev")
		return
	}

	color := !*noColor && os.Getenv("NO_COLOR") == "" && isTerminal(os.Stdout)
	width := terminalWidth()
	opts := gitstate.Options{LogLimit: *logLimit}

	if !*watch {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		printOnce(ctx, opts, ui.Options{Color: color, Width: width})
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ticker := time.NewTicker(maxDuration(*interval, 500*time.Millisecond))
	defer ticker.Stop()

	for {
		fmt.Print("\x1b[2J\x1b[H")
		refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		printOnce(refreshCtx, opts, ui.Options{Color: color, Width: width})
		cancel()
		fmt.Printf("\n%s\n", dim(color, "watching: press Ctrl-C to quit"))

		select {
		case <-ctx.Done():
			fmt.Println()
			return
		case <-ticker.C:
		}
	}
}

func printOnce(ctx context.Context, opts gitstate.Options, render ui.Options) {
	state, err := gitstate.Collect(ctx, ".", opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gst: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(ui.Render(state, render))
}

func terminalWidth() int {
	if cols := strings.TrimSpace(os.Getenv("COLUMNS")); cols != "" {
		var n int
		if _, err := fmt.Sscanf(cols, "%d", &n); err == nil && n >= 60 {
			return n
		}
	}
	return 100
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func dim(color bool, s string) string {
	if !color {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}
