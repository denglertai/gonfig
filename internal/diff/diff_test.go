package diff

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUnified checks the format of unified diffs, including hunk headers, context and missing line breaks
func TestUnified(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{name: "equal", old: "a\nb\n", new: "a\nb\n", want: ""},
		{
			name: "changed line",
			old:  "a\nb\nc\n",
			new:  "a\nB\nc\n",
			want: "--- old\n+++ new\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n",
		},
		{
			name: "context of three lines",
			old:  "1\n2\n3\n4\n5\n6\n7\n8\n9\n",
			new:  "1\n2\n3\n4\nfive\n6\n7\n8\n9\n",
			want: "--- old\n+++ new\n@@ -2,7 +2,7 @@\n 2\n 3\n 4\n-5\n+five\n 6\n 7\n 8\n",
		},
		{
			name: "separate hunks",
			old:  "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n",
			new:  "one\n2\n3\n4\n5\n6\n7\n8\n9\nten\n",
			want: "--- old\n+++ new\n@@ -1,4 +1,4 @@\n-1\n+one\n 2\n 3\n 4\n@@ -7,4 +7,4 @@\n 7\n 8\n 9\n-10\n+ten\n",
		},
		{
			name: "changes separated by six lines share a hunk",
			old:  "1\n2\n3\n4\n5\n6\n7\n8\n",
			new:  "one\n2\n3\n4\n5\n6\n7\neight\n",
			want: "--- old\n+++ new\n@@ -1,8 +1,8 @@\n-1\n+one\n 2\n 3\n 4\n 5\n 6\n 7\n-8\n+eight\n",
		},
		{
			name: "added lines at the start",
			old:  "a\n",
			new:  "x\ny\na\n",
			want: "--- old\n+++ new\n@@ -1 +1,3 @@\n+x\n+y\n a\n",
		},
		{
			name: "removed lines at the end",
			old:  "a\nb\n",
			new:  "a\n",
			want: "--- old\n+++ new\n@@ -1,2 +1 @@\n a\n-b\n",
		},
		{
			name: "empty old text",
			old:  "",
			new:  "a\n",
			want: "--- old\n+++ new\n@@ -0,0 +1 @@\n+a\n",
		},
		{
			name: "missing line break at the end",
			old:  "a\nb",
			new:  "a\nb\n",
			want: "--- old\n+++ new\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Unified("old", "new", tt.old, tt.new))
		})
	}
}

// TestEditScriptIsShortest checks with random texts that the edit script turns the old into the new lines and keeps
// as many lines as the longest common subsequence has, so the diff is as short as possible
func TestEditScriptIsShortest(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		// A small alphabet causes many equal lines, which is the hard case for the search
		a, b := randomLines(r), randomLines(r)
		edits := editScript(a, b)

		gotA, gotB := []string{}, []string{}
		kept := 0
		for _, e := range edits {
			if e.kind != '+' {
				gotA = append(gotA, e.line)
			}
			if e.kind != '-' {
				gotB = append(gotB, e.line)
			}
			if e.kind == ' ' {
				kept++
			}
		}
		assert.Equal(t, a, gotA, "old lines of %q and %q", a, b)
		assert.Equal(t, b, gotB, "new lines of %q and %q", a, b)
		assert.Equal(t, lcsLength(a, b), kept, "kept lines of %q and %q", a, b)
	}
}

// randomLines returns up to 12 lines from an alphabet of four lines
func randomLines(r *rand.Rand) []string {
	lines := make([]string, r.IntN(13))
	for i := range lines {
		lines[i] = string(rune('a'+r.IntN(4))) + "\n"
	}
	return lines
}

// lcsLength returns the length of the longest common subsequence by dynamic programming
func lcsLength(a []string, b []string) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	return dp[0][0]
}

// TestUnifiedMatchesPatch checks that applying the hunks of random diffs to the old text yields the new text
func TestUnifiedMatchesPatch(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 500 {
		a, b := randomLines(r), randomLines(r)
		old, new := strings.Join(a, ""), strings.Join(b, "")
		assert.Equal(t, new, applyUnified(t, old, Unified("old", "new", old, new)), "diff of %q and %q", old, new)
	}
}

// applyUnified applies a unified diff without missing line breaks to the text, using the old line numbers of the headers
func applyUnified(t *testing.T, text string, diff string) string {
	t.Helper()
	if diff == "" {
		return text
	}

	old := splitLines(text)
	var result []string
	next := 0
	for _, line := range splitLines(diff)[2:] {
		switch line[0] {
		case '@':
			// The old range is "-start" or "-start,count", where an empty range names the line before it
			start, count, _ := strings.Cut(strings.Fields(line)[1][1:], ",")
			first, err := strconv.Atoi(start)
			assert.NoError(t, err)
			if count != "0" {
				first--
			}
			result = append(result, old[next:first]...)
			next = first
		case ' ':
			result = append(result, old[next])
			next++
		case '-':
			next++
		case '+':
			result = append(result, line[1:])
		}
	}
	return strings.Join(append(result, old[next:]...), "")
}
