package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lef237/gst/internal/gitstate"
)

type Options struct {
	Color       bool
	Width       int
	Height      int
	Interactive bool
	GraphAll    bool
	DiffStaged  bool
	Scroll      int
	Notice      string
}

type Tab int

const (
	TabOverview Tab = iota
	TabGraph
	TabFiles
	TabDiff
	TabBranches
	TabStash
	TabRefs
	TabRemote
	TabHelp
)

func Tabs() []string {
	return []string{"overview", "graph", "files", "diff", "branches", "stash", "refs", "remote"}
}

func Render(state gitstate.State, opts Options) string {
	return RenderTab(state, TabOverview, opts)
}

func RenderTab(state gitstate.State, active Tab, opts Options) string {
	opts = normalizeOptions(opts)

	prefix := []string{header(state, opts)}
	if opts.Interactive {
		prefix = append(prefix, tabBar(active, opts))
	}

	footer := footerLines(active, opts)
	bodyHeight := tabBodyHeight(opts, len(footer))
	body := renderTabBody(state, active, opts, bodyHeight)
	if opts.Interactive {
		return composeWithFooter(prefix, body, footer, opts.Height)
	}

	var out strings.Builder
	for i, line := range prefix {
		if i > 0 {
			out.WriteString("\n")
		}
		out.WriteString(line)
	}
	out.WriteString("\n")
	out.WriteString(body)
	return trimToHeight(out.String(), opts.Height)
}

func renderTabBody(state gitstate.State, active Tab, opts Options, bodyHeight int) string {
	switch active {
	case TabGraph:
		title := "commit graph"
		if opts.GraphAll {
			title = "commit graph --all"
		}
		return graphScrollPanel(title, state, opts.Width, bodyHeight, opts)
	case TabFiles:
		return scrollPanel("changed files", fileLines(state, opts.Width-4, opts), opts.Width, bodyHeight, opts)
	case TabDiff:
		title := "diff worktree"
		if opts.DiffStaged {
			title = "diff staged"
		}
		return diffScrollPanel(title, state, opts.Width, bodyHeight, opts)
	case TabBranches:
		return scrollPanel("branches", branchLines(state, opts.Width-4, opts), opts.Width, bodyHeight, opts)
	case TabStash:
		return scrollPanel("stash", stashLines(state, opts.Width-4, opts), opts.Width, bodyHeight, opts)
	case TabRefs:
		return scrollPanel("refs", refLines(state, opts.Width-4, opts), opts.Width, bodyHeight, opts)
	case TabRemote:
		return scrollPanel("repository notes", noteLines(state, opts.Width-4, opts), opts.Width, bodyHeight, opts)
	case TabHelp:
		return scrollPanel("help", helpLines(state, opts.Width-4, opts), opts.Width, bodyHeight, opts)
	default:
		return overview(state, opts, bodyHeight)
	}
}

func MaxScroll(state gitstate.State, active Tab, opts Options) int {
	opts = normalizeOptions(opts)
	footerRows := 0
	if opts.Interactive {
		footerRows = len(footerLines(active, opts))
	}
	bodyHeight := tabBodyHeight(opts, footerRows)
	rows := bodyHeight - 2
	switch active {
	case TabGraph:
		return max(0, graphTabLineCount(state)-rows)
	case TabFiles:
		return max(0, len(fileLines(state, opts.Width-4, opts))-rows)
	case TabDiff:
		return max(0, diffLineCount(state, opts)-rows)
	case TabBranches:
		return max(0, len(branchLines(state, opts.Width-4, opts))-rows)
	case TabStash:
		return max(0, len(stashLines(state, opts.Width-4, opts))-rows)
	case TabRefs:
		return max(0, len(refLines(state, opts.Width-4, opts))-rows)
	case TabRemote:
		return max(0, len(noteLines(state, opts.Width-4, opts))-rows)
	case TabHelp:
		return max(0, len(helpLines(state, opts.Width-4, opts))-rows)
	default:
		return 0
	}
}

func RenderNativeStatus(status string, opts Options) string {
	opts = normalizeOptions(opts)
	var footer []string
	bodyHeight := opts.Height - 2
	if opts.Interactive {
		footer = nativeStatusFooterLines(opts)
		bodyHeight = opts.Height - 1 - len(footer)
	}
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	body := panel("git status", nativeStatusLines(status), opts.Width, bodyHeight, opts)
	if opts.Interactive {
		return composeWithFooter([]string{nativeStatusBar(opts)}, body, footer, opts.Height)
	}

	var out strings.Builder
	out.WriteString(nativeStatusBar(opts))
	out.WriteString("\n")
	out.WriteString(body)
	return trimToHeight(out.String(), opts.Height)
}

func normalizeOptions(opts Options) Options {
	if opts.Width < 30 {
		opts.Width = 30
	}
	if opts.Height < 8 {
		opts.Height = 8
	}
	return opts
}

func tabBodyHeight(opts Options, footerRows int) int {
	if opts.Interactive {
		return max(3, opts.Height-2-footerRows)
	}
	if opts.Height-2 < 6 {
		return 6
	}
	return opts.Height - 2
}

func composeWithFooter(prefix []string, body string, footer []string, height int) string {
	if height <= 0 {
		return ""
	}
	lines := append([]string(nil), prefix...)
	bodyLimit := max(0, height-len(lines)-len(footer))
	lines = append(lines, blockLines(trimToHeight(body, bodyLimit))...)
	for len(lines) < height-len(footer) {
		lines = append(lines, "")
	}
	lines = append(lines, footer...)
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func blockLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func overview(state gitstate.State, opts Options, bodyHeight int) string {
	if opts.Width < 90 {
		return narrowOverview(state, opts, bodyHeight)
	}

	var out strings.Builder
	leftW := opts.Width/2 - 1
	rightW := opts.Width - leftW - 2
	topHeight := 7
	bottomHeight := max(5, bodyHeight-topHeight-2)

	if lines := attentionLines(state, opts); len(lines) > 0 {
		alertHeight := min(max(5, len(lines)+2), max(5, bodyHeight-6))
		out.WriteString(panel("attention", lines, opts.Width, alertHeight, opts))
		out.WriteString("\n")
		bodyHeight -= alertHeight + 1
		topHeight = min(7, max(4, bodyHeight/2))
		bottomHeight = max(4, bodyHeight-topHeight-1)
	}

	out.WriteString(joinPanels(
		panel("sync", syncLines(state, opts), leftW, topHeight, opts),
		panel("workspace", workspaceLines(state, opts), rightW, topHeight, opts),
	))
	out.WriteString("\n")
	out.WriteString(joinPanels(
		panel("changed files", fileLines(state, leftW-4, opts), leftW, bottomHeight, opts),
		panel("commit graph", graphLines(state, rightW-4, opts), rightW, bottomHeight, opts),
	))
	return out.String()
}

func narrowOverview(state gitstate.State, opts Options, bodyHeight int) string {
	var out strings.Builder
	if lines := attentionLines(state, opts); len(lines) > 0 {
		alertHeight := min(max(5, len(lines)+2), max(5, bodyHeight-4))
		out.WriteString(panel("attention", lines, opts.Width, alertHeight, opts))
		out.WriteString("\n")
		bodyHeight -= alertHeight + 1
	}

	sync := syncLines(state, opts)
	workspace := workspaceLines(state, opts)
	files := fileLines(state, opts.Width-4, opts)
	syncHeight, workspaceHeight, filesHeight := narrowPanelHeights(bodyHeight, len(sync), len(workspace))

	out.WriteString(panel("sync", sync, opts.Width, syncHeight, opts))
	out.WriteString("\n")
	out.WriteString(panel("workspace", workspace, opts.Width, workspaceHeight, opts))
	out.WriteString("\n")
	out.WriteString(panel("changed files", files, opts.Width, filesHeight, opts))
	return out.String()
}

func narrowPanelHeights(bodyHeight, syncRows, workspaceRows int) (int, int, int) {
	const (
		gapRows        = 2
		minPanelHeight = 3
	)
	syncHeight := min(max(minPanelHeight, syncRows+2), max(4, bodyHeight/3+2))
	workspaceHeight := min(max(minPanelHeight, workspaceRows+2), max(4, bodyHeight/3+3))
	filesHeight := bodyHeight - syncHeight - workspaceHeight - gapRows
	for filesHeight < minPanelHeight && workspaceHeight > minPanelHeight {
		workspaceHeight--
		filesHeight++
	}
	for filesHeight < minPanelHeight && syncHeight > minPanelHeight {
		syncHeight--
		filesHeight++
	}
	if filesHeight < minPanelHeight {
		filesHeight = minPanelHeight
	}
	return syncHeight, workspaceHeight, filesHeight
}

func tabBar(active Tab, opts Options) string {
	if active == TabHelp {
		return helpTabBar(opts)
	}

	tabs := Tabs()
	var parts []string
	for i, name := range tabs {
		label := fmt.Sprintf("%d:%s", i+1, name)
		if Tab(i) == active {
			label = color(opts, "["+label+"]", cyanBold)
		} else {
			label = color(opts, label, dim)
		}
		parts = append(parts, label)
	}

	line := strings.Join(parts, " ")
	if visibleLen(line) <= opts.Width {
		return line
	}

	return compactTabBar(active, tabs, opts)
}

func compactTabBar(active Tab, tabs []string, opts Options) string {
	prefix := fmt.Sprintf("[%d/%d ", int(active)+1, len(tabs))
	suffix := "]"
	nameWidth := opts.Width - visibleLen(prefix) - visibleLen(suffix)
	if nameWidth < 1 {
		return truncate(fmt.Sprintf("[%d/%d]", int(active)+1, len(tabs)), opts.Width)
	}
	name := truncate(tabs[int(active)], nameWidth)
	return color(opts, prefix+name+suffix, cyanBold)
}

func helpTabBar(opts Options) string {
	line := "1:overview 2:graph 3:files 4:diff 5:branches 6:stash 7:refs 8:remote [? help]"
	if visibleLen(line) <= opts.Width {
		return line
	}
	return truncate("[? help]", opts.Width)
}

func nativeStatusBar(opts Options) string {
	line := "[git status]"
	return truncate(line, opts.Width)
}

func footerLines(active Tab, opts Options) []string {
	lines := wrapFooterItems("[keys]", tabKeyItems(active, opts), opts.Width)
	for i, line := range lines {
		lines[i] = color(opts, line, dim)
	}
	if opts.Notice == "" {
		return lines
	}

	notice := wrapFooterText("[notice] "+opts.Notice, opts.Width)
	out := make([]string, 0, len(notice)+len(lines))
	for _, line := range notice {
		out = append(out, color(opts, line, yellow))
	}
	return append(out, lines...)
}

func tabKeyItems(active Tab, opts Options) []string {
	compact := opts.Width < 100
	switch active {
	case TabOverview:
		if compact {
			return []string{"left/right:tabs", "1-8", "t:native", "?", "r", "q"}
		}
		return []string{"left/right:tabs", "1-8:jump", "t:native", "?:help", "r:refresh", "q:quit"}
	case TabGraph:
		mode := "a --all"
		if opts.GraphAll {
			mode = "a normal"
		}
		if compact {
			return []string{strings.ReplaceAll(mode, " ", ":"), "j/k", "d/u", "f/b", "left/right:tabs", "t:native", "?", "r", "q"}
		}
		return []string{strings.ReplaceAll(mode, " ", ":"), "j/k:line", "d/u:half", "f/b:page", "home/end:edge", "left/right:tabs", "t:native", "?:help", "r:refresh", "q:quit"}
	case TabDiff:
		if compact {
			return []string{"y:wt", "i:stg", "a:full", "s:toggle", "j/k", "d/u", "f/b", "left/right:tabs", "?", "r", "q"}
		}
		return []string{"y:copy-worktree", "i:copy-staged", "a:copy-full", "s:toggle", "j/k:line", "d/u:half", "f/b:page", "left/right:tabs", "?:help", "r:refresh", "q:quit"}
	case TabHelp:
		if compact {
			return []string{"j/k", "d/u", "f/b", "left/right", "1-8", "q"}
		}
		return []string{"j/k:line", "d/u:half", "f/b:page", "home/end:edge", "left/right:tabs", "1-8:jump", "q:quit"}
	default:
		if compact {
			return []string{"j/k", "d/u", "f/b", "left/right:tabs", "t:native", "?", "r", "q"}
		}
		return []string{"j/k:line", "d/u:half", "f/b:page", "home/end:edge", "left/right:tabs", "t:native", "?:help", "r:refresh", "q:quit"}
	}
}

func nativeStatusFooterLines(opts Options) []string {
	lines := wrapFooterItems("[keys]", []string{"t:back", "r:refresh", "q:quit"}, opts.Width)
	for i, line := range lines {
		lines[i] = color(opts, line, dim)
	}
	return lines
}

func wrapFooterItems(label string, items []string, width int) []string {
	if len(items) == 0 {
		return []string{truncate(label, width)}
	}
	prefix := label + " "
	indent := strings.Repeat(" ", visibleLen(prefix))
	var lines []string
	current := prefix
	for _, item := range items {
		part := item
		if current != prefix && current != indent {
			part = ", " + item
		}
		if visibleLen(current)+visibleLen(part) <= width {
			current += part
			continue
		}
		lines = append(lines, current)
		current = indent + item
	}
	lines = append(lines, current)
	return lines
}

func wrapFooterText(text string, width int) []string {
	if visibleLen(text) <= width {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		part := " " + word
		if visibleLen(current)+visibleLen(part) <= width {
			current += part
			continue
		}
		lines = append(lines, current)
		current = word
	}
	lines = append(lines, current)
	return lines
}

func nativeStatusLines(status string) []string {
	lines := strings.Split(strings.TrimRight(status, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{"git status produced no output"}
	}
	for i, line := range lines {
		lines[i] = expandTabs(strings.TrimRight(line, "\r"), 8)
	}
	return lines
}

func expandTabs(s string, tabWidth int) string {
	if tabWidth <= 0 || !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\t' {
			spaces := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", spaces))
			col += spaces
			continue
		}
		b.WriteRune(r)
		col += runeWidth(r)
	}
	return b.String()
}

func header(state gitstate.State, opts Options) string {
	branch := state.Branch
	if branch == "" {
		branch = "(unknown)"
	}
	title := " gst "
	head := state.Head
	if head == "" {
		head = "no commits"
	}
	status := "clean"
	if state.Counts.Conflicted > 0 {
		status = color(opts, "conflicts", red)
	} else if dirty(state) {
		status = color(opts, fmt.Sprintf("changes:%d", totalChanges(state.Counts)), yellow)
	} else {
		status = color(opts, "clean", green)
	}
	counters := fmt.Sprintf("S:%d W:%d U:%d C:%d", state.Counts.Staged, state.Counts.Modified, state.Counts.Untracked, state.Counts.Conflicted)
	line := fmt.Sprintf("%s %s on %s @ %s  %s  %s", color(opts, title, cyanBold), status, color(opts, branch, whiteBold), head, color(opts, counters, dim), state.RepoRoot)
	return truncate(line, opts.Width)
}

func syncLines(state gitstate.State, opts Options) []string {
	upstream := state.Upstream
	if upstream == "" {
		upstream = "(no upstream)"
	}
	lines := []string{
		kv("local", state.Branch),
		kv("remote", upstream),
	}
	if state.Upstream == "" {
		lines = append(lines, color(opts, "local only: set an upstream before push/pull can synchronize this branch", yellow))
		return lines
	}

	lines = append(lines, fmt.Sprintf("%-10s +%d / -%d", "distance", state.Ahead, state.Behind))
	switch {
	case state.Ahead == 0 && state.Behind == 0:
		lines = append(lines, color(opts, "local and remote point at the same commit", green))
	case state.Ahead > 0 && state.Behind == 0:
		lines = append(lines, color(opts, fmt.Sprintf("local is ahead by %d commit(s)", state.Ahead), yellow))
	case state.Ahead == 0 && state.Behind > 0:
		lines = append(lines, color(opts, fmt.Sprintf("local is behind by %d commit(s)", state.Behind), magenta))
	default:
		lines = append(lines, color(opts, fmt.Sprintf("diverged: local +%d / remote +%d", state.Ahead, state.Behind), red))
	}
	lines = append(lines, "sync      "+syncBar(state.Ahead, state.Behind, opts))
	return lines
}

func attentionLines(state gitstate.State, opts Options) []string {
	if !state.Operation.InProgress && state.Counts.Conflicted == 0 {
		return nil
	}
	var lines []string
	if state.Operation.InProgress {
		lines = append(lines, color(opts, fmt.Sprintf("%s in progress", state.Operation.Kind), red))
	}
	if state.Counts.Conflicted > 0 {
		lines = append(lines, color(opts, fmt.Sprintf("%d conflicted file(s)", state.Counts.Conflicted), red))
		for _, file := range conflictFiles(state) {
			lines = append(lines, truncate(fmt.Sprintf("%s %s", colorFileStatus(file.Status, opts), file.Path), 120))
		}
	}
	lines = append(lines, "")
	lines = append(lines, "next action:")
	if state.Counts.Conflicted > 0 {
		lines = append(lines, "1. resolve conflicted files")
		lines = append(lines, "2. git add <resolved files>")
		if state.Operation.ContinueCommand != "" {
			lines = append(lines, "3. "+state.Operation.ContinueCommand)
		}
	} else if state.Operation.ContinueCommand != "" {
		lines = append(lines, state.Operation.ContinueCommand)
	}
	if state.Operation.SkipCommand != "" {
		lines = append(lines, "skip: "+state.Operation.SkipCommand)
	}
	if state.Operation.AbortCommand != "" {
		lines = append(lines, "abort: "+state.Operation.AbortCommand)
	}
	lines = append(lines, "press t to compare with native git status")
	return lines
}

func workspaceLines(state gitstate.State, opts Options) []string {
	c := state.Counts
	total := totalChanges(c)
	if total == 0 {
		return []string{
			kv("changes", "0"),
			meterLine("staged", 0, 1, green, opts),
			meterLine("worktree", 0, 1, yellow, opts),
			color(opts, "index and working tree are clean", green),
		}
	}
	return []string{
		kv("changes", strconv.Itoa(total)),
		meterLine("staged", c.Staged, total, green, opts),
		meterLine("worktree", c.Modified, total, yellow, opts),
		meterLine("untracked", c.Untracked, total, magenta, opts),
		meterLine("conflicts", c.Conflicted, total, red, opts),
	}
}

func graphLines(state gitstate.State, width int, opts Options) []string {
	if len(state.Graph) == 0 {
		return []string{color(opts, "no commits yet", dim)}
	}
	lines := make([]string, 0, len(state.Graph))
	for _, line := range state.Graph {
		lines = append(lines, colorGraph(truncate(line, width), opts))
	}
	return lines
}

func graphLineCount(state gitstate.State) int {
	if len(state.Graph) == 0 {
		return 1
	}
	return len(state.Graph)
}

func graphLineAt(state gitstate.State, width, index int, opts Options) string {
	if len(state.Graph) == 0 {
		return color(opts, "no commits yet", dim)
	}
	if index < 0 || index >= len(state.Graph) {
		return ""
	}
	return colorGraph(truncate(state.Graph[index], width), opts)
}

func graphTabLines(state gitstate.State, width int, opts Options) []string {
	lines := make([]string, 0, graphTabLineCount(state))
	for i := 0; i < graphTabLineCount(state); i++ {
		lines = append(lines, graphTabLineAt(state, width, i, opts))
	}
	return lines
}

func graphTabLineCount(state gitstate.State) int {
	return 3 + graphLineCount(state)
}

func graphTabLineAt(state gitstate.State, width, index int, opts Options) string {
	if opts.GraphAll {
		switch index {
		case 0:
			return color(opts, truncate("mode: detailed --all", width), yellow)
		case 1:
			return color(opts, truncate("press a to return to normal branch graph", width), dim)
		case 2:
			return ""
		}
	} else {
		switch index {
		case 0:
			return color(opts, truncate("mode: normal branch graph", width), cyanBold)
		case 1:
			return color(opts, truncate("press a to show detailed --all graph, including stash/internal refs", width), yellow)
		case 2:
			return ""
		}
	}
	return graphLineAt(state, width, index-3, opts)
}

func graphScrollPanel(title string, state gitstate.State, width, maxHeight int, opts Options) string {
	lineWidth := width - 4
	return scrollPanelFromSource(title, graphTabLineCount(state), func(index int) string {
		return graphTabLineAt(state, lineWidth, index, opts)
	}, width, maxHeight, opts)
}

func diffLines(state gitstate.State, width int, opts Options) []string {
	diff, mode, next := selectedDiff(state, opts)
	lines := make([]string, 0, diffDisplayLineCount(diff))
	for i := 0; i < diffDisplayLineCount(diff); i++ {
		lines = append(lines, diffLineAt(diff, mode, next, width, i, opts))
	}
	return lines
}

func selectedDiff(state gitstate.State, opts Options) ([]string, string, string) {
	diff := state.WorktreeDiff
	mode := "worktree"
	next := "staged"
	if opts.DiffStaged {
		diff = state.StagedDiff
		mode = "staged"
		next = "worktree"
	}
	return diff, mode, next
}

func diffLineCount(state gitstate.State, opts Options) int {
	diff, _, _ := selectedDiff(state, opts)
	return diffDisplayLineCount(diff)
}

func diffDisplayLineCount(diff []string) int {
	if len(diff) == 0 {
		return 2
	}
	return len(diff) + 2
}

func diffLineAt(diff []string, mode, next string, width, index int, opts Options) string {
	if len(diff) == 0 {
		switch index {
		case 0:
			return color(opts, fmt.Sprintf("no %s diff", mode), green)
		case 1:
			return color(opts, fmt.Sprintf("press s to show %s diff", next), dim)
		default:
			return ""
		}
	}
	switch index {
	case 0:
		return color(opts, fmt.Sprintf("mode: %s diff, press s to show %s diff", mode, next), cyanBold)
	case 1:
		return color(opts, "copy: y worktree, i staged, a full patch; s toggles view", dim)
	}
	diffIndex := index - 2
	if diffIndex < 0 || diffIndex >= len(diff) {
		return ""
	}
	return colorDiffLine(truncate(expandTabs(diff[diffIndex], 8), width), opts)
}

func diffScrollPanel(title string, state gitstate.State, width, maxHeight int, opts Options) string {
	diff, mode, next := selectedDiff(state, opts)
	lineWidth := width - 4
	return scrollPanelFromSource(title, diffDisplayLineCount(diff), func(index int) string {
		return diffLineAt(diff, mode, next, lineWidth, index, opts)
	}, width, maxHeight, opts)
}

func fileLines(state gitstate.State, width int, opts Options) []string {
	if len(state.Files) == 0 {
		return []string{color(opts, "no file changes", green)}
	}
	files := prioritizedFiles(state.Files)
	limit := min(len(state.Files), 14)
	lines := make([]string, 0, limit+1)
	for _, file := range files[:limit] {
		lines = append(lines, truncate(fmt.Sprintf("%s %-9s %s", colorFileStatus(file.Status, opts), fileKindLabel(file.Kind, opts), file.Path), width))
	}
	if len(state.Files) > limit {
		lines = append(lines, color(opts, fmt.Sprintf("... %d more", len(state.Files)-limit), dim))
	}
	return lines
}

func prioritizedFiles(files []gitstate.File) []gitstate.File {
	out := append([]gitstate.File(nil), files...)
	sort.SliceStable(out, func(i, j int) bool {
		return filePriority(out[i]) < filePriority(out[j])
	})
	return out
}

func filePriority(file gitstate.File) int {
	switch file.Kind {
	case "conflict":
		return 0
	case "staged", "staged+worktree":
		return 1
	case "worktree":
		return 2
	case "untracked":
		return 3
	default:
		return 4
	}
}

func conflictFiles(state gitstate.State) []gitstate.File {
	var files []gitstate.File
	for _, file := range state.Files {
		if file.Kind == "conflict" {
			files = append(files, file)
		}
	}
	return files
}

func colorFileStatus(status string, opts Options) string {
	if !opts.Color {
		return padRight(status, 2)
	}
	if status == "??" {
		return color(opts, "??", red)
	}
	if len(status) < 2 {
		return color(opts, padRight(status, 2), yellow)
	}
	if isConflictStatus(status) {
		return colorStatusRune(rune(status[0]), false, opts) + colorStatusRune(rune(status[1]), false, opts)
	}
	return colorStatusRune(rune(status[0]), true, opts) + colorStatusRune(rune(status[1]), false, opts)
}

func isConflictStatus(status string) bool {
	return strings.ContainsRune(status, 'U') || status == "AA" || status == "DD"
}

func colorStatusRune(r rune, index bool, opts Options) string {
	if r == '.' || r == ' ' {
		return " "
	}
	if index {
		return color(opts, string(r), green)
	}
	return color(opts, string(r), red)
}

func fileKindLabel(kind string, opts Options) string {
	switch kind {
	case "conflict":
		return color(opts, "CONFLICT", red)
	case "staged+worktree":
		return color(opts, "INDEX+WT", yellow)
	case "staged":
		return color(opts, "INDEX", green)
	case "worktree":
		return color(opts, "WORKTREE", yellow)
	case "untracked":
		return color(opts, "NEW", magenta)
	default:
		return color(opts, strings.ToUpper(kind), dim)
	}
}

func refLines(state gitstate.State, width int, opts Options) []string {
	if len(state.Refs) == 0 {
		return []string{color(opts, "no refs", dim)}
	}
	limit := min(len(state.Refs), 14)
	lines := make([]string, 0, limit+1)
	for _, ref := range state.Refs[:limit] {
		prefix := " "
		if ref.Current {
			prefix = color(opts, "*", green)
		}
		name := ref.Name
		if ref.Remote {
			name = color(opts, name, magenta)
		} else if ref.Current {
			name = color(opts, name, whiteBold)
		}
		scope := color(opts, "local", green)
		if ref.Remote {
			scope = color(opts, "remote", magenta)
		}
		line := fmt.Sprintf("%s %-6s %-18s %s %s", prefix, scope, name, ref.Hash, ref.Age)
		lines = append(lines, truncate(line, width))
	}
	if len(state.Refs) > limit {
		lines = append(lines, color(opts, fmt.Sprintf("... %d more", len(state.Refs)-limit), dim))
	}
	return lines
}

func branchLines(state gitstate.State, width int, opts Options) []string {
	var lines []string
	lines = append(lines, kv("current", state.Branch))
	lines = append(lines, kv("upstream", state.Upstream))
	if state.Upstream == "" {
		lines = append(lines, color(opts, "this branch has no upstream; push/pull has no default partner", yellow))
	} else {
		switch {
		case state.Ahead == 0 && state.Behind == 0:
			lines = append(lines, color(opts, "current branch is synchronized with upstream", green))
		case state.Ahead > 0 && state.Behind == 0:
			lines = append(lines, color(opts, fmt.Sprintf("current branch is ahead by %d commit(s)", state.Ahead), yellow))
		case state.Ahead == 0 && state.Behind > 0:
			lines = append(lines, color(opts, fmt.Sprintf("current branch is behind by %d commit(s)", state.Behind), magenta))
		default:
			lines = append(lines, color(opts, fmt.Sprintf("current branch diverged: local +%d / remote +%d", state.Ahead, state.Behind), red))
		}
	}
	lines = append(lines, "")

	locals := 0
	remotes := 0
	for _, ref := range state.Refs {
		if ref.Remote {
			remotes++
			continue
		}
		locals++
		prefix := " "
		if ref.Current {
			prefix = color(opts, "*", green)
		}
		upstream := ref.Upstream
		if upstream == "" {
			upstream = "(no upstream)"
		}
		lines = append(lines, truncate(fmt.Sprintf("%s %-22s -> %-22s %s %s", prefix, ref.Name, upstream, ref.Hash, ref.Age), width))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("local branches: %d    remote branches: %d", locals, remotes))
	if remotes > 0 {
		lines = append(lines, "remote-tracking branches show the last fetched remote state")
	}
	return lines
}

func stashLines(state gitstate.State, width int, opts Options) []string {
	if len(state.Stashes) == 0 {
		return []string{
			color(opts, "no stashes", green),
			"stash is a temporary shelf outside the normal commit graph",
		}
	}
	lines := make([]string, 0, len(state.Stashes)+2)
	lines = append(lines, "stash entries are not on the current branch until applied")
	lines = append(lines, "")
	for _, stash := range state.Stashes {
		lines = append(lines, truncate(fmt.Sprintf("%-10s %-14s %s", stash.Name, stash.Age, stash.Message), width))
	}
	return lines
}

func noteLines(state gitstate.State, width int, opts Options) []string {
	var lines []string
	for _, remote := range state.Remotes {
		lines = append(lines, truncate(fmt.Sprintf("remote %-10s %-5s %s", remote.Name, remote.Kind, remote.URL), width))
	}
	for _, stash := range state.Stashes {
		lines = append(lines, truncate(fmt.Sprintf("stash  %-10s %-12s %s", stash.Name, stash.Age, stash.Message), width))
	}
	for _, warning := range state.Warnings {
		lines = append(lines, color(opts, truncate("warning "+warning, width), yellow))
	}
	return lines
}

func helpLines(state gitstate.State, width int, opts Options) []string {
	lines := []string{
		color(opts, "keys", cyanBold),
		"tab       move to the next view",
		"right     move to the next view",
		"left      move to the previous view",
		"q         quit",
		"j/k       scroll graph and diff by one line",
		"d/u       scroll graph and diff by half a page",
		"f/b       scroll graph and diff by one page",
		"page keys scroll graph and diff by one page",
		"s         toggle staged/worktree diff on diff view",
		"y         copy worktree diff on diff view",
		"i         copy staged/index diff on diff view",
		"a         copy full patch on diff view; toggle --all on graph view",
		"1-8       jump to a view directly",
		"?         open this help view",
		"t         toggle native git status",
		"r         refresh immediately",
		"",
		color(opts, "views", cyanBold),
		"overview  sync, workspace, changed files, and recent graph",
		"graph     normal graph; press a for detailed --all graph",
		"files     index and worktree changes",
		"diff      current staged and worktree patch",
		"branches  current branch, upstream, and branch relationships",
		"stash     temporary saved work outside the current branch",
		"refs      local and remote refs",
		"remote    remotes, stashes, and collection warnings",
		"",
		color(opts, "mental model", cyanBold),
		"local is your checked-out repository state",
		"remote-tracking refs are the last fetched view of the remote",
		"index is the next commit you are preparing",
		"worktree is the files currently on disk",
		"gst never runs write operations; commands shown here are suggestions",
	}
	if state.Counts.Conflicted > 0 {
		lines = append(lines, "", color(opts, "attention: conflicts are present", red))
	}
	if state.Upstream == "" {
		lines = append(lines, "", color(opts, "attention: current branch has no upstream", yellow))
	}
	for i, line := range lines {
		lines[i] = truncate(line, width)
	}
	return lines
}

func panel(title string, lines []string, width, maxHeight int, opts Options) string {
	if width < 20 {
		width = 20
	}
	if titleWidth := visibleLen(title) + 6; width < titleWidth {
		title = truncate(title, max(1, width-6))
	}
	if maxHeight < 3 {
		maxHeight = 3
	}
	inner := width - 4
	contentRows := maxHeight - 2
	truncated := false
	if len(lines) > contentRows {
		lines = lines[:contentRows]
		truncated = true
	}
	if truncated && len(lines) > 1 {
		lines[len(lines)-1] = color(opts, fmt.Sprintf("... more (%s tab for full view)", title), dim)
	}
	var b strings.Builder
	b.WriteString(panelTopBorder(title, width, opts))
	b.WriteString("\n")
	for _, line := range lines {
		b.WriteString(color(opts, "|", dim))
		b.WriteString(" ")
		b.WriteString(padRight(truncate(line, inner), inner))
		b.WriteString(" ")
		b.WriteString(color(opts, "|", dim))
		b.WriteString("\n")
	}
	b.WriteString(panelBorder("+"+strings.Repeat("-", width-2)+"+", opts))
	return b.String()
}

func panelTopBorder(title string, width int, opts Options) string {
	return panelBorder("+- ", opts) +
		color(opts, title, cyanBold) +
		panelBorder(" "+strings.Repeat("-", max(0, width-visibleLen(title)-5))+"+", opts)
}

func panelBorder(s string, opts Options) string {
	return color(opts, s, dim)
}

func scrollPanel(title string, lines []string, width, maxHeight int, opts Options) string {
	if maxHeight < 3 {
		maxHeight = 3
	}
	contentRows := maxHeight - 2
	scroll := clampScroll(opts.Scroll, len(lines), contentRows)
	displayTitle := title
	if len(lines) > contentRows {
		start := scroll + 1
		end := min(len(lines), scroll+contentRows)
		displayTitle = fmt.Sprintf("%s %d-%d/%d", title, start, end, len(lines))
	}
	return panel(displayTitle, sliceLines(lines, scroll, contentRows), width, maxHeight, opts)
}

func scrollPanelFromSource(title string, total int, lineAt func(int) string, width, maxHeight int, opts Options) string {
	if maxHeight < 3 {
		maxHeight = 3
	}
	contentRows := maxHeight - 2
	scroll := clampScroll(opts.Scroll, total, contentRows)
	displayTitle := title
	if total > contentRows {
		start := scroll + 1
		end := min(total, scroll+contentRows)
		displayTitle = fmt.Sprintf("%s %d-%d/%d", title, start, end, total)
	}

	visibleRows := min(contentRows, max(0, total-scroll))
	lines := make([]string, 0, visibleRows)
	for i := 0; i < visibleRows; i++ {
		lines = append(lines, lineAt(scroll+i))
	}
	return panel(displayTitle, lines, width, maxHeight, opts)
}

func sliceLines(lines []string, scroll, rows int) []string {
	if rows <= 0 || len(lines) == 0 {
		return nil
	}
	scroll = clampScroll(scroll, len(lines), rows)
	end := min(len(lines), scroll+rows)
	return lines[scroll:end]
}

func clampScroll(scroll, total, rows int) int {
	if scroll < 0 || total <= rows {
		return 0
	}
	maxScroll := max(0, total-rows)
	return min(scroll, maxScroll)
}

func joinPanels(left, right string) string {
	ll := strings.Split(left, "\n")
	rr := strings.Split(right, "\n")
	rows := max(len(ll), len(rr))
	leftW := visibleLen(ll[0])
	var b strings.Builder
	for i := 0; i < rows; i++ {
		if i > 0 {
			b.WriteString("\n")
		}
		l := ""
		if i < len(ll) {
			l = ll[i]
		}
		r := ""
		if i < len(rr) {
			r = rr[i]
		}
		b.WriteString(padRight(l, leftW))
		b.WriteString("  ")
		b.WriteString(r)
	}
	return b.String()
}

func syncBar(ahead, behind int, opts Options) string {
	if ahead == 0 && behind == 0 {
		return "[local == remote]"
	}
	total := max(1, ahead+behind)
	const cells = 24
	left := ahead * cells / total
	right := behind * cells / total
	if ahead > 0 && left == 0 {
		left = 1
	}
	if behind > 0 && right == 0 {
		right = 1
	}
	mid := cells - left - right
	return "[" +
		color(opts, strings.Repeat(">", left), yellow) +
		color(opts, strings.Repeat("-", mid), dim) +
		color(opts, strings.Repeat("<", right), magenta) +
		"]"
}

func meterLine(label string, value, total int, st style, opts Options) string {
	return fmt.Sprintf("%-10s %3d %s", label, value, meter(value, total, 18, st, opts))
}

func meter(value, total, cells int, st style, opts Options) string {
	if cells <= 0 {
		return "[]"
	}
	if total <= 0 {
		total = 1
	}
	value = max(0, value)
	filled := value * cells / total
	if value > 0 && filled == 0 {
		filled = 1
	}
	filled = min(cells, filled)
	return "[" +
		color(opts, strings.Repeat("#", filled), st) +
		color(opts, strings.Repeat(".", cells-filled), dim) +
		"]"
}

func totalChanges(c gitstate.Counts) int {
	return c.Staged + c.Modified + c.Untracked + c.Conflicted
}

func kv(k, v string) string {
	if v == "" {
		v = "(unknown)"
	}
	return fmt.Sprintf("%-10s %s", k, v)
}

func dirty(state gitstate.State) bool {
	return state.Counts.Staged+state.Counts.Modified+state.Counts.Untracked+state.Counts.Conflicted > 0
}

func colorGraph(line string, opts Options) string {
	if !opts.Color {
		return line
	}
	replacer := strings.NewReplacer(
		"*", color(opts, "*", yellow),
		"|", color(opts, "|", dim),
		"/", color(opts, "/", dim),
		"\\", color(opts, "\\", dim),
		"(", color(opts, "(", cyan),
		")", color(opts, ")", cyan),
	)
	return replacer.Replace(line)
}

func colorDiffLine(line string, opts Options) string {
	if !opts.Color {
		return line
	}
	switch {
	case strings.HasPrefix(line, "staged diff"), strings.HasPrefix(line, "worktree diff"):
		return color(opts, line, cyanBold)
	case strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "):
		return color(opts, line, dim)
	case strings.HasPrefix(line, "@@"):
		return color(opts, line, cyan)
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return color(opts, line, green)
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return color(opts, line, red)
	default:
		return line
	}
}

type style string

const (
	reset     style = "\x1b[0m"
	dim       style = "\x1b[2m"
	red       style = "\x1b[31m"
	green     style = "\x1b[32m"
	yellow    style = "\x1b[33m"
	magenta   style = "\x1b[35m"
	cyan      style = "\x1b[36m"
	cyanBold  style = "\x1b[1;36m"
	whiteBold style = "\x1b[1;37m"
)

func color(opts Options, s string, st style) string {
	if !opts.Color {
		return s
	}
	return string(st) + s + string(reset)
}

func trimToHeight(s string, height int) string {
	if height <= 0 {
		return s
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:height], "\n")
}

func truncate(s string, width int) string {
	if visibleLen(s) <= width {
		return s
	}
	if width <= 1 {
		return ""
	}
	var b strings.Builder
	used := 0
	inEsc := false
	styleActive := false
	var esc strings.Builder
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			esc.Reset()
			esc.WriteRune(r)
			b.WriteRune(r)
			continue
		}
		if inEsc {
			esc.WriteRune(r)
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
				code := esc.String()
				if code == string(reset) {
					styleActive = false
				} else {
					styleActive = true
				}
			}
			continue
		}
		w := runeWidth(r)
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	if styleActive {
		return b.String() + "." + string(reset)
	}
	return b.String() + "."
}

func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-visibleLen(s)))
}

func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	if r == utf8.RuneError {
		return 1
	}
	if r >= 0x1100 {
		return 2
	}
	return 1
}
