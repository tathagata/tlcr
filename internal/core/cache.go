package core

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

func cachePath(root, key string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tlcr", Hash(root)[:16], key+".json"), nil
}

func readCache(root, key string) (Explanation, bool) {
	path, err := cachePath(root, key)
	if err != nil {
		return Explanation{}, false
	}
	// Read legacy state only at this boundary; all new writes use the canonical path.
	legacy := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(path))), "coderead", Hash(root)[:16], key+".json")
	for _, candidate := range []string{path, legacy} {
		data, err := readCacheFile(candidate)
		if err != nil || len(data) > 1024*1024 {
			continue
		}
		var result Explanation
		if json.Unmarshal(data, &result) == nil && result.Text != "" {
			return result, true
		}
	}
	return Explanation{}, false
}

func writeCache(root, key string, result Explanation) {
	path, err := cachePath(root, key)
	if err != nil {
		return
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	data, err := json.Marshal(result)
	if err != nil {
		return
	}
	if len(data) > 1024*1024 {
		return
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".explanation-*")
	if err != nil {
		return
	}
	name := file.Name()
	defer func() { _ = file.Close(); _ = os.Remove(name) }()
	if _, err = file.Write(data); err != nil {
		return
	}
	if err = file.Close(); err != nil {
		return
	}
	_ = os.Rename(name, path)
}

func readCacheFile(path string) ([]byte, error) {
	file, err := os.Open(path) // #nosec G304 -- fixed cache namespaces plus hashed repository and request identities.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, 1024*1024+1))
}
