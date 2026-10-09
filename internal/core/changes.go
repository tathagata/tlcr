package core

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxListedCommits  = 30
	maxListedBranches = 20
)

// ReviewableChange is one change a reader can pick, with the exact selection
// to pass to ReviewChange. Title and Detail come from Git and are untrusted
// display text: bounded, single-line and never interpreted.
type ReviewableChange struct {
	Selection ChangeSelection `json:"selection"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Detail    string          `json:"detail"`
	ID        string          `json:"id,omitempty"`
	Date      string          `json:"date,omitempty"`
	Files     int             `json:"files"`
}

// ChangeList is everything reviewable in the local repository right now.
type ChangeList struct {
	Branch        string             `json:"branch"`
	DefaultBranch string             `json:"default_branch"`
	Working       []ReviewableChange `json:"working"`
	Commits       []ReviewableChange `json:"commits"`
	Branches      []ReviewableChange `json:"branches"`
	Notes         []string           `json:"notes"`
}

// Changes lists uncommitted work, recent commits and branches from local Git
// only. A missing repository or an unborn branch yields notes, not an error.
func (r *Repository) Changes(ctx context.Context) (ChangeList, error) {
	idx := r.Snapshot()
	if err := idx.CheckPolicy(); err != nil {
		return ChangeList{}, err
	}
	list := ChangeList{Working: []ReviewableChange{}, Commits: []ReviewableChange{}, Branches: []ReviewableChange{}, Notes: []string{}}
	if !gitSucceeds(ctx, idx.Root, "rev-parse", "--git-dir") {
		list.Notes = append(list.Notes, "Not a Git repository, or Git is unavailable: there are no changes to list.")
		return list, nil
	}
	if name, err := gitRead(ctx, idx.Root, nil, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		list.Branch = displayText(string(name), 120)
	}
	born := gitSucceeds(ctx, idx.Root, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	list.Working = workingChanges(ctx, idx, born)
	if !born {
		list.Notes = append(list.Notes, "This branch has no commits yet: only staged and unstaged work can be reviewed.")
		return list, nil
	}
	if list.Branch == "" {
		list.Notes = append(list.Notes, "HEAD is detached: recent commits are listed from the checked-out commit.")
	}
	list.Commits = recentCommits(ctx, idx.Root)
	base := defaultBranch(ctx, idx.Root)
	if base == "" {
		list.Notes = append(list.Notes, "No default branch found (origin/HEAD, main or master): branches are not compared.")
		return list, nil
	}
	list.DefaultBranch = base
	list.Branches = branchChanges(ctx, idx.Root, base, list.Branch)
	return list, nil
}

func gitSucceeds(ctx context.Context, root string, args ...string) bool {
	_, err := gitRead(ctx, root, nil, args...)
	return err == nil
}

// displayText makes Git-supplied text safe to show on one bounded line.
func displayText(text string, limit int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return string(runes)
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

// changedPaths runs one name-only listing and keeps indexed or permitted paths.
func changedPaths(ctx context.Context, idx *Index, indexedOnly bool, args ...string) map[string]bool {
	paths := map[string]bool{}
	data, err := gitRun(ctx, idx.Root, nil, 1024*1024, gitListTimeout, args...)
	if err != nil {
		return paths
	}
	for _, name := range strings.Split(string(data), "\x00") {
		if name == "" || !permitted(idx.Root, name) {
			continue
		}
		if _, err := idx.Entry(name); err != nil && indexedOnly {
			continue
		}
		paths[name] = true
	}
	return paths
}

func workingChanges(ctx context.Context, idx *Index, born bool) []ReviewableChange {
	staged := changedPaths(ctx, idx, false, "diff", "--cached", "--name-only", "-z", "--relative", "--no-renames", "--no-ext-diff", "--no-textconv")
	unstaged := changedPaths(ctx, idx, false, "diff", "--name-only", "-z", "--relative", "--no-renames", "--no-ext-diff", "--no-textconv")
	untracked := changedPaths(ctx, idx, true, "ls-files", "--others", "--exclude-standard", "-z")
	all := map[string]bool{}
	for _, set := range []map[string]bool{staged, unstaged, untracked} {
		for name := range set {
			all[name] = true
		}
	}
	base := ""
	if !born {
		base = SideEmpty
	}
	detail := plural(len(staged), "staged file") + " · " + plural(len(unstaged), "unstaged file") + " · " + plural(len(untracked), "untracked file")
	return []ReviewableChange{
		{Kind: "uncommitted", Title: "Uncommitted changes", Detail: detail, Files: len(all), Selection: ChangeSelection{Base: base}},
		{Kind: "staged", Title: "Staged changes", Detail: "What the next commit would contain", Files: len(staged), Selection: ChangeSelection{Base: base, Head: SideIndex}},
		{Kind: "unstaged", Title: "Unstaged changes", Detail: "Working tree against the index, including untracked files", Files: len(unstaged) + len(untracked), Selection: ChangeSelection{Base: SideIndex}},
	}
}

// recentCommits lists the newest commits reachable from HEAD. The file count
// is relative to the analysis root and absent for merges.
func recentCommits(ctx context.Context, root string) []ReviewableChange {
	commits := []ReviewableChange{}
	data, err := gitRun(ctx, root, nil, 1024*1024, gitListTimeout, "log", "-n", strconv.Itoa(maxListedCommits), "--no-show-signature", "--no-renames", "--no-ext-diff", "--no-textconv", "--relative", "--shortstat", "--date=short", "--format=%x1e%H%x1f%P%x1f%ad%x1f%an%x1f%s%x1f")
	if err != nil {
		return commits
	}
	for _, record := range strings.Split(string(data), "\x1e") {
		fields := strings.Split(record, "\x1f")
		if len(fields) != 6 || !objectID.MatchString(fields[0]) {
			continue
		}
		commit := ReviewableChange{Kind: "commit", ID: fields[0][:12], Date: displayText(fields[2], 10), Title: displayText(fields[4], 120), Selection: ChangeSelection{Commit: fields[0]}}
		if commit.Title == "" {
			commit.Title = "(no subject)"
		}
		commit.Files, _ = strconv.Atoi(firstField(fields[5]))
		commit.Detail = displayText(fields[3], 60)
		if strings.Contains(fields[1], " ") {
			commit.Detail += " · merge, shown against its first parent"
		} else {
			commit.Detail += " · " + plural(commit.Files, "file")
		}
		commits = append(commits, commit)
	}
	return commits
}

func firstField(text string) string {
	if fields := strings.Fields(text); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// defaultBranch prefers the remote's advertised default, then local
// convention; nothing is fetched to find it.
func defaultBranch(ctx context.Context, root string) string {
	if name, err := gitRead(ctx, root, nil, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if short := strings.TrimSpace(string(name)); short != "" && len(short) <= 200 {
			return short
		}
	}
	for _, name := range []string{"main", "master"} {
		if gitSucceeds(ctx, root, "rev-parse", "--verify", "--quiet", "refs/heads/"+name+"^{commit}") {
			return name
		}
	}
	return ""
}

// branchChanges compares local and remote-tracking branches with their merge
// base against the default branch; branches with nothing ahead are omitted.
func branchChanges(ctx context.Context, root, base, current string) []ReviewableChange {
	branches := []ReviewableChange{}
	baseID, err := gitRead(ctx, root, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", base+"^{commit}")
	if err != nil {
		return branches
	}
	data, err := gitRun(ctx, root, nil, 1024*1024, gitListTimeout, "for-each-ref", "--sort=refname", "--count=200", "--format=%(refname)%1f%(objectname)%1f%(committerdate:short)%1e", "refs/heads", "refs/remotes")
	if err != nil {
		return branches
	}
	seen := map[string]bool{strings.TrimSpace(string(baseID)): true}
	for _, record := range strings.Split(string(data), "\x1e") {
		fields := strings.Split(strings.TrimSpace(record), "\x1f")
		if len(fields) != 3 || !objectID.MatchString(fields[1]) || seen[fields[1]] || strings.HasSuffix(fields[0], "/HEAD") {
			continue
		}
		seen[fields[1]] = true
		name := strings.TrimPrefix(strings.TrimPrefix(fields[0], "refs/heads/"), "refs/remotes/")
		fork, err := gitRead(ctx, root, nil, "merge-base", strings.TrimSpace(string(baseID)), fields[1])
		forkID := strings.TrimSpace(string(fork))
		if err != nil || !objectID.MatchString(forkID) || forkID == fields[1] {
			continue
		}
		ahead, err := gitRead(ctx, root, nil, "rev-list", "--count", forkID+".."+fields[1])
		if err != nil {
			continue
		}
		count, _ := strconv.Atoi(strings.TrimSpace(string(ahead)))
		detail := plural(count, "commit") + " ahead of " + displayText(base, 60)
		if name == current {
			detail = "Current branch · " + detail
		}
		branches = append(branches, ReviewableChange{Kind: "branch", Title: displayText(name, 120), Detail: detail, ID: fields[1][:12], Date: displayText(fields[2], 10), Selection: ChangeSelection{Base: forkID, Head: fields[1]}})
	}
	sort.SliceStable(branches, func(i, j int) bool {
		if a, b := strings.HasPrefix(branches[i].Detail, "Current"), strings.HasPrefix(branches[j].Detail, "Current"); a != b {
			return a
		}
		return branches[i].Date > branches[j].Date
	})
	if len(branches) > maxListedBranches {
		branches = branches[:maxListedBranches]
	}
	return branches
}
