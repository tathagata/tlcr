package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
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

func TestOpenAICompatibleAdapter(t *testing.T) {
	t.Setenv("EXAMPLE_MODEL_KEY", "test-not-a-real-key")
	old := model.Client
	t.Cleanup(func() { model.Client = old })
	var seen *http.Request
	var body map[string]any
	reply := `{"choices":[{"message":{"content":"Reads the source."}}],"usage":{"prompt_tokens":30,"completion_tokens":6}}`
	model.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r
		body = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(reply)), Header: make(http.Header)}, nil
	})}
	chat := Config{Provider: "openai-compatible", Endpoint: "https://models.example.com/inference/chat/completions", APIKeyEnv: "EXAMPLE_MODEL_KEY", Model: "vendor/test-model", MaxOutputTokens: 300}
	result, err := model.Call(context.Background(), chat, "explain source")
	if err != nil || result.Text != "Reads the source." || result.InputTokens != 30 || result.OutputTokens != 6 {
		t.Fatalf("chat result: %#v %v", result, err)
	}
	if seen.URL.String() != chat.Endpoint || seen.Header.Get("Authorization") != "Bearer test-not-a-real-key" || body["model"] != "vendor/test-model" || body["max_completion_tokens"] != float64(300) {
		t.Fatalf("chat request: %v %v %v", seen.URL, seen.Header, body)
	}
	t.Setenv("EXAMPLE_MODEL_KEY", "")
	if _, err := model.Call(context.Background(), chat, "explain source"); err == nil || !strings.Contains(err.Error(), "EXAMPLE_MODEL_KEY") {
		t.Fatalf("missing key: %v", err)
	}
}

// Azure-style: the Responses format, with the key in an api-key header.
func TestOpenAICompatibleResponsesAndLocal(t *testing.T) {
	t.Setenv("EXAMPLE_MODEL_KEY", "test-not-a-real-key")
	old := model.Client
	t.Cleanup(func() { model.Client = old })
	var seen *http.Request
	var body map[string]any
	reply := `{"output":[{"content":[{"type":"output_text","text":"Reads the source."}]}],"usage":{"input_tokens":30,"output_tokens":6}}`
	model.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r
		body = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(reply)), Header: make(http.Header)}, nil
	})}
	azure := Config{Provider: "openai-compatible", Endpoint: "https://resource.example.com/openai/v1/responses", API: "responses", Auth: "api-key", APIKeyEnv: "EXAMPLE_MODEL_KEY", Model: "deployment", MaxOutputTokens: 300}
	if result, err := model.Call(context.Background(), azure, "explain source"); err != nil || result.Text != "Reads the source." {
		t.Fatalf("responses result: %#v %v", result, err)
	}
	if seen.Header.Get("api-key") != "test-not-a-real-key" || seen.Header.Get("Authorization") != "" || body["input"] != "explain source" || azure.Destination() != "resource.example.com" {
		t.Fatalf("responses request: %v %v", seen.Header, body)
	}
	// A local server needs no key.
	local := Config{Provider: "openai-compatible", Endpoint: "http://127.0.0.1:11434/v1/chat/completions", Model: "local", MaxOutputTokens: 300}
	reply = `{"choices":[{"message":{"content":"Reads the source."}}]}`
	if _, err := model.Call(context.Background(), local, "explain source"); err != nil || seen.Header.Get("Authorization") != "" {
		t.Fatalf("local request: %v %v", err, seen.Header)
	}
}

func TestOpenAICompatibleConfigIsChecked(t *testing.T) {
	good := Config{Provider: "openai-compatible", Endpoint: "https://resource.example.com/openai/v1/responses", API: "responses", Auth: "api-key", APIKeyEnv: "AZURE_OPENAI_API_KEY"}
	if err := model.Validate(good); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]Config{
		"plain http to another host": {Provider: "openai-compatible", Endpoint: "http://models.example.com/v1/chat/completions"},
		"no endpoint":                {Provider: "openai-compatible"},
		"credentials in the URL":     {Provider: "openai-compatible", Endpoint: "https://user:secret@models.example.com/v1"},
		"unknown format":             {Provider: "openai-compatible", Endpoint: "https://models.example.com/v1", API: "completions"},
		"unknown auth":               {Provider: "openai-compatible", Endpoint: "https://models.example.com/v1", Auth: "basic"},
		"key value, not a name":      {Provider: "openai-compatible", Endpoint: "https://models.example.com/v1", APIKeyEnv: "test-not-a-real-key"},
		"endpoint on openai":         {Provider: "openai", Endpoint: "https://models.example.com/v1"},
		"unknown provider":           {Provider: "azure"},
	} {
		if model.Validate(bad) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestModelRequestsDoNotFollowRedirects(t *testing.T) {
	t.Setenv("EXAMPLE_MODEL_KEY", "test-not-a-real-key")
	other := 0
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { other++ }))
	defer elsewhere.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	cfg := Config{Provider: "openai-compatible", Endpoint: redirect.URL + "/v1/chat/completions", Auth: "api-key", APIKeyEnv: "EXAMPLE_MODEL_KEY", Model: "local", MaxOutputTokens: 300}
	if _, err := model.Call(context.Background(), cfg, "explain source"); err == nil || !strings.Contains(err.Error(), "HTTP 307") || other != 0 {
		t.Fatalf("redirect followed: %v, %d requests elsewhere", err, other)
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
	// The approved payload carries the reading brief, so the model sees what the reader sees.
	if !strings.Contains(prepared.Prompt, "Reading brief (JSON") || !strings.Contains(prepared.Prompt, `"unit":"func Work"`) {
		t.Fatalf("enriched prompt lacks the reading brief:\n%s", prepared.Prompt)
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

func TestReviewAndChangesRoutes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	root := committedFixture(t)
	put(t, root, "main.go", "package main\nfunc main(){ println(1) }\n")
	idx, err := core.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(idx, Config{})
	get := func(target string, into any) int {
		t.Helper()
		out := httptest.NewRecorder()
		app.Routes().ServeHTTP(out, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:1234"+target, nil))
		if into != nil {
			if err := json.Unmarshal(out.Body.Bytes(), into); err != nil {
				t.Fatalf("%s: %v", target, err)
			}
		}
		return out.Code
	}
	var list core.ChangeList
	if code := get("/api/changes", &list); code != 200 || len(list.Working) != 3 || len(list.Commits) != 1 {
		t.Fatalf("changes: %d %#v", code, list)
	}
	assertReviewRoutes(t, get, list.Commits[0].Selection.Commit)
}

func assertReviewRoutes(t *testing.T, get func(string, any) int, commit string) {
	t.Helper()
	var review core.ChangeReview
	if code := get("/api/review", &review); code != 200 || !review.Live || len(review.Changes) != 1 || review.Changes[0].ID == "" || len(review.Changes[0].Hunks) != 1 {
		t.Fatalf("uncommitted review: %d %#v", code, review)
	}
	if code := get("/api/review?commit="+commit, &review); code != 200 || review.Live || review.Base != core.SideEmpty {
		t.Fatalf("commit review: %d %#v", code, review)
	}
	if a, b, c := get("/api/review?base=%3Aworktree", nil), get("/api/review?commit=HEAD&head=HEAD", nil), get("/api/review?head=--help", nil); a != 400 || b != 400 || c != 400 {
		t.Fatalf("invalid selections: %d %d %d", a, b, c)
	}
}

func committedFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, "main.go", "package main\nfunc main(){}\n")
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "Base"}} {
		command := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s %v", output, err)
		}
	}
	return root
}

func TestChangeDescriptionNeedsApprovalOfTheExactPayload(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	root := committedFixture(t)
	for _, name := range []string{"XDG_CACHE_HOME", "HOME", "LocalAppData"} {
		t.Setenv(name, t.TempDir())
	}
	t.Setenv("OPENAI_API_KEY", "test-not-a-real-key")
	put(t, root, "main.go", "package main\nfunc main(){ println(1) }\n")
	idx, err := core.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(idx, Config{Provider: "openai", Model: "fake", MaxInputTokens: 6000, MaxOutputTokens: 300, SessionInputBudget: 12000})
	old := model.Client
	t.Cleanup(func() { model.Client = old })
	calls := 0
	model.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		data, _ := io.ReadAll(r.Body)
		if !bytes.Contains(data, []byte("Give no opinion")) || !bytes.Contains(data, []byte("println(1)")) {
			t.Fatal("change payload incomplete")
		}
		body := `{"output":[{"content":[{"type":"output_text","text":"Now prints 1."}]}],"usage":{"input_tokens":60,"output_tokens":5}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	review, err := app.repository.Review(context.Background(), "HEAD")
	if err != nil || len(review.Changes) != 1 {
		t.Fatalf("review: %v", err)
	}
	post := func(path string, input ChangeExplainRequest) *httptest.ResponseRecorder {
		body, _ := json.Marshal(input)
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:1234"+path, bytes.NewReader(body))
		req.Header.Set("Origin", "http://127.0.0.1:1234")
		out := httptest.NewRecorder()
		app.Routes().ServeHTTP(out, req)
		return out
	}
	input := ChangeExplainRequest{ChangeID: review.Changes[0].ID, Enrich: true}
	var prepared core.PreparedExplanation
	if out := post("/api/review/explain/preview", input); out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &prepared) != nil || prepared.Digest == "" {
		t.Fatalf("preview: %d %s", out.Code, out.Body.String())
	}
	assertChangeConsent(t, post, input, prepared.Digest, &calls)
	put(t, root, "main.go", "package main\nfunc main(){ println(2) }\n")
	input.Approved, input.Digest = true, prepared.Digest
	if out := post("/api/review/explain", input); out.Code != 409 || calls != 1 {
		t.Fatalf("stale source: %d calls=%d", out.Code, calls)
	}
}

func assertChangeConsent(t *testing.T, post func(string, ChangeExplainRequest) *httptest.ResponseRecorder, input ChangeExplainRequest, digest string, calls *int) {
	t.Helper()
	if out := post("/api/review/explain", input); out.Code != 400 || *calls != 0 {
		t.Fatalf("unapproved: %d", out.Code)
	}
	input.Approved, input.Digest = true, "wrong"
	if out := post("/api/review/explain", input); out.Code != 409 || *calls != 0 {
		t.Fatalf("wrong digest: %d", out.Code)
	}
	input.Digest = digest
	out := post("/api/review/explain", input)
	if out.Code != 200 || *calls != 1 || !strings.Contains(out.Body.String(), "Now prints 1.") {
		t.Fatalf("approved: %d %s", out.Code, out.Body.String())
	}
	input.ChangeID = "0000000000000000"
	if out := post("/api/review/explain/preview", input); out.Code != 404 {
		t.Fatalf("unknown change: %d", out.Code)
	}
}
