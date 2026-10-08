package core

import (
	"path/filepath"
	"regexp"
	"strings"
)

// bashFuncRe matches the start of a function definition: `name() {`,
// `function name {`, or `function name() {`, with any one-line body after
// the brace. A bare `name {` with neither the `function` keyword nor `()` is
// not valid bash function syntax and is rejected by the caller.
var bashFuncRe = regexp.MustCompile(`^\s*(function\s+)?([A-Za-z_][A-Za-z0-9_:.-]*)\s*(\(\s*\))?\s*\{(.*)$`)

// bashHeredocRe matches a heredoc redirection (`<<EOF`, `<<-EOF`, `<<'EOF'`,
// `<< "EOF"`) but not a `<<<` here-string.
var bashHeredocRe = regexp.MustCompile(`(?:^|[^<])<<(-?)\s*(?:'([^']+)'|"([^"]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)

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
// closing brace, and heredoc bodies are skipped entirely. A definition whose
// opening brace is on the following line is not recognized.
func bashUnits(path string, data []byte) []Unit {
	lines := strings.Split(string(data), "\n")
	heredoc := bashHeredocLines(lines)
	units := []Unit{}

	for i := 0; i < len(lines); i++ {
		if heredoc[i] {
			continue
		}
		m := bashFuncRe.FindStringSubmatch(lines[i])
		if m == nil || (m[1] == "" && m[3] == "") {
			continue
		}
		name := "function " + m[2]
		depth := 1 + bashBraceDelta(m[4])
		end := len(lines)
		if depth <= 0 {
			end = i + 1
		}
		for j := i + 1; depth > 0 && j < len(lines); j++ {
			if heredoc[j] {
				continue
			}
			depth += bashBraceDelta(lines[j])
			if depth <= 0 {
				end = j + 1
			}
		}
		units = append(units, Unit{ID: path + ":" + name, Kind: "function", Name: name, Start: i + 1, End: end})
	}
	return uniqueUnits(units)
}

// bashHeredocLines marks heredoc bodies and their terminators. A `<<` with
// no matching terminator line (an arithmetic shift, say) marks nothing.
func bashHeredocLines(lines []string) []bool {
	body := make([]bool, len(lines))
	for i := 0; i < len(lines); i++ {
		m := bashHeredocRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		delim := m[2] + m[3] + m[4]
		for j := i + 1; j < len(lines); j++ {
			candidate := strings.TrimRight(lines[j], "\r")
			if m[1] == "-" {
				candidate = strings.TrimLeft(candidate, "\t")
			}
			if candidate == delim {
				for k := i + 1; k <= j; k++ {
					body[k] = true
				}
				i = j
				break
			}
		}
	}
	return body
}

// bashBraceDelta returns a line's net effect on brace depth, ignoring braces
// inside quotes or after an unquoted '#' comment marker. A '#' only starts a
// comment at the beginning of a word, so `${#items[@]}` and `${name#prefix}`
// stay balanced.
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
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return delta
			}
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}
