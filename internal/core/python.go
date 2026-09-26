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
// string is never mistaken for a top-level statement.
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

		isTopLevel := raw[0] != ' ' && raw[0] != '\t'

		for _, delim := range []string{`"""`, "'''"} {
			if strings.Count(raw, delim)%2 == 1 {
				inString = true
				stringDelim = delim
				break
			}
		}

		if !isTopLevel {
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
