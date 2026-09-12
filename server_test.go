package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)
func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExplainConsentCacheAndChange(t *testing.T) {
	root := t.TempDir()
	put(t, root, "main.tf", `resource "aws_s3_bucket" "one" {}`)
	idx, err := Scan(root)
	if err != nil { t.Fatal(err) }
	app := NewApp(idx, Config{Provider: "openai", Model: "test-model", MaxInputTokens: 6000, MaxOutputTokens: 300, SessionInputBudget: 12000})
	t.Setenv("OPENAI_API_KEY", "test-not-a-real-key")
	old := modelClient
	t.Cleanup(func() { modelClient = old })
	calls := 0
	modelClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.openai.com" { t.Fatal("unexpected endpoint") }
		data, _ := io.ReadAll(r.Body)
		if !bytes.Contains(data, []byte("aws_s3_bucket")) { t.Fatal("source absent") }
		body := `{"output":[{"content":[{"type":"output_text","text":"Creates a bucket."}]}],"usage":{"input_tokens":90,"output_tokens":8}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	unit := idx.Files[0].Units[0]
	post := func(approved bool, origin string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(ExplainRequest{Path:"main.tf", UnitID:unit.ID, Approved:approved})
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:1234/api/explain", bytes.NewReader(payload))
		req.Header.Set("Origin", origin)
		out := httptest.NewRecorder()
		app.Routes().ServeHTTP(out, req)
		return out
	}
	if got := post(true, "https://evil.example").Code; got != 403 { t.Fatalf("cross-origin status %d", got) }
	if got := post(false, "http://127.0.0.1:1234").Code; got != 400 { t.Fatalf("missing consent status %d", got) }
	for i:=0; i<2; i++ { if got:=post(true, "http://127.0.0.1:1234").Code; got != 200 { t.Fatalf("explain status %d", got) } }
	if calls != 1 { t.Fatalf("expected one model call, got %d", calls) }
	put(t, root, "main.tf", `resource "aws_s3_bucket" "one" { bucket = "new" }`)
	if got:=post(true, "http://127.0.0.1:1234").Code; got != 200 { t.Fatalf("updated source status %d", got) }
	if calls != 2 { t.Fatalf("changed source should invalidate cache: %d", calls) }
}
