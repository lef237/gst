package gitstate

import "testing"

func TestParseStatusPorcelainV2(t *testing.T) {
	out := `# branch.oid abcdef1234567890
# branch.head feature
# branch.upstream origin/feature
# branch.ab +2 -1
1 M. N... 100644 100644 100644 aaaaaaa bbbbbbb file.go
1 .M N... 100644 100644 100644 aaaaaaa bbbbbbb README.md
? scratch.txt
u UU N... 100644 100644 100644 100644 aaaaaaa bbbbbbb ccccccc ddddddd conflict.txt
`
	var state State
	parseStatus(out, &state)

	if state.Branch != "feature" || state.Upstream != "origin/feature" {
		t.Fatalf("branch parse failed: %#v", state)
	}
	if state.Ahead != 2 || state.Behind != 1 {
		t.Fatalf("ahead/behind parse failed: +%d -%d", state.Ahead, state.Behind)
	}
	if state.Counts.Staged != 1 || state.Counts.Modified != 1 || state.Counts.Untracked != 1 || state.Counts.Conflicted != 1 {
		t.Fatalf("counts mismatch: %#v", state.Counts)
	}
	if len(state.Files) != 4 {
		t.Fatalf("files mismatch: %d", len(state.Files))
	}
}

func TestParseRefsSortsCurrentFirst(t *testing.T) {
	refs := parseRefs("origin/main|1111111|2 days ago|\nmain|2222222|1 day ago|origin/main\n", "main")
	if len(refs) != 2 {
		t.Fatalf("refs mismatch: %d", len(refs))
	}
	if refs[0].Name != "main" || !refs[0].Current {
		t.Fatalf("current ref should be first: %#v", refs)
	}
}
