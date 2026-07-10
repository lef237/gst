package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/lef237/gst/internal/gitstate"
	"github.com/lef237/gst/internal/ui"
)

// version is overridable at build time with
// -ldflags "-X main.version=v0.1.0"; otherwise it is read from the module
// build info so `go install ...@v0.1.0` reports the installed tag.
var version = ""

func main() {
	os.Exit(run(os.Args[1:]))
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
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
		fmt.Println("gst " + buildVersion())
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
	resizes := make(chan os.Signal, 1)
	stopResize := notifyResize(resizes)
	defer stopResize()
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
	stateRefreshes := make(chan stateRefreshResult, 1)
	nativeRefreshes := make(chan nativeRefreshResult, 1)
	active := ui.TabOverview
	nativeStatus := false
	graphAll := false
	diffStaged := false
	scrolls := map[ui.Tab]int{}
	var state gitstate.State
	stateReady := false
	nativeOutput := ""
	nativeReady := false
	stateRefreshPending := true
	stateRefreshing := false
	stateRefreshID := 0
	nativeRefreshPending := false
	nativeRefreshing := false
	nativeRefreshID := 0
	drawer := frameDrawer{}
	lastFrame := ""
	forceDraw := true
	notice := ""
	selecting := false

	for {
		renderOpts := ui.Options{Color: color, Width: width, Height: height, Interactive: true, GraphAll: graphAll, DiffStaged: diffStaged, Scroll: scrolls[active], Notice: notice, Selecting: selecting}
		frame := ""
		if nativeStatus {
			if (nativeRefreshPending || !nativeReady) && !nativeRefreshing {
				nativeRefreshPending = false
				nativeRefreshing = true
				nativeRefreshID++
				startNativeRefresh(ctx, nativeRefreshID, renderOpts.Color, nativeRefreshes)
			}
			if nativeReady {
				frame = ui.RenderNativeStatus(nativeOutput, renderOpts)
			} else {
				frame = loadingFrame("loading git status...", width, height)
			}
		} else {
			if (stateRefreshPending || !stateReady) && !stateRefreshing {
				stateRefreshPending = false
				stateRefreshing = true
				stateRefreshID++
				startStateRefresh(ctx, stateRefreshID, opts, graphAll, stateRefreshes)
			}
			if stateReady {
				if canScroll(active, nativeStatus) {
					scrolls[active] = min(scrolls[active], ui.MaxScroll(state, active, renderOpts))
					renderOpts.Scroll = scrolls[active]
				}
				frame = ui.RenderTab(state, active, renderOpts)
			} else {
				frame = loadingFrame("loading repository state...", width, height)
			}
		}
		if forceDraw || frame != lastFrame {
			drawer.draw(frame)
			lastFrame = frame
			forceDraw = false
		}

		select {
		case <-ctx.Done():
			return 0
		case result := <-stateRefreshes:
			stateRefreshing = false
			if result.id != stateRefreshID {
				continue
			}
			if result.graphAll != graphAll {
				stateRefreshPending = true
				forceDraw = true
				continue
			}
			if result.err != nil {
				return failTUI(result.err)
			}
			state = result.state
			stateReady = true
			if !selecting {
				forceDraw = true
			}
		case result := <-nativeRefreshes:
			nativeRefreshing = false
			if result.id != nativeRefreshID {
				continue
			}
			if result.err != nil {
				return failTUI(result.err)
			}
			nativeOutput = result.output
			nativeReady = true
			if !selecting {
				forceDraw = true
			}
		case key, ok := <-keys:
			if !ok {
				return 0
			}
			if selecting {
				switch key {
				case "v", "V":
					selecting = false
					setMouseCapture(true)
					notice = "text selection off"
					stateRefreshPending = true
					forceDraw = true
				case "q", "Q", "\x03":
					return 0
				}
				continue
			}
			if notice != "" {
				notice = ""
				forceDraw = true
			}
			if next, ok := tabClick(key, active, nativeStatus, stateReady, width); ok {
				active = next
				nativeStatus = false
				forceDraw = true
				continue
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
			case "wheeldown":
				if canScroll(active, nativeStatus) {
					scrolls[active] += wheelStep
					forceDraw = true
				}
			case "wheelup":
				if canScroll(active, nativeStatus) {
					scrolls[active] = max(0, scrolls[active]-wheelStep)
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
					nativeRefreshPending = true
				}
				forceDraw = true
			case "a", "A":
				if active == ui.TabGraph && !nativeStatus {
					graphAll = !graphAll
					scrolls[ui.TabGraph] = 0
					stateReady = false
					stateRefreshPending = true
					forceDraw = true
				} else if active == ui.TabDiff && !nativeStatus {
					notice = copyDiff(ctx, state, copyAllDiffs)
					forceDraw = true
				}
			case "s", "S":
				if active == ui.TabDiff && !nativeStatus {
					diffStaged = !diffStaged
					scrolls[ui.TabDiff] = 0
					forceDraw = true
				}
			case "y", "Y":
				if active == ui.TabDiff && !nativeStatus {
					notice = copyDiff(ctx, state, copyWorktreeDiff)
					forceDraw = true
				}
			case "i", "I":
				if active == ui.TabDiff && !nativeStatus {
					notice = copyDiff(ctx, state, copyStagedDiff)
					forceDraw = true
				}
			case "v", "V":
				selecting = true
				setMouseCapture(false)
				notice = "text selection on — drag to select & copy; press v to resume"
				forceDraw = true
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				next := ui.Tab(key[0] - '1')
				if int(next) < len(ui.Tabs()) {
					nativeStatus = false
					active = next
					forceDraw = true
				}
			case "r", "R":
				if nativeStatus {
					nativeReady = false
					nativeRefreshPending = true
				} else {
					stateRefreshPending = true
				}
				forceDraw = true
			}
		case <-ticker.C:
			if selecting {
				continue
			}
			if nativeStatus {
				nativeRefreshPending = true
			} else {
				stateRefreshPending = true
			}
		case <-resizes:
			nextWidth, nextHeight := terminalSize()
			if nextWidth != width || nextHeight != height {
				width, height = nextWidth, nextHeight
				forceDraw = true
			}
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

type stateRefreshResult struct {
	id       int
	graphAll bool
	state    gitstate.State
	err      error
}

type nativeRefreshResult struct {
	id     int
	output string
	err    error
}

func startStateRefresh(ctx context.Context, id int, opts gitstate.Options, graphAll bool, results chan<- stateRefreshResult) {
	go func() {
		refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		collectOpts := opts
		collectOpts.GraphAll = graphAll
		state, err := gitstate.Collect(refreshCtx, ".", collectOpts)
		select {
		case results <- stateRefreshResult{id: id, graphAll: graphAll, state: state, err: err}:
		case <-ctx.Done():
		}
	}()
}

func startNativeRefresh(ctx context.Context, id int, color bool, results chan<- nativeRefreshResult) {
	go func() {
		refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		output, err := gitstate.NativeStatus(refreshCtx, ".", color)
		select {
		case results <- nativeRefreshResult{id: id, output: output, err: err}:
		case <-ctx.Done():
		}
	}()
}

func loadingFrame(message string, width, height int) string {
	if width < 30 {
		width = 30
	}
	if height < 8 {
		height = 8
	}
	lines := make([]string, height)
	lines[0] = fitPlain(" gst ", width)
	if height > 2 {
		lines[2] = fitPlain(message, width)
	}
	return strings.Join(lines, "\n")
}

func fitPlain(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return ""
	}
	return s[:width-1] + "."
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

func tabClick(key string, active ui.Tab, nativeStatus, stateReady bool, width int) (ui.Tab, bool) {
	if nativeStatus || !stateReady {
		return ui.TabOverview, false
	}
	row, col, ok := parseMouseKey(key)
	if !ok || row != 2 {
		return ui.TabOverview, false
	}
	return ui.TabAtColumn(active, ui.Options{Width: width, Interactive: true}, col)
}

type diffCopyTarget int

const (
	copyWorktreeDiff diffCopyTarget = iota
	copyStagedDiff
	copyAllDiffs
)

func copyDiff(ctx context.Context, state gitstate.State, target diffCopyTarget) string {
	label, text := diffClipboardPayload(state, target)
	if text == "" {
		return "nothing to copy: " + label + " is empty"
	}

	copyCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := writeClipboard(copyCtx, text); err != nil {
		return "copy failed: " + err.Error()
	}
	return "copied " + label
}

func diffClipboardPayload(state gitstate.State, target diffCopyTarget) (string, string) {
	switch target {
	case copyStagedDiff:
		return "staged diff", joinDiffSections(state.StagedDiff)
	case copyAllDiffs:
		return "full diff", joinDiffSections(state.HeadDiff)
	default:
		return "worktree diff", joinDiffSections(state.WorktreeDiff)
	}
}

func joinDiffSections(sections ...[]string) string {
	var chunks []string
	for _, section := range sections {
		if len(section) == 0 {
			continue
		}
		chunks = append(chunks, strings.Join(section, "\n"))
	}
	if len(chunks) == 0 {
		return ""
	}
	return strings.Join(chunks, "\n\n") + "\n"
}

const wheelStep = 3

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

const mouseCaptureOn = "\x1b[?1000h\x1b[?1006h"
const mouseCaptureOff = "\x1b[?1006l\x1b[?1000l"

func enterTUI() {
	fmt.Print("\x1b[?1049h\x1b[?25l" + mouseCaptureOn + "\x1b[H")
}

func leaveTUI() {
	fmt.Print(mouseCaptureOff + "\x1b[?25h\x1b[?1049l")
}

func setMouseCapture(on bool) {
	if on {
		fmt.Print(mouseCaptureOn)
	} else {
		fmt.Print(mouseCaptureOff)
	}
}

type frameDrawer struct {
	lines []string
}

func (d *frameDrawer) draw(frame string) {
	if out := d.render(frame); out != "" {
		fmt.Print(out)
	}
}

func (d *frameDrawer) render(frame string) string {
	next := strings.Split(frame, "\n")
	var out strings.Builder
	out.Grow(len(frame) + len(next)*10)
	if len(d.lines) == 0 {
		out.WriteString("\x1b[H")
		for i, line := range next {
			if i > 0 {
				out.WriteString("\n")
			}
			out.WriteString("\x1b[2K")
			out.WriteString(line)
		}
		out.WriteString("\x1b[J")
		d.lines = next
		return out.String()
	}

	coveredRows, forcedRows := d.writeScrollShift(&out, next)
	rows := max(len(d.lines), len(next))
	for i := 0; i < rows; i++ {
		if coveredRows[i] {
			continue
		}
		oldLine := ""
		if i < len(d.lines) {
			oldLine = d.lines[i]
		}
		newLine := ""
		if i < len(next) {
			newLine = next[i]
		}
		if oldLine == newLine && !forcedRows[i] {
			continue
		}
		writeMoveClear(&out, i+1)
		out.WriteString(newLine)
	}
	d.lines = next
	return out.String()
}

func writeMoveClear(out *strings.Builder, row int) {
	out.WriteString("\x1b[")
	out.WriteString(strconv.Itoa(row))
	out.WriteString(";1H\x1b[2K")
}

type scrollShift struct {
	runStart    int
	runEnd      int
	regionStart int
	regionEnd   int
	edgeRow     int
	direction   int
}

func (d *frameDrawer) writeScrollShift(out *strings.Builder, next []string) ([]bool, []bool) {
	rows := max(len(d.lines), len(next))
	coveredRows := make([]bool, rows)
	forcedRows := make([]bool, rows)
	shift, ok := findScrollShift(d.lines, next)
	if !ok {
		return coveredRows, forcedRows
	}

	writeScrollRegion(out, shift)
	for i := shift.runStart; i <= shift.runEnd; i++ {
		coveredRows[i] = true
	}
	forcedRows[shift.edgeRow] = true
	return coveredRows, forcedRows
}

func findScrollShift(oldLines, nextLines []string) (scrollShift, bool) {
	if len(oldLines) != len(nextLines) {
		return scrollShift{}, false
	}
	up := longestScrollShift(oldLines, nextLines, 1)
	down := longestScrollShift(oldLines, nextLines, -1)
	if down.runLength() > up.runLength() {
		up = down
	}
	if up.runLength() < 3 {
		return scrollShift{}, false
	}
	return up, true
}

func longestScrollShift(oldLines, nextLines []string, direction int) scrollShift {
	bestStart := 0
	bestLen := 0
	currentStart := 0
	currentLen := 0
	for i := 0; i < len(nextLines); i++ {
		oldIndex := i + direction
		matches := oldIndex >= 0 && oldIndex < len(oldLines) && nextLines[i] == oldLines[oldIndex]
		if matches {
			if currentLen == 0 {
				currentStart = i
			}
			currentLen++
			if currentLen > bestLen {
				bestStart = currentStart
				bestLen = currentLen
			}
			continue
		}
		currentLen = 0
	}
	if bestLen == 0 {
		return scrollShift{}
	}

	shift := scrollShift{runStart: bestStart, runEnd: bestStart + bestLen - 1, direction: direction}
	if direction > 0 {
		shift.regionStart = shift.runStart
		shift.regionEnd = shift.runEnd + 1
		shift.edgeRow = shift.regionEnd
	} else {
		shift.regionStart = shift.runStart - 1
		shift.regionEnd = shift.runEnd
		shift.edgeRow = shift.regionStart
	}
	if shift.regionStart < 0 || shift.regionEnd >= len(nextLines) {
		return scrollShift{}
	}
	return shift
}

func (s scrollShift) runLength() int {
	if s.direction == 0 || s.runEnd < s.runStart {
		return 0
	}
	return s.runEnd - s.runStart + 1
}

func writeScrollRegion(out *strings.Builder, shift scrollShift) {
	out.WriteString("\x1b[")
	out.WriteString(strconv.Itoa(shift.regionStart + 1))
	out.WriteString(";")
	out.WriteString(strconv.Itoa(shift.regionEnd + 1))
	out.WriteString("r\x1b[")
	out.WriteString(strconv.Itoa(shift.regionStart + 1))
	out.WriteString(";1H")
	if shift.direction > 0 {
		out.WriteString("\x1b[S")
	} else {
		out.WriteString("\x1b[T")
	}
	out.WriteString("\x1b[r")
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
	switch next {
	case '[':
		return parseCSIKey(reader)
	case 'O':
		final, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		return keyFromSS3(final), nil
	default:
		return "", nil
	}
}

func parseCSIKey(reader *bufio.Reader) (string, error) {
	var seq strings.Builder
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		seq.WriteByte(b)
		if b >= 0x40 && b <= 0x7e {
			break
		}
	}

	s := seq.String()
	if s == "M" {
		return parseLegacyMouse(reader)
	}
	if strings.HasPrefix(s, "<") {
		return parseSGRMouse(s), nil
	}

	switch s {
	case "A":
		return "up", nil
	case "B":
		return "down", nil
	case "C":
		return "right", nil
	case "D":
		return "left", nil
	case "F":
		return "end", nil
	case "H":
		return "home", nil
	case "5~":
		return "pageup", nil
	case "6~":
		return "pagedown", nil
	default:
		return "", nil
	}
}

func keyFromSS3(final byte) string {
	switch final {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'C':
		return "right"
	case 'D':
		return "left"
	case 'F':
		return "end"
	case 'H':
		return "home"
	default:
		return ""
	}
}

const mouseKeyPrefix = "mouse:"

func mouseKey(row, col int) string {
	return fmt.Sprintf("%s%d:%d", mouseKeyPrefix, row, col)
}

func parseMouseKey(key string) (int, int, bool) {
	if !strings.HasPrefix(key, mouseKeyPrefix) {
		return 0, 0, false
	}
	coords := strings.TrimPrefix(key, mouseKeyPrefix)
	rowText, colText, ok := strings.Cut(coords, ":")
	if !ok {
		return 0, 0, false
	}
	row, rowErr := strconv.Atoi(rowText)
	col, colErr := strconv.Atoi(colText)
	if rowErr != nil || colErr != nil {
		return 0, 0, false
	}
	return row, col, true
}

func parseSGRMouse(seq string) string {
	if len(seq) < 2 {
		return ""
	}
	final := seq[len(seq)-1]
	if final != 'M' {
		return ""
	}
	body := seq[1 : len(seq)-1]
	parts := strings.Split(body, ";")
	if len(parts) != 3 {
		return ""
	}
	button, buttonErr := strconv.Atoi(parts[0])
	col, colErr := strconv.Atoi(parts[1])
	row, rowErr := strconv.Atoi(parts[2])
	if buttonErr != nil || colErr != nil || rowErr != nil {
		return ""
	}
	return mousePressKey(button, row, col)
}

func parseLegacyMouse(reader *bufio.Reader) (string, error) {
	buttonByte, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	colByte, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	rowByte, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	button := int(buttonByte) - 32
	col := int(colByte) - 32
	row := int(rowByte) - 32
	return mousePressKey(button, row, col), nil
}

func mousePressKey(button, row, col int) string {
	if row <= 0 || col <= 0 {
		return ""
	}
	if button&32 != 0 {
		return ""
	}
	if button&64 != 0 {
		switch button & 3 {
		case 0:
			return "wheelup"
		case 1:
			return "wheeldown"
		}
		return ""
	}
	if button&3 != 0 {
		return ""
	}
	return mouseKey(row, col)
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
