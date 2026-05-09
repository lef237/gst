package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/lef237/gst/internal/gitstate"
	"github.com/lef237/gst/internal/ui"
)

func main() {
	var (
		once     = flag.Bool("once", false, "print one snapshot and exit")
		interval = flag.Duration("interval", 2*time.Second, "refresh interval for the TUI")
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
	width, height := terminalSize()
	opts := gitstate.Options{LogLimit: *logLimit}

	if *once {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		printOnce(ctx, opts, ui.Options{Color: color, Width: width, Height: height})
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	restoreInput, err := enableCBreakMode()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gst: %v\n", err)
		os.Exit(1)
	}
	enterTUI()
	defer func() {
		leaveTUI()
		restoreInput()
	}()

	ticker := time.NewTicker(maxDuration(*interval, 500*time.Millisecond))
	defer ticker.Stop()
	keys := readKeys(ctx)
	active := ui.TabOverview
	lastFrame := ""
	forceDraw := true

	for {
		width, height = terminalSize()
		refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		frame, err := renderTab(refreshCtx, active, opts, ui.Options{Color: color, Width: width, Height: height, Interactive: true})
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "gst: %v\n", err)
			return
		}
		if forceDraw || frame != lastFrame {
			drawFrame(frame)
			lastFrame = frame
			forceDraw = false
		}

		select {
		case <-ctx.Done():
			return
		case key, ok := <-keys:
			if !ok {
				return
			}
			switch key {
			case "q", "Q", "\x03":
				return
			case "\t":
				if active == ui.TabHelp {
					active = ui.TabOverview
				} else {
					active = (active + 1) % ui.Tab(len(ui.Tabs()))
				}
				forceDraw = true
			case "?":
				active = ui.TabHelp
				forceDraw = true
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				next := ui.Tab(key[0] - '1')
				if int(next) < len(ui.Tabs()) {
					active = next
					forceDraw = true
				}
			case "r", "R":
				forceDraw = true
			}
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

func renderTab(ctx context.Context, tab ui.Tab, opts gitstate.Options, render ui.Options) (string, error) {
	state, err := gitstate.Collect(ctx, ".", opts)
	if err != nil {
		return "", err
	}
	return ui.RenderTab(state, tab, render), nil
}

func terminalSize() (int, int) {
	if size, ok := sttySize(); ok {
		return size.width, size.height
	}
	width := 100
	height := 32
	if cols := strings.TrimSpace(os.Getenv("COLUMNS")); cols != "" {
		var n int
		if _, err := fmt.Sscanf(cols, "%d", &n); err == nil && n >= 30 {
			width = n
		}
	}
	if lines := strings.TrimSpace(os.Getenv("LINES")); lines != "" {
		var n int
		if _, err := fmt.Sscanf(lines, "%d", &n); err == nil && n >= 8 {
			height = n
		}
	}
	return width, height
}

type termSize struct {
	width  int
	height int
}

func sttySize() (termSize, bool) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return termSize{}, false
	}
	var rows, cols int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &rows, &cols); err != nil {
		return termSize{}, false
	}
	if rows < 8 || cols < 30 {
		return termSize{}, false
	}
	return termSize{width: cols, height: rows}, true
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

func enableCBreakMode() (func(), error) {
	if !isTerminal(os.Stdin) {
		return func() {}, nil
	}
	before, err := stty("-g")
	if err != nil {
		return nil, fmt.Errorf("could not inspect terminal mode: %w", err)
	}
	if _, err := stty("-icanon", "-echo", "min", "1", "time", "0"); err != nil {
		return nil, fmt.Errorf("could not enter terminal input mode: %w", err)
	}
	return func() {
		_, _ = stty(strings.TrimSpace(before))
	}, nil
}

func enterTUI() {
	fmt.Print("\x1b[?1049h\x1b[?25l\x1b[H")
}

func leaveTUI() {
	fmt.Print("\x1b[?25h\x1b[?1049l")
}

func drawFrame(frame string) {
	fmt.Print("\x1b[H")
	for i, line := range strings.Split(frame, "\n") {
		if i > 0 {
			fmt.Print("\n")
		}
		fmt.Print("\x1b[2K")
		fmt.Print(line)
	}
	fmt.Print("\x1b[J")
}

func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func readKeys(ctx context.Context) <-chan string {
	keys := make(chan string, 8)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			b, err := reader.ReadByte()
			if err != nil {
				close(keys)
				return
			}
			select {
			case keys <- string([]byte{b}):
			case <-ctx.Done():
				return
			}
		}
	}()
	return keys
}
