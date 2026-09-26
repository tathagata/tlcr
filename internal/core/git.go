package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var objectID = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("git output limit exceeded")
	}
	return b.Buffer.Write(p)
}

// gitRead invokes fixed read-only operations without shell expansion, optional locks,
// user Git environment overrides, hooks, external diffs, pagers or signature programs.
func gitRead(ctx context.Context, root string, stdin io.Reader, args ...string) ([]byte, error) {
	executable, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	fixed := make([]string, 0, 13+len(args))
	fixed = append(fixed, []string{"--no-pager", "--literal-pathspecs", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.pager=cat", "-c", "log.showSignature=false", "-C", root}...)
	command := exec.CommandContext(ctx, executable, append(fixed, args...)...) // #nosec G204 -- executable resolved from user PATH; internal fixed read-only commands and literal pathspecs, never a shell
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "SYSTEMROOT=" + os.Getenv("SYSTEMROOT"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1"}
	output := &boundedOutput{limit: 256 * 1024}
	command.Stdout = output
	command.Stderr = io.Discard
	command.Stdin = stdin
	if err := command.Run(); err != nil {
		return nil, errors.New("git evidence unavailable (not a repository, timeout, or command failure)")
	}
	return output.Bytes(), nil
}

// BlobIdentity hashes the current approved bytes for tracked files; it never hashes
// the last commit instead of a dirty working tree, applies filters, or writes an object.
func BlobIdentity(ctx context.Context, idx *Index, path, source string) (string, bool) {
	if !permitted(idx.Root, path) {
		return "", false
	}
	if _, err := gitRead(ctx, idx.Root, nil, "ls-files", "--error-unmatch", "--", path); err != nil {
		return "", false
	}
	data, err := gitRead(ctx, idx.Root, strings.NewReader(source), "hash-object", "--no-filters", "--stdin")
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(data))
	return id, objectID.MatchString(id)
}

// RecentFiles returns only indexed files from bounded local history, in Git's order.
func RecentFiles(ctx context.Context, idx *Index) ([]string, error) {
	data, err := gitRead(ctx, idx.Root, nil, "log", "-30", "--no-renames", "--no-show-signature", "--format=", "--name-only", "--relative", "-z", "--")
	if err != nil {
		return nil, err
	}
	out := []string{}
	seen := map[string]bool{}
	for _, name := range strings.Split(string(data), "\x00") {
		if _, err := idx.Entry(name); err != nil || seen[name] || !permitted(idx.Root, name) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}
