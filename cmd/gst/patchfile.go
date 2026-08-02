package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lef237/gst/internal/gitstate"
)

const (
	// patchNameAttempts caps how many names savePatch tries. The timestamp only
	// resolves to the second, so two saves in quick succession would otherwise
	// land on the same name.
	patchNameAttempts = 100
	// patchBranchLimit keeps a long branch name from pushing the file name past
	// what the filesystem accepts.
	patchBranchLimit = 40
	patchTimeLayout  = "20060102-150405"
)

// savePatch writes a diff to a .patch file in the working directory gst was
// started from and returns the notice to show. The file is never overwritten:
// a name already in use gets a counter instead, since the diff a user just
// saved is not something a second keystroke should silently discard.
func savePatch(state gitstate.State, target diffTarget) string {
	label, text := diffPayload(state, target)
	if text == "" {
		return "nothing to save: " + label + " is empty"
	}
	dir, err := os.Getwd()
	if err != nil {
		return "save failed: " + err.Error()
	}
	name, err := writePatchFile(dir, patchFileBase(state.Branch, target, time.Now()), text)
	if err != nil {
		return "save failed: " + err.Error()
	}
	return "saved " + label + " to " + name
}

// patchFileBase builds the stem of the file name. The target is part of it
// because a worktree and a staged save one keystroke apart share a timestamp,
// and naming the two apart also says what a patch holds without opening it.
func patchFileBase(branch string, target diffTarget, now time.Time) string {
	name := "detached"
	if branch != "" {
		name = sanitizeFileName(branch)
		if name == "" {
			// A branch spelled entirely in characters the name cannot carry,
			// which is still a branch and must not read as a detached HEAD.
			name = "branch"
		}
	}
	return fmt.Sprintf("gst-%s-%s-%s", name, target.slug(), now.Format(patchTimeLayout))
}

// sanitizeFileName reduces a branch name to characters that are safe in a file
// name on every platform gst runs on. git allows slashes and much more, and a
// name like "feature/fix" would otherwise be read as a directory that does not
// exist.
func sanitizeFileName(branch string) string {
	var b strings.Builder
	dash := false
	for _, r := range branch {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
			dash = false
		default:
			// Collapse runs so "feature//a  b" does not become "feature--a--b".
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
		if b.Len() >= patchBranchLimit {
			break
		}
	}
	return strings.Trim(b.String(), "-.")
}

// writePatchFile creates base.patch, or base-2.patch and so on when the name is
// taken. O_EXCL is what makes that safe: checking for the file first and
// creating it after would still race another gst writing the same second.
func writePatchFile(dir, base, text string) (string, error) {
	for attempt := 1; attempt <= patchNameAttempts; attempt++ {
		name := base + ".patch"
		if attempt > 1 {
			name = fmt.Sprintf("%s-%d.patch", base, attempt)
		}
		file, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := file.WriteString(text); err != nil {
			file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return name, nil
	}
	return "", fmt.Errorf("%s.patch and the %d names after it are taken", base, patchNameAttempts-1)
}
