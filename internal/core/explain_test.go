package core

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func explainFixture(t *testing.T) (*Index, Config, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	put(t, root, "main.go", "package main\nfunc main(){}\n")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	return idx, Config{Provider: "test", Model: "fake", MaxInputTokens: 6000, MaxOutputTokens: 800, SessionInputBudget: 24000}, idx.Files[0].Units[0].ID
}

func TestExplanationCoreCacheAndErrors(t *testing.T) {
	idx, cfg, id := explainFixture(t)
	calls := 0
	service := NewExplanationService(cfg, func(_ context.Context, _ Config, prompt string) (Explanation, error) {
		calls++
		if !strings.Contains(prompt, "func main()") {
			t.Fatal("missing source")
		}
		return Explanation{Text: "test", InputTokens: 50}, nil
	})
	first, err := service.Explain(context.Background(), idx, "main.go", id)
	if err != nil || first.Cached {
		t.Fatal(err)
	}
	second, err := service.Explain(context.Background(), idx, "main.go", id)
	if err != nil || !second.Cached || calls != 1 || service.Used() != 50 {
		t.Fatal("cache/usage mismatch")
	}
	_, err = service.Explain(context.Background(), idx, "../secret.go", id)
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != NotFound {
		t.Fatalf("typed missing-source error: %v", err)
	}
}

func TestExplanationBudgetReservationAndRollback(t *testing.T) {
	idx, cfg, id := explainFixture(t)
	cfg.SessionInputBudget = 250
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	service := NewExplanationService(cfg, func(context.Context, Config, string) (Explanation, error) {
		calls.Add(1)
		close(entered)
		<-release
		return Explanation{}, errors.New("provider failed")
	})
	done := make(chan error, 1)
	go func() { _, err := service.Explain(context.Background(), idx, "main.go", id); done <- err }()
	<-entered
	_, err := service.Explain(context.Background(), idx, "main.go", id)
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != BudgetExceeded {
		t.Errorf("concurrent reservation failed: %v", err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("expected model failure")
	}
	if service.Used() != 0 || calls.Load() != 1 {
		t.Fatal("reservation not rolled back")
	}
}
