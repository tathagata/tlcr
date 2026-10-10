package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCanonicalProductSurfaces(t *testing.T) {
	// Legacy names are allowed only at the migration and module boundaries.
	files := []string{"main.go", "ui/index.html", "ui/app.js", "ui/style.css", "README.md", "SECURITY.md", "Dockerfile", "Makefile", ".github/workflows/ci.yml", ".github/workflows/release.yml"}
	allowed := []string{"github.com/tathagata/coderead", "`coderead/<repository-hash>/`", "`.coderead/`"}
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(data), "github.com/tathagata/coderead/internal/", "internal/")
		if name == "README.md" {
			for _, legacy := range allowed {
				text = strings.ReplaceAll(text, legacy, "")
			}
		}
		if regexp.MustCompile(`(?i)\bcode ?read\b`).MatchString(text) {
			t.Errorf("stale product identity in %s", name)
		}
	}
}

func TestRepositoryPortIsStableAndFallsBack(t *testing.T) {
	port := repositoryPort("/some/repository")
	if port != repositoryPort("/some/repository") || port < 20000 || port >= 40000 || port == repositoryPort("/another/repository") {
		t.Fatalf("port %d", port)
	}
	first, stable, err := listen(t.TempDir() + "/fixed")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	if !stable {
		t.Skip("derived port already in use on this machine")
	}
	root := t.TempDir()
	held, stable, err := listen(root)
	if err != nil || !stable {
		t.Skipf("derived port unavailable: %v", err)
	}
	defer func() { _ = held.Close() }()
	second, stable, err := listen(root)
	if err != nil || stable {
		t.Fatalf("a taken port must fall back: %v stable=%v", err, stable)
	}
	_ = second.Close()
}
