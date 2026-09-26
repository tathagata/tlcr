package core

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

// LanguageParser emits source units without requiring a model or external service.
type LanguageParser interface {
	Kind() string
	Match(path string, peek []byte) bool
	Units(path string, data []byte) []Unit
}

// ParserRegistration makes precedence explicit rather than depending on registration order.
type ParserRegistration struct {
	Parser   LanguageParser
	Priority int
}

// Registry classifies files with a bounded content peek and deterministic precedence.
type Registry struct{ parsers []ParserRegistration }

// NewRegistry rejects duplicate identities and ambiguous priorities.
func NewRegistry(parsers ...ParserRegistration) (*Registry, error) {
	seen := map[string]bool{}
	priorities := map[int]bool{}
	copyParsers := append([]ParserRegistration(nil), parsers...)
	for _, p := range copyParsers {
		if p.Parser == nil || p.Parser.Kind() == "" || seen[p.Parser.Kind()] || priorities[p.Priority] {
			return nil, errors.New("parser identity and priority must be unique")
		}
		seen[p.Parser.Kind()] = true
		priorities[p.Priority] = true
	}
	sort.Slice(copyParsers, func(i, j int) bool { return copyParsers[i].Priority > copyParsers[j].Priority })
	return &Registry{parsers: copyParsers}, nil
}

// Parse returns a whole-file unit when a recognized file has no reliable structure.
func (r *Registry) Parse(path string, data []byte) (FileEntry, bool) {
	peek := data
	if len(peek) > 4096 {
		peek = peek[:4096]
	}
	for _, candidate := range r.parsers {
		parser := candidate.Parser
		if !parser.Match(path, peek) {
			continue
		}
		units := uniqueUnits(parser.Units(path, data))
		if len(units) == 0 {
			units = []Unit{{ID: path + ":file", Kind: "file", Name: filepath.Base(path), Start: 1, End: countLines(data)}}
		}
		return FileEntry{Path: path, Kind: parser.Kind(), Units: units}, true
	}
	return FileEntry{}, false
}

type sourceParser struct {
	parse      func(string, []byte) []Unit
	kind       string
	extensions string
}

func (p sourceParser) Kind() string { return p.kind }
func (p sourceParser) Match(path string, _ []byte) bool {
	return strings.Contains(p.extensions, "|"+strings.ToLower(filepath.Ext(path))+"|")
}
func (p sourceParser) Units(path string, data []byte) []Unit {
	if p.parse == nil {
		return nil
	}
	return p.parse(path, data)
}

// DefaultRegistry uses native ASTs where available and explicit file-level fallbacks otherwise.
func DefaultRegistry() *Registry {
	registry, _ := NewRegistry(
		ParserRegistration{Parser: sourceParser{kind: "terraform", extensions: "|.tf|", parse: terraformUnits}, Priority: 100},
		ParserRegistration{Parser: sourceParser{kind: "go", extensions: "|.go|", parse: goUnits}, Priority: 90},
		ParserRegistration{Parser: sourceParser{kind: "frontend", extensions: "|.js|.jsx|.ts|.tsx|.html|.css|", parse: nil}, Priority: 80},
		ParserRegistration{Parser: sourceParser{kind: "document", extensions: "|.md|.mdx|.txt|", parse: nil}, Priority: 10},
	)
	return registry
}
