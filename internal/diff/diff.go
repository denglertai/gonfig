// Package diff computes the differences of two texts line by line and formats them as a unified diff
package diff

import (
	"fmt"
	"strings"
)

// context is the number of unchanged lines shown around the changes of a hunk, like the default of diff -u
const context = 3

// Unified returns the unified diff of the old and new text with three lines of context, or "" if they are equal.
// A last line without a line break is marked with "\ No newline at end of file", like diff -u does.
func Unified(oldName string, newName string, old string, new string) string {
	if old == new {
		return ""
	}

	a, b := splitLines(old), splitLines(new)
	edits := editScript(a, b)

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", oldName, newName)
	for _, h := range hunks(edits) {
		writeHunk(&sb, h)
	}
	return sb.String()
}

// splitLines splits the text into lines including their line breaks, so a last line without one differs from
// the same line with one
func splitLines(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	// SplitAfter returns an empty string after a trailing line break, which is no line
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// edit is a line of the edit script turning the old into the new lines
type edit struct {
	// kind is ' ' for an unchanged line, '-' for a removed and '+' for an added line
	kind byte
	line string
	// oldLine and newLine are the indexes of the line in the old and new lines, or of the next one for added and
	// removed lines, which locate empty ranges in hunk headers
	oldLine, newLine int
}

// editScript returns the edits of a shortest edit script turning a into b, removing lines before adding lines
func editScript(a []string, b []string) []edit {
	// Lines are compared as numbers, which is faster than comparing strings repeatedly
	ids := map[string]int{}
	toIDs := func(lines []string) []int {
		result := make([]int, len(lines))
		for i, line := range lines {
			id, ok := ids[line]
			if !ok {
				id = len(ids)
				ids[line] = id
			}
			result[i] = id
		}
		return result
	}

	m := newMatcher(toIDs(a), toIDs(b))
	m.match(0, len(m.a), 0, len(m.b))

	edits := make([]edit, 0, len(a)+len(b)-len(m.matches))
	i, j := 0, 0
	// A final match after the last lines adds the remaining changes
	for _, match := range append(m.matches, [2]int{len(a), len(b)}) {
		for ; i < match[0]; i++ {
			edits = append(edits, edit{kind: '-', line: a[i], oldLine: i, newLine: j})
		}
		for ; j < match[1]; j++ {
			edits = append(edits, edit{kind: '+', line: b[j], oldLine: i, newLine: j})
		}
		if i < len(a) {
			edits = append(edits, edit{kind: ' ', line: a[i], oldLine: i, newLine: j})
			i++
			j++
		}
	}
	return edits
}

// matcher finds the lines of a longest common subsequence of a and b with the algorithm of Myers,
// which takes O((N+M)D) time and linear space for N and M lines and D differences
type matcher struct {
	a, b []int
	// aIndex and bIndex map the indexes of a and b to the indexes of the lines in the texts
	aIndex, bIndex []int
	// matches are the indexes of equal lines in the old and new lines in increasing order
	matches [][2]int
}

// newMatcher returns a matcher of the lines that occur in both texts. Other lines cannot be part of a common
// subsequence, so leaving them out keeps the result while files whose lines all changed, e.g. by a different
// indentation, take linear instead of quadratic time.
func newMatcher(a []int, b []int) *matcher {
	m := &matcher{}
	m.a, m.aIndex = common(a, b)
	m.b, m.bIndex = common(b, a)
	return m
}

// common returns the lines of a that occur in b and their indexes in a
func common(a []int, b []int) (lines []int, indexes []int) {
	inB := make(map[int]bool, len(b))
	for _, id := range b {
		inB[id] = true
	}
	for i, id := range a {
		if inB[id] {
			lines = append(lines, id)
			indexes = append(indexes, i)
		}
	}
	return lines, indexes
}

// addMatch adds a match of the lines at the indexes of a and b
func (m *matcher) addMatch(i int, j int) {
	m.matches = append(m.matches, [2]int{m.aIndex[i], m.bIndex[j]})
}

// match adds the matches of the ranges a[aLo:aHi] and b[bLo:bHi]
func (m *matcher) match(aLo int, aHi int, bLo int, bHi int) {
	// Common prefixes and suffixes are matched directly, which is all most config changes need
	for aLo < aHi && bLo < bHi && m.a[aLo] == m.b[bLo] {
		m.addMatch(aLo, bLo)
		aLo++
		bLo++
	}
	suffix := 0
	for aLo < aHi && bLo < bHi && m.a[aHi-1] == m.b[bHi-1] {
		aHi--
		bHi--
		suffix++
	}

	switch {
	case aLo == aHi || bLo == bHi:
		// Only removed or only added lines are left
	case aHi-aLo == 1 || bHi-bLo == 1:
		m.matchSingle(aLo, aHi, bLo, bHi)
	default:
		x, y, found := m.split(aLo, aHi, bLo, bHi)
		if found {
			m.match(aLo, x, bLo, y)
			m.match(x, aHi, y, bHi)
		}
	}

	for i := range suffix {
		m.addMatch(aHi+i, bHi+i)
	}
}

// matchSingle matches a range consisting of a single line with its first occurrence in the other range.
// The ranges have no common prefix or suffix, so their longest common subsequence has at most one line.
func (m *matcher) matchSingle(aLo int, aHi int, bLo int, bHi int) {
	for i := aLo; i < aHi; i++ {
		for j := bLo; j < bHi; j++ {
			if m.a[i] == m.b[j] {
				m.addMatch(i, j)
				return
			}
		}
	}
}

// split returns a point (x, y) on a shortest edit path through the ranges, found by searching from both ends until
// the paths overlap. Both ranges are split into smaller ones there. It returns false if the ranges have no equal lines.
// The ranges have at least two lines each and no common prefix or suffix, so the point is never one of the ends.
func (m *matcher) split(aLo int, aHi int, bLo int, bHi int) (x int, y int, found bool) {
	a, b := m.a[aLo:aHi], m.b[bLo:bHi]
	n, mLen := len(a), len(b)
	maxD := (n + mLen + 1) / 2
	offset := maxD
	// forward[offset+k] and backward[offset+k] are the furthest x reached on diagonal k from the start and the end
	forward := make([]int, 2*maxD)
	backward := make([]int, 2*maxD)
	for i := range forward {
		forward[i], backward[i] = -1, -1
	}
	forward[offset+1], backward[offset+1] = 0, 0

	delta := n - mLen
	// The paths overlap during the forward search if delta is odd, otherwise during the backward search
	odd := delta%2 != 0
	// Diagonals leaving the ranges are skipped at their start or end
	kForwardStart, kForwardEnd, kBackwardStart, kBackwardEnd := 0, 0, 0, 0

	for d := range maxD {
		for k := -d + kForwardStart; k <= d-kForwardEnd; k += 2 {
			i := offset + k
			var x1 int
			if k == -d || (k != d && forward[i-1] < forward[i+1]) {
				x1 = forward[i+1]
			} else {
				x1 = forward[i-1] + 1
			}
			y1 := x1 - k
			for x1 < n && y1 < mLen && a[x1] == b[y1] {
				x1++
				y1++
			}
			forward[i] = x1
			switch {
			case x1 > n:
				kForwardEnd += 2
			case y1 > mLen:
				kForwardStart += 2
			case odd:
				j := offset + delta - k
				if j >= 0 && j < len(backward) && backward[j] != -1 && x1 >= n-backward[j] {
					return aLo + x1, bLo + y1, true
				}
			}
		}

		for k := -d + kBackwardStart; k <= d-kBackwardEnd; k += 2 {
			i := offset + k
			var x2 int
			if k == -d || (k != d && backward[i-1] < backward[i+1]) {
				x2 = backward[i+1]
			} else {
				x2 = backward[i-1] + 1
			}
			y2 := x2 - k
			for x2 < n && y2 < mLen && a[n-x2-1] == b[mLen-y2-1] {
				x2++
				y2++
			}
			backward[i] = x2
			switch {
			case x2 > n:
				kBackwardEnd += 2
			case y2 > mLen:
				kBackwardStart += 2
			case !odd:
				j := offset + delta - k
				if j >= 0 && j < len(forward) && forward[j] != -1 {
					x1 := forward[j]
					y1 := offset + x1 - j
					if x1 >= n-x2 {
						return aLo + x1, bLo + y1, true
					}
				}
			}
		}
	}
	return 0, 0, false
}

// hunk is a range of the edit script with changes and their surrounding unchanged lines
type hunk []edit

// hunks groups the changes of the edit script into hunks. Changes separated by up to twice the context lines
// share a hunk, so the context of a hunk never overlaps with the next one.
func hunks(edits []edit) []hunk {
	var result []hunk
	start, end := -1, -1
	for i, e := range edits {
		if e.kind == ' ' {
			continue
		}
		if start >= 0 && i-end > 2*context {
			result = append(result, edits[start:min(end+context, len(edits))])
			start = -1
		}
		if start < 0 {
			start = max(i-context, 0)
		}
		end = i + 1
	}
	if start >= 0 {
		result = append(result, edits[start:min(end+context, len(edits))])
	}
	return result
}

// writeHunk writes the header and the lines of the hunk
func writeHunk(sb *strings.Builder, h hunk) {
	oldCount, newCount := 0, 0
	for _, e := range h {
		if e.kind != '+' {
			oldCount++
		}
		if e.kind != '-' {
			newCount++
		}
	}
	fmt.Fprintf(sb, "@@ -%s +%s @@\n", hunkRange(h[0].oldLine, oldCount), hunkRange(h[0].newLine, newCount))

	for _, e := range h {
		sb.WriteByte(e.kind)
		sb.WriteString(e.line)
		if !strings.HasSuffix(e.line, "\n") {
			sb.WriteString("\n\\ No newline at end of file\n")
		}
	}
}

// hunkRange formats the range of a hunk starting at the line index like diff -u: the first line counted from 1 and
// the number of lines if it is not 1. An empty range names the line before it, which is 0 at the start of the file.
func hunkRange(start int, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return fmt.Sprintf("%d", start+1)
	default:
		return fmt.Sprintf("%d,%d", start+1, count)
	}
}
