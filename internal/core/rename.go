package core

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maxRenamePairs bounds the removed × added comparisons in one review.
	maxRenamePairs = 4096
	// renameThreshold is the share of lines two bodies must have in common.
	renameThreshold = 0.7
)

// pairRenames replaces a removed and an added unit of the same kind with one
// renamed or moved change when each is the other's single best match. Ties
// are left alone: an ambiguous pairing would be a guess.
func pairRenames(changes []UnitChange, oldGraph, newGraph *Graph) ([]UnitChange, []string) {
	removed, added := []int{}, []int{}
	for i, change := range changes {
		switch {
		case change.Node.UnitID == "":
		case change.Status == "removed":
			removed = append(removed, i)
		case change.Status == "added":
			added = append(added, i)
		}
	}
	if len(removed)*len(added) > maxRenamePairs {
		return changes, []string{"Rename detection was skipped: too many removed and added units to compare. They are shown as removed/added."}
	}
	scores := map[[2]int]float64{}
	score := func(r, a int) float64 {
		key := [2]int{r, a}
		if _, done := scores[key]; !done {
			scores[key] = renameScore(changes[r], changes[a])
		}
		return scores[key]
	}
	forward := bestMatches(removed, added, score)
	backward := bestMatches(added, removed, func(a, r int) float64 { return score(r, a) })
	drop := map[int]bool{}
	for r, a := range forward {
		if back, mutual := backward[a]; !mutual || back != r {
			continue
		}
		drop[r] = true
		changes[a] = renamedChange(changes[r], changes[a], oldGraph, newGraph)
	}
	kept := changes[:0]
	for i, change := range changes {
		if !drop[i] {
			kept = append(kept, change)
		}
	}
	return kept, nil
}

// bestMatches maps each index in from to its strictly best counterpart at or
// above the threshold; a tie for first place yields no match.
func bestMatches(from, to []int, score func(a, b int) float64) map[int]int {
	best := map[int]int{}
	for _, a := range from {
		top, second, at := 0.0, 0.0, -1
		for _, b := range to {
			switch s := score(a, b); {
			case s > top:
				top, second, at = s, top, b
			case s > second:
				second = s
			}
		}
		if at >= 0 && top >= renameThreshold && top > second {
			best[a] = at
		}
	}
	return best
}

func renamedChange(removed, added UnitChange, oldGraph, newGraph *Graph) UnitChange {
	change := added
	before := removed.Node
	change.Before, change.BeforeSource = &before, removed.BeforeSource
	change.Status = "renamed"
	if removed.Node.Name == added.Node.Name {
		change.Status = "moved"
	}
	change.RelationshipsAdded, change.RelationshipsRemoved = relationshipDelta(oldGraph, newGraph, removed.Node.ID, added.Node.ID)
	change.setDiff()
	return change
}

// renameScore is 1 when the bodies are identical once the unit's own name is
// substituted, and otherwise the share of lines they have in common.
// Indentation is ignored so a unit moved into another scope still matches.
func renameScore(removed, added UnitChange) float64 {
	if removed.Node.Kind != added.Node.Kind || removed.Node.Language != added.Node.Language {
		return 0
	}
	before := trimmedLines(replaceIdentifier(removed.BeforeSource, shortName(removed.Node.Name), shortName(added.Node.Name)))
	after := trimmedLines(added.AfterSource)
	if strings.Join(before, "\n") == strings.Join(after, "\n") {
		return 1
	}
	if len(before) < 3 || len(after) < 3 || len(before) > 2*len(after) || len(after) > 2*len(before) {
		return 0
	}
	common := bytes.Count(diffOps(before, after), []byte{' '})
	return 2 * float64(common) / float64(len(before)+len(after))
}

func trimmedLines(source string) []string {
	lines := sourceLines(source)
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return lines
}

// shortName is the identifier a unit declares: the last word of its display
// name, after any receiver or address prefix.
func shortName(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	return last[strings.LastIndexByte(last, '.')+1:]
}

func identifierRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// replaceIdentifier substitutes whole-word occurrences only.
func replaceIdentifier(source, from, to string) string {
	if from == "" || from == to {
		return source
	}
	var out strings.Builder
	for {
		at := strings.Index(source, from)
		if at < 0 {
			break
		}
		previous, _ := utf8.DecodeLastRuneInString(source[:at])
		next, _ := utf8.DecodeRuneInString(source[at+len(from):])
		out.WriteString(source[:at])
		if at > 0 && identifierRune(previous) || at+len(from) < len(source) && identifierRune(next) {
			out.WriteString(from)
		} else {
			out.WriteString(to)
		}
		source = source[at+len(from):]
	}
	out.WriteString(source)
	return out.String()
}
