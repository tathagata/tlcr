package core

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
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
		data, err := readCacheFileRetrying(candidate)
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
	renameCacheFile(name, path)
}

// cacheRaceRetries and cacheRaceBackoff bound how long a writer or reader
// waits out a concurrent replacement of the same cache entry before giving
// up. Windows can transiently fail a rename-over-existing-file with a
// sharing violation when two writers race to replace the same destination
// (POSIX rename(2) has no such window), and a reader's open can likewise
// transiently conflict with an in-flight replace of the same path. Both are
// retried rather than treated as permanent, since the operation is safe to
// retry and the racing write carries identical content in every caller
// today. A genuine "does not exist" from the reader is never retried here:
// that is the overwhelmingly common cache-miss case and must stay fast.
const cacheRaceRetries = 5

var cacheRaceBackoff = 5 * time.Millisecond

// renameCacheFile retries a bounded number of times; see cacheRaceRetries.
func renameCacheFile(name, path string) {
	for attempt := 0; attempt < cacheRaceRetries; attempt++ {
		if err := os.Rename(name, path); err == nil {
			return
		}
		time.Sleep(time.Duration(attempt+1) * cacheRaceBackoff)
	}
}

// readCacheFileRetrying retries a bounded number of times on anything other
// than "does not exist"; see cacheRaceRetries.
func readCacheFileRetrying(path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < cacheRaceRetries; attempt++ {
		data, err := readCacheFile(path)
		if err == nil {
			return data, nil
		}
		if os.IsNotExist(err) {
			return nil, err
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * cacheRaceBackoff)
	}
	return nil, lastErr
}

func readCacheFile(path string) ([]byte, error) {
	file, err := os.Open(path) // #nosec G304 -- fixed cache namespaces plus hashed repository and request identities.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, 1024*1024+1))
}
