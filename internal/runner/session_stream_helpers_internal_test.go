package runner

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestHeadLinesMatchesSplit pins headLines to the strings.Split behaviour it replaces, including the
// single empty line that Split returns for an empty input.
func TestHeadLinesMatchesSplit(t *testing.T) {
	cases := []string{
		"",
		"x",
		"a\nb",
		"a\nb\n",
		"a\n\nb",
		"1\n2\n3\n4",
		"1\n2\n3\n4\n",
		"1\n2\n3\n4\n5",
		"1\n2\n3\n4\n5\n6\n7\n8\n9\n10",
		strings.Repeat("line\n", 5000) + "tail",
	}
	for _, s := range cases {
		for _, n := range []int{0, 1, 4, 100} {
			want := strings.Split(s, "\n")
			head, total := headLines(s, n)
			if total != len(want) {
				t.Fatalf("headLines(%q, %d) total = %d, want %d", s, n, total, len(want))
			}
			wantHead := want
			if len(wantHead) > n {
				wantHead = wantHead[:n]
			}
			if !slices.Equal(head, wantHead) {
				t.Fatalf("headLines(%q, %d) head = %q, want %q", s, n, head, wantHead)
			}
		}
	}
}

func TestPartKeyMatchesTheSprintfFormItReplaces(t *testing.T) {
	for _, tc := range []struct {
		msgID string
		idx   int
	}{
		{"msg_1", 0},
		{"msg_1", 12},
		{"", 3},
		{"msg_1", 1_000_000},
	} {
		want := fmt.Sprintf("%s_%d", tc.msgID, tc.idx)
		if got := partKey(tc.msgID, tc.idx); got != want {
			t.Fatalf("partKey(%q, %d) = %q, want %q", tc.msgID, tc.idx, got, want)
		}
	}
}
