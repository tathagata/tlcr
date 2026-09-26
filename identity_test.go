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
