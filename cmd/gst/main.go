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
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("gst", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var (
		once     = flags.Bool("once", false, "print one snapshot and exit")
		interval = flags.Duration("interval", 2*time.Second, "refresh interval for the TUI")
		noColor  = flags.Bool("no-color", false, "disable ANSI colors")
		logLimit = flags.Int("log", 200, "number of commits to keep available in the graph")
		version  = flags.Bool("version", false, "print version and exit")
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *version {
		fmt.Println("gst dev")
		return 0
	}

	color := !*noColor && os.Getenv("NO_COLOR") == "" && isTerminal(os.Stdout)
	width, height := terminalSize()
	opts := gitstate.Options{LogLimit: *logLimit}

	if *once {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return printOnce(ctx, opts, ui.Options{Color: color, Width: width, Height: height})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	restoreInput, err := enableCBreakMode()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gst: %v\n", err)
		return 1
	}
	enterTUI()
	tuiActive := true
	cleanupTUI := func() {
		if !tuiActive {
			return
		}
		leaveTUI()
		restoreInput()
		tuiActive = false
	}
	defer cleanupTUI()
	failTUI := func(err error) int {
		cleanupTUI()
		fmt.Fprintf(os.Stderr, "gst: %v\n", err)
		return 1
	}

	ticker := time.NewTicker(max(*interval, 500*time.Millisecond))
	defer ticker.Stop()
	keys := readKeys(ctx)
	active := ui.TabOverview
	nativeStatus := false
	graphAll := false
	diffStaged := false
	scrolls := map[ui.Tab]int{}
	var state gitstate.State
	stateReady := false
	nativeOutput := ""
	nativeReady := false
	needsRefresh := true
	lastFrame := ""
	forceDraw := true

	for {
		width, height = terminalSize()
		renderOpts := ui.Options{Color: color, Width: width, Height: height, Interactive: true, GraphAll: graphAll, DiffStaged: diffStaged, Scroll: scrolls[active]}
		frame := ""
		if nativeStatus {
			if needsRefresh || !nativeReady {
				refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				status, err := gitstate.NativeStatus(refreshCtx, ".", renderOpts.Color)
				cancel()
				if err != nil {
					return failTUI(err)
				}
				nativeOutput = status
				nativeReady = true
			}
			frame = ui.RenderNativeStatus(nativeOutput, renderOpts)
		} else {
			if needsRefresh || !stateReady {
				refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				collectOpts := opts
				collectOpts.GraphAll = graphAll
				nextState, err := gitstate.Collect(refreshCtx, ".", collectOpts)
				cancel()
				if err != nil {
					return failTUI(err)
				}
				state = nextState
				stateReady = true
			}
			if canScroll(active, nativeStatus) {
				scrolls[active] = min(scrolls[active], ui.MaxScroll(state, active, renderOpts))
				renderOpts.Scroll = scrolls[active]
			}
			frame = ui.RenderTab(state, active, renderOpts)
		}
		needsRefresh = false
		if forceDraw || frame != lastFrame {
			drawFrame(frame)
			lastFrame = frame
			forceDraw = false
		}

		select {
		case <-ctx.Done():
			return 0
		case key, ok := <-keys:
			if !ok {
				return 0
			}
			switch key {
			case "q", "Q", "\x03":
				return 0
			case "\t", "right":
				active = nextTab(active)
				nativeStatus = false
				forceDraw = true
			case "left":
				active = previousTab(active)
				nativeStatus = false
				forceDraw = true
			case "down", "j", "J":
				if canScroll(active, nativeStatus) {
					scrolls[active]++
					forceDraw = true
				}
			case "up", "k", "K":
				if canScroll(active, nativeStatus) {
					scrolls[active] = max(0, scrolls[active]-1)
					forceDraw = true
				}
			case "pagedown", "f", "F":
				if canScroll(active, nativeStatus) {
					scrolls[active] += pageStep(height)
					forceDraw = true
				}
			case "pageup", "b", "B":
				if canScroll(active, nativeStatus) {
					scrolls[active] = max(0, scrolls[active]-pageStep(height))
					forceDraw = true
				}
			case "d", "D":
				if canScroll(active, nativeStatus) {
					scrolls[active] += halfPageStep(height)
					forceDraw = true
				}
			case "u", "U":
				if canScroll(active, nativeStatus) {
					scrolls[active] = max(0, scrolls[active]-halfPageStep(height))
					forceDraw = true
				}
			case "home":
				if canScroll(active, nativeStatus) {
					scrolls[active] = 0
					forceDraw = true
				}
			case "end":
				if canScroll(active, nativeStatus) {
					scrolls[active] = 1 << 30
					forceDraw = true
				}
			case "?":
				nativeStatus = false
				active = ui.TabHelp
				forceDraw = true
			case "t", "T":
				nativeStatus = !nativeStatus
				if nativeStatus && !nativeReady {
					needsRefresh = true
				}
				forceDraw = true
			case "a", "A":
				if active == ui.TabGraph && !nativeStatus {
					graphAll = !graphAll
					scrolls[ui.TabGraph] = 0
					stateReady = false
					needsRefresh = true
					forceDraw = true
				}
			case "s", "S":
				if active == ui.TabDiff && !nativeStatus {
					diffStaged = !diffStaged
					scrolls[ui.TabDiff] = 0
					forceDraw = true
				}
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				next := ui.Tab(key[0] - '1')
				if int(next) < len(ui.Tabs()) {
					nativeStatus = false
					active = next
					forceDraw = true
				}
			case "r", "R":
				needsRefresh = true
				nativeReady = false
				stateReady = false
				forceDraw = true
			}
		case <-ticker.C:
			needsRefresh = true
		}
	}
}

func printOnce(ctx context.Context, opts gitstate.Options, render ui.Options) int {
	state, err := gitstate.Collect(ctx, ".", opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gst: %v\n", err)
		return 1
	}
	fmt.Print(ui.Render(state, render))
	return 0
}

func nextTab(active ui.Tab) ui.Tab {
	if active == ui.TabHelp {
		return ui.TabOverview
	}
	return (active + 1) % ui.Tab(len(ui.Tabs()))
}

func previousTab(active ui.Tab) ui.Tab {
	if active == ui.TabHelp || active == ui.TabOverview {
		return ui.Tab(len(ui.Tabs()) - 1)
	}
	return active - 1
}

func canScroll(active ui.Tab, native bool) bool {
	if native {
		return false
	}
	return active != ui.TabOverview
}

func pageStep(height int) int {
	return max(1, height-6)
}

func halfPageStep(height int) int {
	return max(1, pageStep(height)/2)
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
	keys := make(chan string, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			b, err := reader.ReadByte()
			if err != nil {
				close(keys)
				return
			}
			key, err := parseKey(reader, b)
			if err != nil {
				close(keys)
				return
			}
			if key == "" {
				continue
			}
			if !enqueueLatest(ctx, keys, key) {
				return
			}
		}
	}()
	return keys
}

func parseKey(reader *bufio.Reader, b byte) (string, error) {
	if b != '\x1b' {
		return string([]byte{b}), nil
	}

	next, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	if next != '[' && next != 'O' {
		return "", nil
	}

	final, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	switch final {
	case 'A':
		return "up", nil
	case 'B':
		return "down", nil
	case 'C':
		return "right", nil
	case 'D':
		return "left", nil
	case 'F':
		return "end", nil
	case 'H':
		return "home", nil
	case '5', '6':
		tilde, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		if tilde != '~' {
			return "", nil
		}
		if final == '5' {
			return "pageup", nil
		}
		return "pagedown", nil
	default:
		return "", nil
	}
}

func enqueueLatest(ctx context.Context, keys chan string, key string) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}

	select {
	case keys <- key:
		return true
	default:
	}

	select {
	case <-keys:
	default:
	}

	select {
	case keys <- key:
		return true
	case <-ctx.Done():
		return false
	}
}
