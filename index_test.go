package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tathagata/coderead/internal/core"
)

func put(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigAndHash(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "config.json")
	if _, err := loadConfig(file); err != nil {
		t.Fatal(err)
	}
	put(t, root, "config.json", `{"provider":"anthropic","model":"example","max_input_tokens":2000,"max_output_tokens":300,"session_input_budget":4000}`)
	c, err := loadConfig(file)
	if err != nil || c.Provider != "anthropic" {
		t.Fatalf("config: %#v %v", c, err)
	}
	if core.Hash("a", "bc") == core.Hash("ab", "c") {
		t.Fatal("hash boundary collision")
	}
}
