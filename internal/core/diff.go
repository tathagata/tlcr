package core

import (
	"bytes"
	"strings"
)

// DiffLine is one line of a hunk. Op is " " for context, "+" for a line only
// in the head and "-" for a line only in the base; Before and After are the
// line numbers in the file on each side, zero where the line is absent.
type DiffLine struct {
	Op     string `json:"op"`
	Text   string `json:"text"`
	Before int    `json:"before,omitempty"`
	After  int    `json:"after,omitempty"`
}

// Hunk is a run of changed lines with surrounding context.
type Hunk struct {
	Lines []DiffLine `json:"lines"`
}

const (
	diffContext = 3
	// maxDiffCells bounds the quadratic table used between anchors; a larger
	// region with no line unique to both sides is shown as replaced outright.
	maxDiffCells = 1 << 20
)

func sourceLines(source string) []string {
	if source == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(source, "\n"), "\n")
}

// diffOps returns one op per output line: ' ' consumes a line from both
// sides, '-' from a and '+' from b. It anchors on lines unique to both sides
// (patience diff) and solves what lies between exactly when small enough, so
// the result is always a valid diff and its cost is bounded.
func diffOps(a, b []string) []byte {
	ops := make([]byte, 0, len(a)+len(b))
	return appendDiff(ops, a, b)
}

func appendDiff(ops []byte, a, b []string) []byte {
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		ops, a, b = append(ops, ' '), a[1:], b[1:]
	}
	tail := 0
	for tail < len(a) && tail < len(b) && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	a, b = a[:len(a)-tail], b[:len(b)-tail]
	switch anchors := uniqueAnchors(a, b); {
	case len(a) == 0 || len(b) == 0:
		ops = appendReplaced(ops, len(a), len(b))
	case len(anchors) > 0:
		i, j := 0, 0
		for _, anchor := range anchors {
			ops = appendDiff(ops, a[i:anchor[0]], b[j:anchor[1]])
			ops = append(ops, ' ')
			i, j = anchor[0]+1, anchor[1]+1
		}
		ops = appendDiff(ops, a[i:], b[j:])
	case len(a)*len(b) <= maxDiffCells:
		ops = appendLCS(ops, a, b)
	default:
		ops = appendReplaced(ops, len(a), len(b))
	}
	for ; tail > 0; tail-- {
		ops = append(ops, ' ')
	}
	return ops
}

func appendReplaced(ops []byte, removed, added int) []byte {
	for ; removed > 0; removed-- {
		ops = append(ops, '-')
	}
	for ; added > 0; added-- {
		ops = append(ops, '+')
	}
	return ops
}

// uniqueAnchors pairs lines that occur exactly once on each side and keeps
// the longest run of pairs that is increasing on both.
func uniqueAnchors(a, b []string) [][2]int {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	inA, inB := uniquePositions(a), uniquePositions(b)
	pairs := [][2]int{}
	for j, line := range b {
		if i, ok := inA[line]; ok && i >= 0 && inB[line] == j {
			pairs = append(pairs, [2]int{i, j})
		}
	}
	return longestIncreasing(pairs)
}

// uniquePositions maps each line to its index, or to -1 when it repeats.
func uniquePositions(lines []string) map[string]int {
	positions := make(map[string]int, len(lines))
	for i, line := range lines {
		if _, seen := positions[line]; seen {
			i = -1
		}
		positions[line] = i
	}
	return positions
}

// longestIncreasing keeps the longest subsequence of pairs whose first index
// increases; the pairs arrive ordered by their second.
func longestIncreasing(pairs [][2]int) [][2]int {
	tails, previous := []int{}, make([]int, len(pairs))
	for k, pair := range pairs {
		low, high := 0, len(tails)
		for low < high {
			if mid := (low + high) / 2; pairs[tails[mid]][0] < pair[0] {
				low = mid + 1
			} else {
				high = mid
			}
		}
		previous[k] = -1
		if low > 0 {
			previous[k] = tails[low-1]
		}
		if low == len(tails) {
			tails = append(tails, k)
		} else {
			tails[low] = k
		}
	}
	kept := make([][2]int, len(tails))
	for k, at := len(tails)-1, -1; k >= 0; k-- {
		if at == -1 {
			at = tails[k]
		}
		kept[k], at = pairs[at], previous[at]
	}
	return kept
}

// appendLCS diffs a small region exactly with a longest-common-subsequence table.
func appendLCS(ops []byte, a, b []string) []byte {
	width := len(b) + 1
	table := make([]uint16, (len(a)+1)*width)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				table[i*width+j] = table[(i+1)*width+j+1] + 1
			case table[(i+1)*width+j] >= table[i*width+j+1]:
				table[i*width+j] = table[(i+1)*width+j]
			default:
				table[i*width+j] = table[i*width+j+1]
			}
		}
	}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops, i, j = append(ops, ' '), i+1, j+1
		case table[(i+1)*width+j] >= table[i*width+j+1]:
			ops, i = append(ops, '-'), i+1
		default:
			ops, j = append(ops, '+'), j+1
		}
	}
	return appendReplaced(ops, len(a)-i, len(b)-j)
}

// unitDiff holds the hunks between two sides and their changed-line counts.
type unitDiff struct {
	hunks          []Hunk
	added, removed int
}

// diffSources diffs two sources that start at the given lines of their files.
func diffSources(before, after string, beforeStart, afterStart int) unitDiff {
	a, b := sourceLines(before), sourceLines(after)
	return diffNumbered(a, b, func(i int) int { return beforeStart + i }, func(j int) int { return afterStart + j })
}

// diffNumbered groups changed lines into hunks with context; the number
// functions give each line's position in its file.
func diffNumbered(a, b []string, beforeLine, afterLine func(int) int) unitDiff {
	ops := diffOps(a, b)
	lines := make([]DiffLine, 0, len(ops))
	i, j := 0, 0
	for _, op := range ops {
		switch op {
		case ' ':
			lines = append(lines, DiffLine{Op: " ", Text: a[i], Before: beforeLine(i), After: afterLine(j)})
			i, j = i+1, j+1
		case '-':
			lines = append(lines, DiffLine{Op: "-", Text: a[i], Before: beforeLine(i)})
			i++
		default:
			lines = append(lines, DiffLine{Op: "+", Text: b[j], After: afterLine(j)})
			j++
		}
	}
	result := unitDiff{hunks: []Hunk{}, added: bytes.Count(ops, []byte{'+'}), removed: bytes.Count(ops, []byte{'-'})}
	for start := 0; start < len(lines); {
		if lines[start].Op == " " {
			start++
			continue
		}
		// Extend while the next change is close enough to share context.
		end, quiet := start+1, 0
		for scan := start + 1; scan < len(lines) && quiet <= 2*diffContext; scan++ {
			if lines[scan].Op == " " {
				quiet++
				continue
			}
			end, quiet = scan+1, 0
		}
		result.hunks = append(result.hunks, Hunk{Lines: lines[max(0, start-diffContext):min(len(lines), end+diffContext)]})
		start = end
	}
	return result
}

// whitespaceOnly reports sources that differ only in spacing or blank lines.
func whitespaceOnly(before, after string) bool {
	return before != after && before != "" && after != "" && strings.Join(strings.Fields(before), " ") == strings.Join(strings.Fields(after), " ")
}
