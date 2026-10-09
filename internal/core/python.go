package core

import (
	"regexp"
	"strings"
)

// pythonDefRe matches a top-level def/async def/class statement; it is
// applied only to lines already known to start at column 0.
var pythonDefRe = regexp.MustCompile(`^(async\s+def|def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// pythonUnits finds top-level function/class boundaries by indentation, not
// a full parse: Python has no stdlib-free Go parser, and this project
// prefers a small hand-rolled reader over a new dependency where one is
// reasonably tractable (see gitignore.go). Nested/method-level units are
// intentionally out of scope; a line inside a multi-line triple-quoted
// string, a column-0 comment and a column-0 closing bracket are never
// mistaken for a top-level statement.
func pythonUnits(path string, data []byte) []Unit {
	lines := strings.Split(string(data), "\n")
	units := []Unit{}
	var current *Unit
	decoratorStart := 0
	inString := false
	stringDelim := ""

	closeCurrent := func(endLine int) {
		if current != nil {
			current.End = endLine
			units = append(units, *current)
			current = nil
		}
	}

	for i, raw := range lines {
		lineNo := i + 1

		if inString {
			if strings.Contains(raw, stringDelim) {
				inString = false
			}
			continue
		}

		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		// A comment never ends a block, whatever its indentation, and its
		// text must not be read as a string delimiter.
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		if delim := pythonOpenString(raw); delim != "" {
			inString = true
			stringDelim = delim
		}

		if !pythonStartsStatement(raw) {
			continue
		}

		if strings.HasPrefix(trimmed, "@") {
			closeCurrent(lineNo - 1)
			if decoratorStart == 0 {
				decoratorStart = lineNo
			}
			continue
		}

		if m := pythonDefRe.FindStringSubmatch(trimmed); m != nil {
			closeCurrent(lineNo - 1)
			start := lineNo
			if decoratorStart != 0 {
				start = decoratorStart
			}
			kind, label := "function", "func"
			if m[1] == "class" {
				kind, label = "type", "class"
			}
			name := label + " " + m[2]
			current = &Unit{ID: path + ":" + name, Kind: kind, Name: name, Start: start}
			decoratorStart = 0
			continue
		}

		closeCurrent(lineNo - 1)
		decoratorStart = 0
	}
	closeCurrent(len(lines))
	return uniqueUnits(units)
}

// pythonOpenString returns the triple-quote delimiter a line leaves open.
func pythonOpenString(line string) string {
	for _, delim := range []string{`"""`, "'''"} {
		if strings.Count(line, delim)%2 == 1 {
			return delim
		}
	}
	return ""
}

// pythonStartsStatement reports whether a non-blank line begins a top-level
// statement. Any leading whitespace means it does not, whatever the indent
// width; nor does a column-0 closing bracket, which continues the previous
// statement, as in a signature wrapped one parameter per line.
func pythonStartsStatement(line string) bool {
	return !strings.ContainsRune(" \t)]}", rune(line[0]))
}
