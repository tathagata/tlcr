package core

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestCacheRenameCompatibility(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("XDG_CACHE_HOME", base)
	t.Setenv("LocalAppData", base)
	root := t.TempDir()
	key := Hash("approved source", "model", "context")
	current, err := cachePath(root, key)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(filepath.Dir(current))) != "tlcr" {
		t.Fatalf("wrong cache namespace: %s", current)
	}
	legacy := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(current))), "coderead", Hash(root)[:16], key+".json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte(`{"text":"legacy explanation","input_tokens":42}`)
	if err := os.WriteFile(legacy, old, 0600); err != nil {
		t.Fatal(err)
	}
	if got, ok := readCache(root, key); !ok || got.Text != "legacy explanation" {
		t.Fatalf("legacy cache lost: %#v %v", got, ok)
	}
	if _, ok := readCache(t.TempDir(), key); ok {
		t.Fatal("cache leaked across repositories")
	}
	if _, ok := readCache(root, Hash("different source")); ok {
		t.Fatal("cache leaked across keys")
	}
	checkCanonicalCache(t, root, key, legacy, old)
}

func checkCanonicalCache(t *testing.T, root, key, legacy string, old []byte) {
	t.Helper()
	current, err := cachePath(root, key)
	if err != nil {
		t.Fatal(err)
	}
	writeCache(root, key, Explanation{Text: "new explanation"})
	if got, ok := readCache(root, key); !ok || got.Text != "new explanation" {
		t.Fatalf("canonical cache must win: %#v %v", got, ok)
	}
	after, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(old, after) {
		t.Fatal("legacy cache was modified")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(current)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("cache permissions: %v %v", info, err)
		}
	}
	if err := os.WriteFile(current, []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, ok := readCache(root, key); !ok || got.Text != "legacy explanation" {
		t.Fatal("corrupt new cache should fall back to valid legacy entry")
	}
}

func TestPrivateStateDirectoriesStayExcluded(t *testing.T) {
	root := t.TempDir()
	put(t, root, "main.go", "package main\n")
	put(t, root, ".tlcr/private.go", "package secret\n")
	put(t, root, ".coderead/private.go", "package secret\n")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Files) != 1 || idx.Files[0].Path != "main.go" {
		t.Fatalf("private state indexed: %#v", idx.Files)
	}
}

func TestCacheConcurrentReplacement(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("XDG_CACHE_HOME", base)
	t.Setenv("LocalAppData", base)
	root := t.TempDir()
	key := Hash("atomic replacement")
	text := strings.Repeat("explanation ", 1000)
	writeCache(root, key, Explanation{Text: text})
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 10; j++ {
				writeCache(root, key, Explanation{Text: text})
				if got, ok := readCache(root, key); !ok || got.Text != text {
					t.Error("reader observed partial replacement")
				}
			}
		}()
	}
	group.Wait()
	path, err := cachePath(root, key)
	if err != nil {
		t.Fatal(err)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".explanation-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary cache files left behind: %v %v", leftovers, err)
	}
}
