package core

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Evidence is a repository fact or document/history observation, never model inference.
type Evidence struct {
	Provider   string     `json:"provider"`
	Kind       string     `json:"kind"`
	Label      string     `json:"label"`
	Detail     string     `json:"detail"`
	NodeID     string     `json:"node_id,omitempty"`
	Provenance Provenance `json:"provenance"`
}

// EvidenceQuery is independent of browser, HTTP and model concerns.
type EvidenceQuery struct {
	Index *Index
	Graph *Graph
	Node  Node
}

// EvidenceProvider contributes bounded local observations; optional providers can fail independently.
type EvidenceProvider interface {
	Name() string
	Evidence(context.Context, EvidenceQuery) ([]Evidence, error)
}

// EvidenceResult exposes incomplete evidence rather than silently presenting an empty result as certainty.
type EvidenceResult struct {
	Items       []Evidence   `json:"items"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// CollectEvidence isolates provider failures and caps the result used by every surface.
func CollectEvidence(ctx context.Context, q EvidenceQuery, providers ...EvidenceProvider) EvidenceResult {
	result := EvidenceResult{Items: []Evidence{}, Diagnostics: []Diagnostic{}}
	for _, p := range providers {
		if err := ctx.Err(); err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Path: q.Node.Path, Message: "Evidence request cancelled"})
			break
		}
		items, err := p.Evidence(ctx, q)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Path: q.Node.Path, Message: p.Name() + ": " + err.Error()})
			continue
		}
		for _, e := range items {
			if len(result.Items) >= 24 {
				break
			}
			if e.Provenance.Path != "" {
				if _, err := q.Index.Entry(e.Provenance.Path); err != nil || !permitted(q.Index.Root, e.Provenance.Path) {
					continue
				}
			}
			result.Items = append(result.Items, e)
		}
	}
	return result
}

// DefaultEvidenceProviders includes only local read-only sources. No model is needed.
func DefaultEvidenceProviders() []EvidenceProvider {
	return []EvidenceProvider{StructureEvidence{}, DocumentEvidence{}, GitEvidence{}}
}

// StructureEvidence exposes relationships without hiding them in AI prompts.
type StructureEvidence struct{}

// Name identifies the local relationship provider.
func (StructureEvidence) Name() string { return "Source relationships" }

// Evidence returns tests and module relationships already present in the graph.
func (StructureEvidence) Evidence(_ context.Context, q EvidenceQuery) ([]Evidence, error) {
	out := []Evidence{}
	for _, edge := range q.Graph.Outgoing(q.Node.ID) {
		if edge.Kind != "tested-by" && edge.Kind != "local-module-source" && edge.Kind != "route-to-handler" {
			continue
		}
		node, _ := q.Graph.Node(edge.To)
		out = append(out, Evidence{Provider: "Source relationships", Kind: edge.Kind, Label: node.Name, Detail: edge.Provenance.Detail, Provenance: edge.Provenance, NodeID: node.ID})
	}
	return out, nil
}

// DocumentEvidence conservatively matches explicit file references in local ADRs/changelogs.
type DocumentEvidence struct{}

// Name identifies the document provider.
func (DocumentEvidence) Name() string { return "Local documents" }

// Evidence returns exact path references from locally readable ADRs and changelogs.
func (DocumentEvidence) Evidence(ctx context.Context, q EvidenceQuery) ([]Evidence, error) {
	out := []Evidence{}
	for _, f := range q.Index.Files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if !isEvidenceDocument(f) {
			continue
		}
		source, _, err := q.Index.Read(f.Path)
		if err != nil {
			continue
		}
		line, number := documentReference(source, q.Node.Path)
		if number == 0 {
			continue
		}
		out = append(out, Evidence{Provider: "Local documents", Kind: "document-reference", Label: "Explicit reference in " + f.Path, Detail: line, Provenance: Provenance{Provider: "Local document", Path: f.Path, Start: number, End: number, Detail: "Exact file-path reference; relevance is not proof of design intent"}, NodeID: "file:" + f.Path})
		if len(out) >= 6 {
			break
		}
	}
	return out, nil
}
func isEvidenceDocument(f FileEntry) bool {
	if f.Kind != "document" {
		return false
	}
	lower := strings.ToLower(f.Path)
	for _, prefix := range []string{"docs/adr/", "docs/decisions/", "adr/"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return lower == "changelog.md" || lower == "changes.md"
}
func documentReference(source, path string) (string, int) {
	for i, line := range strings.Split(source, "\n") {
		tokens := strings.FieldsFunc(line, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_./-", r)
		})
		for _, token := range tokens {
			if token == path {
				if len(line) > 400 {
					line = line[:400]
				}
				return line, i + 1
			}
		}
	}
	return "", 0
}

// GitEvidence reports file history, not a semantic explanation of why a unit exists.
type GitEvidence struct{}

// Name identifies the local Git provider.
func (GitEvidence) Name() string { return "Local Git history" }

// Evidence returns bounded file-level history without inferring design intent.
func (GitEvidence) Evidence(ctx context.Context, q EvidenceQuery) ([]Evidence, error) {
	data, err := gitRead(ctx, q.Index.Root, nil, "log", "-5", "--no-renames", "--no-show-signature", "--format=%H%x00%ct%x00%s", "--", q.Node.Path)
	if err != nil {
		return nil, err
	}
	out := []Evidence{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.SplitN(line, "\x00", 3)
		if len(fields) != 3 || !objectID.MatchString(fields[0]) {
			continue
		}
		seconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		subject := fields[2]
		if len(subject) > 240 {
			subject = subject[:240]
		}
		detail := time.Unix(seconds, 0).UTC().Format("2006-01-02") + " · " + subject
		out = append(out, Evidence{Provider: "Local Git history", Kind: "commit", Label: fields[0][:12], Detail: detail, Provenance: Provenance{Provider: "Git log", Path: q.Node.Path, Start: 0, End: 0, Detail: "Commit touches this file; not necessarily this unit", Commit: fields[0]}, NodeID: ""})
	}
	if count, err := gitRead(ctx, q.Index.Root, nil, "rev-list", "--count", "--since=90.days", "HEAD", "--", q.Node.Path); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(count))); err == nil {
			out = append(out, Evidence{Provider: "Local Git history", Kind: "churn", Label: fmt.Sprintf("%d commits in 90 days", n), Detail: "File-level change frequency in available local history; shallow clones may be incomplete.", Provenance: Provenance{Provider: "Git rev-list", Path: q.Node.Path, Start: 0, End: 0, Detail: "Count over the trailing 90 days", Commit: ""}, NodeID: ""})
		}
	}
	return out, nil
}

// EvidenceText serializes the same facts visible to humans, under a global byte bound.
func EvidenceText(items []Evidence, limit int) string {
	sorted := append([]Evidence(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Provider+sorted[i].Label < sorted[j].Provider+sorted[j].Label })
	var out strings.Builder
	for _, e := range sorted {
		line := fmt.Sprintf("[%s / %s] %s: %s (%s:%d)\n", e.Provider, e.Kind, e.Label, e.Detail, e.Provenance.Path, e.Provenance.Start)
		if out.Len()+len(line) > limit {
			break
		}
		out.WriteString(line)
	}
	return out.String()
}
