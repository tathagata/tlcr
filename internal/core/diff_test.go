package core

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
)

func rebuild(d unitDiff, source string, start int, skip string) string {
	lines := sourceLines(source)
	out := append([]string{}, lines...)
	for _, hunk := range d.hunks {
		for _, line := range hunk.Lines {
			number := line.Before
			if skip == "-" {
				number = line.After
			}
			if line.Op == skip {
				continue
			}
			if number-start < 0 || number-start >= len(out) || out[number-start] != line.Text {
				return "line " + strconv.Itoa(number) + " mismatch"
			}
		}
	}
	return strings.Join(out, "\n")
}

func applyOps(t *testing.T, a, b []string) {
	t.Helper()
	i, j := 0, 0
	for _, op := range diffOps(a, b) {
		switch {
		case op == ' ' && i < len(a) && j < len(b) && a[i] == b[j]:
			i, j = i+1, j+1
		case op == '-' && i < len(a):
			i++
		case op == '+' && j < len(b):
			j++
		default:
			t.Fatalf("invalid op %q at %d/%d", op, i, j)
		}
	}
	if i != len(a) || j != len(b) {
		t.Fatalf("diff consumed %d/%d of %d/%d lines", i, j, len(a), len(b))
	}
}

func TestDiffReconstructsBothSidesForRandomEdits(t *testing.T) {
	random := rand.New(rand.NewSource(7)) // #nosec G404 -- deterministic test input
	for round := 0; round < 300; round++ {
		a := make([]string, random.Intn(60))
		for i := range a {
			a[i] = "line " + strconv.Itoa(random.Intn(12))
		}
		b := append([]string{}, a...)
		for edits := random.Intn(8); edits > 0; edits-- {
			at := random.Intn(len(b) + 1)
			switch random.Intn(3) {
			case 0:
				b = append(b[:at], append([]string{"new " + strconv.Itoa(random.Intn(5))}, b[at:]...)...)
			case 1:
				if at < len(b) {
					b = append(b[:at], b[at+1:]...)
				}
			default:
				if at < len(b) {
					b[at] = "edit " + strconv.Itoa(random.Intn(5))
				}
			}
		}
		applyOps(t, a, b)
		before, after := strings.Join(a, "\n"), strings.Join(b, "\n")
		d := diffSources(before, after, 10, 20)
		if got := rebuild(d, before, 10, "+"); got != before {
			t.Fatalf("round %d base: %s", round, got)
		}
		if got := rebuild(d, after, 20, "-"); got != after {
			t.Fatalf("round %d head: %s", round, got)
		}
		if (before == after) != (len(d.hunks) == 0) {
			t.Fatalf("round %d: %d hunks for equal=%v", round, len(d.hunks), before == after)
		}
	}
}

func TestDiffHunksNumbersAndCounts(t *testing.T) {
	before := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\n"
	after := strings.Replace(strings.Replace(before, "b\n", "B\n", 1), "r\n", "r\nR\n", 1)
	d := diffSources(before, after, 5, 105)
	if len(d.hunks) != 2 || d.added != 2 || d.removed != 1 {
		t.Fatalf("hunks: %#v", d)
	}
	first := d.hunks[0].Lines
	if first[0] != (DiffLine{Op: " ", Text: "a", Before: 5, After: 105}) || first[1] != (DiffLine{Op: "-", Text: "b", Before: 6}) || first[2] != (DiffLine{Op: "+", Text: "B", After: 106}) || len(first) != 6 {
		t.Fatalf("first hunk: %#v", first)
	}
	for _, d := range []unitDiff{diffSources("", "x\ny", 1, 7), diffSources("x\ny", "", 7, 1)} {
		if len(d.hunks) != 1 || len(d.hunks[0].Lines) != 2 || d.added+d.removed != 2 {
			t.Fatalf("one-sided: %#v", d)
		}
	}
}

func TestDiffWhitespaceOnly(t *testing.T) {
	if !whitespaceOnly("func a() {\n\treturn\n}", "func a() {\n    return\n\n}") || whitespaceOnly("a b", "ab") || whitespaceOnly("a", "a") || whitespaceOnly("", " ") {
		t.Fatal("whitespace-only classification")
	}
}

func TestDiffWorstCaseIsBounded(t *testing.T) {
	a, b := make([]string, 6000, 12000), make([]string, 6000, 12000)
	for i := range a {
		a[i], b[i] = "x"+strconv.Itoa(i%3), "y"+strconv.Itoa(i%3)
	}
	a[3000], b[10] = "same", "same"
	started := time.Now()
	applyOps(t, a, b)
	applyOps(t, append(a, b...), append(b, a...))
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("diff took %s", elapsed)
	}
}
