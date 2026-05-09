package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lef237/gst/internal/gitstate"
)

type Options struct {
	Color bool
	Width int
}

func Render(state gitstate.State, opts Options) string {
	if opts.Width < 60 {
		opts.Width = 60
	}

	var out strings.Builder
	out.WriteString(header(state, opts))
	out.WriteString("\n")

	leftW := opts.Width/2 - 1
	rightW := opts.Width - leftW - 2

	out.WriteString(joinPanels(
		panel("sync", syncLines(state, opts), leftW, opts),
		panel("workspace", workspaceLines(state, opts), rightW, opts),
	))
	out.WriteString("\n")
	out.WriteString(panel("commit graph", graphLines(state, opts.Width-4, opts), opts.Width, opts))
	out.WriteString("\n")
	out.WriteString(joinPanels(
		panel("changed files", fileLines(state, leftW-4, opts), leftW, opts),
		panel("refs", refLines(state, rightW-4, opts), rightW, opts),
	))
	if len(state.Remotes) > 0 || len(state.Stashes) > 0 || len(state.Warnings) > 0 {
		out.WriteString("\n")
		out.WriteString(panel("repository notes", noteLines(state, opts.Width-4, opts), opts.Width, opts))
	}
	return out.String()
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
		status = color(opts, "changes", yellow)
	} else {
		status = color(opts, "clean", green)
	}
	line := fmt.Sprintf("%s %s on %s @ %s  %s", color(opts, title, cyanBold), status, color(opts, branch, whiteBold), head, state.RepoRoot)
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

	switch {
	case state.Ahead == 0 && state.Behind == 0:
		lines = append(lines, color(opts, "local and remote point at the same commit", green))
	case state.Ahead > 0 && state.Behind == 0:
		lines = append(lines, color(opts, fmt.Sprintf("local is ahead by %d commit(s)", state.Ahead), yellow))
		lines = append(lines, "mental model: local has commits remote cannot see yet")
	case state.Ahead == 0 && state.Behind > 0:
		lines = append(lines, color(opts, fmt.Sprintf("local is behind by %d commit(s)", state.Behind), magenta))
		lines = append(lines, "mental model: remote has commits local has not copied yet")
	default:
		lines = append(lines, color(opts, fmt.Sprintf("diverged: local +%d / remote +%d", state.Ahead, state.Behind), red))
		lines = append(lines, "mental model: both sides have unique commits")
	}
	lines = append(lines, syncBar(state.Ahead, state.Behind, opts))
	return lines
}

func workspaceLines(state gitstate.State, opts Options) []string {
	c := state.Counts
	lines := []string{
		kv("staged", strconv.Itoa(c.Staged)),
		kv("worktree", strconv.Itoa(c.Modified)),
		kv("untracked", strconv.Itoa(c.Untracked)),
		kv("conflicts", strconv.Itoa(c.Conflicted)),
	}
	if !dirty(state) {
		lines = append(lines, color(opts, "index and working tree are clean", green))
	} else {
		lines = append(lines, "index = next commit, worktree = files on disk")
	}
	return lines
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

func fileLines(state gitstate.State, width int, opts Options) []string {
	if len(state.Files) == 0 {
		return []string{color(opts, "no file changes", green)}
	}
	limit := min(len(state.Files), 14)
	lines := make([]string, 0, limit+1)
	for _, file := range state.Files[:limit] {
		status := file.Status
		switch file.Kind {
		case "conflict":
			status = color(opts, status, red)
		case "untracked":
			status = color(opts, status, cyan)
		case "staged", "staged+worktree":
			status = color(opts, status, green)
		default:
			status = color(opts, status, yellow)
		}
		lines = append(lines, truncate(fmt.Sprintf("%-2s %s", status, file.Path), width))
	}
	if len(state.Files) > limit {
		lines = append(lines, color(opts, fmt.Sprintf("... %d more", len(state.Files)-limit), dim))
	}
	return lines
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
		line := fmt.Sprintf("%s %-18s %s %s", prefix, name, ref.Hash, ref.Age)
		lines = append(lines, truncate(line, width))
	}
	if len(state.Refs) > limit {
		lines = append(lines, color(opts, fmt.Sprintf("... %d more", len(state.Refs)-limit), dim))
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

func panel(title string, lines []string, width int, opts Options) string {
	if width < 20 {
		width = 20
	}
	inner := width - 4
	var b strings.Builder
	b.WriteString("+- " + title + " " + strings.Repeat("-", max(0, width-visibleLen(title)-5)) + "+\n")
	for _, line := range lines {
		b.WriteString("| ")
		b.WriteString(padRight(truncate(line, inner), inner))
		b.WriteString(" |\n")
	}
	b.WriteString("+" + strings.Repeat("-", width-2) + "+")
	return b.String()
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
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
		}
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
