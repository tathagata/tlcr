package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(full, []byte(content), 0600); err != nil { t.Fatal(err) }
}

func TestTerraformModuleLinksAndIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	put(t, root, "live/blog/dev/main.tf", `module "blog" {
  source = "../../../modules/lightsail_blog"
}
resource "aws_s3_bucket" "test" {}
`)
	put(t, root, "modules/lightsail_blog/main.tf", `resource "aws_lightsail_instance" "this" {}`)
	put(t, root, "live/blog/dev/terraform.tfvars", `secret = "do-not-send"`)
	put(t, root, ".terraform/providers/provider.tf", `resource "ignored" "test" {}`)
	idx, err := Scan(root)
	if err != nil { t.Fatal(err) }
	if len(idx.Files) != 2 { t.Fatalf("expected 2 safe files, got %#v", idx.Files) }
	entry, err := idx.Entry("live/blog/dev/main.tf")
	if err != nil { t.Fatal(err) }
	if len(entry.Units) != 2 { t.Fatalf("expected module and resource: %#v", entry.Units) }
	if len(entry.Units[0].Links) != 1 || entry.Units[0].Links[0] != "modules/lightsail_blog" { t.Fatalf("module link: %#v", entry.Units[0].Links) }
	if _, _, err := idx.Read("live/blog/dev/terraform.tfvars"); err == nil { t.Fatal("tfvars should not be readable") }
	if _, _, err := idx.Read("../../etc/passwd"); err == nil { t.Fatal("traversal should not be readable") }
}

func TestGoSymbolsAndChangedSource(t *testing.T) {
	root := t.TempDir()
	put(t, root, "app.go", "package main\ntype Thing struct{}\nfunc Work() {}\n")
	idx, err := Scan(root)
	if err != nil { t.Fatal(err) }
	entry, _ := idx.Entry("app.go")
	if len(entry.Units) != 2 || entry.Units[1].Name != "func Work" { t.Fatalf("Go symbols: %#v", entry.Units) }
	put(t, root, "app.go", "package main\nfunc Changed() {}\n")
	source, entry, err := idx.Read("app.go")
	if err != nil { t.Fatal(err) }
	if !strings.Contains(source, "Changed") || currentUnits("app.go", entry, source)[0].Name != "func Changed" { t.Fatal("expected current source units") }
}

func TestConfigAndHash(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "config.json")
	if _, err := loadConfig(file); err != nil { t.Fatal(err) }
	put(t, root, "config.json", `{"provider":"anthropic","model":"example","max_input_tokens":2000,"max_output_tokens":300,"session_input_budget":4000}`)
	c, err := loadConfig(file)
	if err != nil || c.Provider != "anthropic" { t.Fatalf("config: %#v %v", c, err) }
	if hash("a", "bc") == hash("ab", "c") { t.Fatal("hash boundary collision") }
}
