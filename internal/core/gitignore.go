package core

import (
	"os"
	"path"
	"regexp"
	"strings"
)

// ignoreRule is one parsed, compiled line from a .gitignore file.
type ignoreRule struct {
	re       *regexp.Regexp
	negate   bool // leading '!'
	dirOnly  bool // trailing '/'
	anchored bool // contains a '/' other than a trailing one
}

// ignoreLayer holds the rules from a single .gitignore file.
type ignoreLayer struct {
	rules []ignoreRule
}

// match reports whether this layer has an opinion on the candidate (matched)
// and, if so, whether its last matching rule excludes it (ignore). relToDir
// is the candidate's path relative to this layer's own directory; base is
// the candidate's file/directory name.
func (l *ignoreLayer) match(relToDir, base string, isDir bool) (matched, ignore bool) {
	for _, r := range l.rules {
		if r.dirOnly && !isDir {
			continue
		}
		var hit bool
		if r.anchored {
			hit = r.re.MatchString(relToDir)
		} else {
			hit = r.re.MatchString(base)
		}
		if hit {
			matched = true
			ignore = !r.negate
		}
	}
	return matched, ignore
}

// loadGitignore reads and compiles root/dir/.gitignore (dir is "" for the
// scan root). It returns nil if the file doesn't exist or has no rules.
func loadGitignore(root, dir string) *ignoreLayer {
	data, err := readWithin(root, path.Join(dir, ".gitignore"), 64*1024)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return &ignoreLayer{rules: []ignoreRule{{re: regexp.MustCompile(".*")}}}
	}
	layer := &ignoreLayer{}
	for _, line := range strings.Split(string(data), "\n") {
		if rule, ok := parseIgnoreLine(line); ok {
			layer.rules = append(layer.rules, rule)
		}
	}
	if len(layer.rules) == 0 {
		return nil
	}
	return layer
}

// parseIgnoreLine parses one .gitignore line. ok is false for blank lines
// and comments.
func parseIgnoreLine(line string) (rule ignoreRule, ok bool) {
	line = strings.TrimRight(line, "\r")
	line = strings.TrimRight(line, " ")
	if line == "" || strings.HasPrefix(line, "#") {
		return ignoreRule{}, false
	}
	switch {
	case strings.HasPrefix(line, `\`):
		line = line[1:] // escaped leading '!' or '#'
	case strings.HasPrefix(line, "!"):
		rule.negate = true
		line = line[1:]
	}
	if line == "" {
		return ignoreRule{}, false
	}
	if strings.HasSuffix(line, "/") {
		rule.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	if line == "" {
		return ignoreRule{}, false
	}
	rule.anchored = strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	rule.re = compileGlob(line)
	return rule, true
}

// compileGlob translates one gitignore pattern (already stripped of its
// optional leading '!', leading anchor '/', and trailing dir-only '/') into
// a regexp matching the full string it will be compared against. It handles
// '*', '?', '[...]' classes, and '**' as a whole path segment (leading,
// trailing, or in the middle). A '**' anywhere else is treated as an
// ordinary '*', since that placement is a rare, poorly-defined construct
// even in git itself.
func compileGlob(pattern string) *regexp.Regexp {
	segments := strings.Split(pattern, "/")
	var b strings.Builder
	needSep := false
	for i, seg := range segments {
		last := i == len(segments)-1
		switch {
		case seg == "**" && len(segments) == 1:
			b.WriteString(".*")
			needSep = false
		case seg == "**" && i == 0:
			b.WriteString("(?:.*/)?")
			needSep = false
		case seg == "**" && last:
			if needSep {
				b.WriteByte('/')
			}
			b.WriteString(".*")
			needSep = false
		case seg == "**":
			if needSep {
				b.WriteByte('/')
			}
			b.WriteString("(?:[^/]+/)*")
			needSep = false
		default:
			if needSep {
				b.WriteByte('/')
			}
			b.WriteString(translateSegment(seg))
			needSep = true
		}
	}
	compiled, err := regexp.Compile("^" + b.String() + "$")
	if err != nil {
		return regexp.MustCompile("^" + regexp.QuoteMeta(pattern) + "$")
	}
	return compiled
}

// translateSegment translates a single path segment (no '/') of a gitignore
// pattern into the equivalent regexp fragment.
func translateSegment(seg string) string {
	var b strings.Builder
	runes := []rune(seg)
	for i := 0; i < len(runes); i++ {
		switch c := runes[i]; c {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '[':
			j := i + 1
			neg := false
			if j < len(runes) && (runes[j] == '!' || runes[j] == '^') {
				neg = true
				j++
			}
			start := j
			for j < len(runes) && runes[j] != ']' {
				j++
			}
			if j >= len(runes) {
				b.WriteString(regexp.QuoteMeta("["))
				continue
			}
			b.WriteByte('[')
			if neg {
				b.WriteByte('^')
			}
			b.WriteString(strings.ReplaceAll(string(runes[start:j]), `\`, `\\`))
			b.WriteByte(']')
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// gitignored reports whether rel (slash-separated, relative to root) is
// excluded by any .gitignore file from root down to its containing
// directory, honoring negation and nested-file precedence the way git
// does: patterns are considered from the root .gitignore down to the most
// deeply nested one that applies, in file order, and the last matching
// pattern overall wins. A directory's own .gitignore doesn't affect the
// directory itself, only what's inside it. cache memoizes loaded files by
// their slash-relative directory ("" for the root) across the whole scan.
func gitignored(root, rel string, isDir bool, cache map[string]*ignoreLayer) bool {
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	base := path.Base(rel)
	ignore := false
	for _, ancestor := range ancestorDirs(dir) {
		layer, ok := cache[ancestor]
		if !ok {
			layer = loadGitignore(root, ancestor)
			cache[ancestor] = layer
		}
		if layer == nil {
			continue
		}
		relToDir := strings.TrimPrefix(rel, ancestor)
		relToDir = strings.TrimPrefix(relToDir, "/")
		if matched, ig := layer.match(relToDir, base, isDir); matched {
			ignore = ig
		}
	}
	return ignore
}

// ancestorDirs returns dir and all of its ancestors, root ("") first. dir
// must be "" or a slash-separated relative path with no leading/trailing
// slash.
func ancestorDirs(dir string) []string {
	if dir == "" {
		return []string{""}
	}
	parts := strings.Split(dir, "/")
	dirs := make([]string, 0, len(parts)+1)
	dirs = append(dirs, "")
	cur := ""
	for _, p := range parts {
		if cur == "" {
			cur = p
		} else {
			cur = cur + "/" + p
		}
		dirs = append(dirs, cur)
	}
	return dirs
}
