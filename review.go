package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tathagata/coderead/internal/core"
)

func runReview(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("tlcr review", flag.ContinueOnError)
	flags.SetOutput(out)
	base := flags.String("base", "", "local commit/ref to compare from (default HEAD)")
	head := flags.String("head", "", "local commit/ref to compare to (default: the indexed working tree)")
	commit := flags.String("commit", "", "review one local commit against its first parent")
	staged := flags.Bool("staged", false, "review staged changes (HEAD to the index)")
	unstaged := flags.Bool("unstaged", false, "review unstaged and untracked changes (the index to the working tree)")
	list := flags.Bool("list", false, "list what can be reviewed here instead of reviewing")
	asJSON := flags.Bool("json", false, "emit changed source, relationships, and the Change Tour as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: tlcr review [--base REV] [--head REV] [--commit REV] [--staged] [--unstaged] [--list] [--json] [repository]")
	}
	selection := core.ChangeSelection{Base: *base, Head: *head, Commit: *commit}
	if *staged || *unstaged {
		if *staged && *unstaged || selection != (core.ChangeSelection{}) {
			return fmt.Errorf("--staged and --unstaged cannot be combined with each other or with --base, --head or --commit")
		}
		selection = core.ChangeSelection{Head: core.SideIndex}
		if *unstaged {
			selection = core.ChangeSelection{Base: core.SideIndex}
		}
	}
	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	idx, err := core.Scan(root)
	if err != nil {
		return err
	}
	if *list {
		return printChanges(ctx, out, core.NewRepository(idx), *asJSON)
	}
	review, err := core.NewRepository(idx).ReviewChange(ctx, selection)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(out).Encode(review)
	}
	return printReview(out, review)
}

func printReview(out io.Writer, review core.ChangeReview) error {
	if _, err := fmt.Fprintf(out, "Change Tour · %d changed files · %d changed units/context stops · %d generated files collapsed\nBase: %s\nHead: %s\n\n", review.Files, len(review.Changes), review.GeneratedFiles, review.BaseLabel, review.HeadLabel); err != nil {
		return err
	}
	for i, change := range review.Changes {
		if _, err := fmt.Fprintf(out, "%d. %s · %s · +%d −%d lines\n   %s:%d · %s\n   Relationships: +%d / -%d\n", i+1, change.Status, change.Node.Name, change.Added, change.Removed, change.Node.Path, change.Node.Start, change.Cohort, len(change.RelationshipsAdded), len(change.RelationshipsRemoved)); err != nil {
			return err
		}
		for _, related := range change.Related {
			if related.Relationship == "tested-by" || related.Relationship == "called-by" {
				if _, err := fmt.Fprintf(out, "   %s: %s:%d\n", related.Reason, related.Node.Path, related.Node.Start); err != nil {
					return err
				}
			}
		}
	}
	for _, note := range review.Limitations {
		if _, err := fmt.Fprintln(out, note); err != nil {
			return err
		}
	}
	return nil
}

func printChanges(ctx context.Context, out io.Writer, repository *core.Repository, asJSON bool) error {
	list, err := repository.Changes(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(out).Encode(list)
	}
	for _, section := range []struct {
		title   string
		changes []core.ReviewableChange
	}{{"Working tree", list.Working}, {"Recent commits", list.Commits}, {"Branches against " + list.DefaultBranch, list.Branches}} {
		if len(section.changes) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s\n", section.title); err != nil {
			return err
		}
		for _, change := range section.changes {
			if _, err := fmt.Fprintf(out, "  %s\n      %s\n      %s\n", strings.TrimSpace(change.ID+" "+change.Title), strings.TrimSpace(change.Date+" "+change.Detail), reviewCommand(change.Selection)); err != nil {
				return err
			}
		}
	}
	for _, note := range list.Notes {
		if _, err := fmt.Fprintln(out, note); err != nil {
			return err
		}
	}
	return nil
}

// reviewCommand spells a selection as the flags that reproduce it.
func reviewCommand(selection core.ChangeSelection) string {
	switch {
	case selection.Commit != "":
		return "tlcr review --commit " + selection.Commit[:12]
	case selection == core.ChangeSelection{Head: core.SideIndex}:
		return "tlcr review --staged"
	case selection == core.ChangeSelection{Base: core.SideIndex}:
		return "tlcr review --unstaged"
	}
	command := "tlcr review"
	for _, side := range [][2]string{{"--base", selection.Base}, {"--head", selection.Head}} {
		if len(side[1]) > 12 && !strings.HasPrefix(side[1], ":") {
			side[1] = side[1][:12]
		}
		if side[1] != "" {
			command += " " + side[0] + " " + side[1]
		}
	}
	return command
}
