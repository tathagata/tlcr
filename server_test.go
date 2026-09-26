package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tathagata/coderead/internal/core"
	"github.com/tathagata/coderead/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExplainConsentCacheAndChange(t *testing.T) {
	root := t.TempDir()
	put(t, root, "main.tf", `resource "aws_s3_bucket" "one" {}`)
	idx, err := core.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(idx, Config{Provider: "openai", Model: "test-model", MaxInputTokens: 6000, MaxOutputTokens: 300, SessionInputBudget: 12000})
	t.Setenv("OPENAI_API_KEY", "test-not-a-real-key")
	old := model.Client
	t.Cleanup(func() { model.Client = old })
	calls := 0
	model.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.openai.com" {
			t.Fatal("unexpected endpoint")
		}
		data, _ := io.ReadAll(r.Body)
		if !bytes.Contains(data, []byte("aws_s3_bucket")) {
			t.Fatal("source absent")
		}
		body := `{"output":[{"content":[{"type":"output_text","text":"Creates a bucket."}]}],"usage":{"input_tokens":90,"output_tokens":8}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	unit := idx.Files[0].Units[0]
	post := func(approved bool, origin string) *httptest.ResponseRecorder {
		prepared, err := app.explanations.Prepare(context.Background(), app.repository.Snapshot(), nil, "main.tf", unit.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(ExplainRequest{Path: "main.tf", UnitID: unit.ID, Approved: approved, Digest: prepared.Digest})
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:1234/api/explain", bytes.NewReader(payload))
		req.Header.Set("Origin", origin)
		out := httptest.NewRecorder()
		app.Routes().ServeHTTP(out, req)
		return out
	}
	if got := post(true, "https://evil.example").Code; got != 403 {
		t.Fatalf("cross-origin status %d", got)
	}
	if got := post(false, "http://127.0.0.1:1234").Code; got != 400 {
		t.Fatalf("missing consent status %d", got)
	}
	for i := 0; i < 2; i++ {
		if got := post(true, "http://127.0.0.1:1234").Code; got != 200 {
			t.Fatalf("explain status %d", got)
		}
	}
	if calls != 1 {
		t.Fatalf("expected one model call, got %d", calls)
	}
	put(t, root, "main.tf", `resource "aws_s3_bucket" "one" { bucket = "new" }`)
	if got := post(true, "http://127.0.0.1:1234").Code; got != 200 {
		t.Fatalf("updated source status %d", got)
	}
	if calls != 2 {
		t.Fatalf("changed source should invalidate cache: %d", calls)
	}
}

func TestAnthropicAdapter(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-not-a-real-key")
	old := model.Client
	t.Cleanup(func() { model.Client = old })
	model.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.anthropic.com" || r.Header.Get("anthropic-version") == "" || r.Header.Get("x-api-key") == "" {
			t.Fatal("incorrect Anthropic request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`{"content":[{"type":"text","text":"Reads the source."}],"usage":{"input_tokens":30,"output_tokens":6}}`)), Header: make(http.Header)}, nil
	})}
	result, err := model.Call(context.Background(), Config{Provider: "anthropic", Model: "test-model", MaxOutputTokens: 300}, "explain source")
	if err != nil || result.Text != "Reads the source." || result.InputTokens != 30 {
		t.Fatalf("result: %#v %v", result, err)
	}
}

func TestExactPreviewAndStructuralEndpoints(t *testing.T) {
	root := t.TempDir()
	put(t, root, "app.go", "package app\nfunc Work(){}\n")
	idx, err := core.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(idx, Config{Provider: "openai", Model: "fake", MaxInputTokens: 6000, MaxOutputTokens: 800, SessionInputBudget: 12000})
	old := model.Client
	t.Cleanup(func() { model.Client = old })
	calls := 0
	model.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("unexpected model call") })}
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), method, "http://127.0.0.1:1234"+path, strings.NewReader(body))
		req.Header.Set("Origin", "http://127.0.0.1:1234")
		out := httptest.NewRecorder()
		app.Routes().ServeHTTP(out, req)
		return out
	}
	for _, path := range []string{"/api/tree", "/api/overview", "/api/commands", "/api/tour?kind=architecture", "/api/node?id=unit:app.go:func%20Work", "/api/evidence?id=unit:app.go:func%20Work"} {
		if got := do("GET", path, ""); got.Code != 200 {
			t.Fatalf("%s: %d %s", path, got.Code, got.Body.String())
		}
	}
	preview := do("POST", "/api/explain/preview", `{"path":"app.go","unit_id":"app.go:func Work","enrich":true}`)
	var prepared core.PreparedExplanation
	if err := json.Unmarshal(preview.Body.Bytes(), &prepared); err != nil || prepared.Digest == "" {
		t.Fatalf("preview: %s", preview.Body.String())
	}
	put(t, root, "app.go", "package app\nfunc Work(){println(\"changed\")}\n")
	body, _ := json.Marshal(ExplainRequest{Path: "app.go", UnitID: "app.go:func Work", Approved: true, Digest: prepared.Digest})
	if got := do("POST", "/api/explain", string(body)); got.Code != 409 {
		t.Fatalf("stale approval: %d %s", got.Code, got.Body.String())
	}
	if calls != 0 {
		t.Fatal("structural browsing/preview/stale consent invoked model")
	}
	if got := do("POST", "/api/explain/preview", `{} {}`); got.Code != 400 {
		t.Fatal("multiple JSON objects accepted")
	}
}

func TestTreeDoesNotRescanUntilExplicitRefresh(t *testing.T) {
	root := t.TempDir()
	put(t, root, "one.go", "package app\nfunc One(){}\n")
	idx, err := core.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(idx, Config{})
	put(t, root, "two.go", "package app\nfunc Two(){}\n")
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(context.Background(), method, "http://127.0.0.1:1234"+path, nil)
		r.Header.Set("Origin", "http://127.0.0.1:1234")
		w := httptest.NewRecorder()
		app.Routes().ServeHTTP(w, r)
		return w
	}
	if strings.Contains(request("GET", "/api/tree").Body.String(), "two.go") {
		t.Fatal("navigation rescanned repository")
	}
	if !strings.Contains(request("POST", "/api/refresh").Body.String(), "two.go") {
		t.Fatal("explicit refresh did not update index")
	}
}

func TestChangedExclusionsPreserveStaleStatus(t *testing.T) {
	root := t.TempDir()
	put(t, root, "app.go", "package app\nfunc Work(){}\n")
	idx, err := core.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(idx, Config{})
	put(t, root, ".gitignore", "app.go\n")
	for _, path := range []string{"/api/file?path=app.go", "/api/tree", "/api/overview"} {
		req := httptest.NewRequestWithContext(context.Background(), "GET", "http://127.0.0.1:1234"+path, nil)
		response := httptest.NewRecorder()
		app.Routes().ServeHTTP(response, req)
		if response.Code != 409 {
			t.Fatalf("%s: expected stale status, got %d", path, response.Code)
		}
	}
}
