package core

import (
	"path/filepath"
	"regexp"
	"strings"
)

// bashFuncRe matches a function definition whose opening brace is alone at
// the end of the line: `name() {`, `function name {`, or `function name() {`.
// A bare `name {` with neither the `function` keyword nor `()` is not valid
// bash function syntax and must not match.
var bashFuncRe = regexp.MustCompile(`^\s*(function\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(\(\s*\))?\s*\{\s*$`)

// shellParser recognizes shell scripts by extension or, for extension-less
// executables, by shebang.
type shellParser struct{}

func (shellParser) Kind() string { return "shell" }

func (shellParser) Match(path string, peek []byte) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh", ".bash":
		return true
	case "":
		return isShellShebang(peek)
	default:
		return false
	}
}

func (shellParser) Units(path string, data []byte) []Unit { return bashUnits(path, data) }

// isShellShebang reports whether the first line names a bash/sh interpreter,
// directly or via `env`.
func isShellShebang(peek []byte) bool {
	line := string(peek)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#!") {
		return false
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return false
	}
	interpreter := filepath.Base(fields[len(fields)-1])
	return interpreter == "sh" || interpreter == "bash"
}

// bashUnits finds function definitions by regular-expression match plus a
// brace-depth scan for the matching close — not a shell parser. Quotes,
// backslash escapes and `#` comments are tracked per line so that `if`/`for`/
// `case` bodies and embedded strings inside a function never look like its
// closing brace; heredoc bodies are not specially handled and could throw
// off brace counting if they contain unbalanced braces.
func bashUnits(path string, data []byte) []Unit {
	lines := strings.Split(string(data), "\n")
	units := []Unit{}

	for i := 0; i < len(lines); i++ {
		m := bashFuncRe.FindStringSubmatch(lines[i])
		if m == nil || (m[1] == "" && m[3] == "") {
			continue
		}
		name := "function " + m[2]
		start := i + 1
		depth := 1
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			depth += bashBraceDelta(lines[j])
			if depth <= 0 {
				end = j + 1
				break
			}
		}
		units = append(units, Unit{ID: path + ":" + name, Kind: "function", Name: name, Start: start, End: end})
	}
	return uniqueUnits(units)
}

// bashBraceDelta returns a line's net effect on brace depth, ignoring braces
// inside quotes or after an unquoted '#' comment marker.
func bashBraceDelta(line string) int {
	delta := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '#':
			return delta
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}
