package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

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
	asJSON := flags.Bool("json", false, "emit changed source, relationships, and the Change Tour as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: tlcr review [--base REV] [--head REV] [--commit REV] [--staged] [--unstaged] [--json] [repository]")
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
		if _, err := fmt.Fprintf(out, "%d. %s · %s\n   %s:%d · %s\n   Relationships: +%d / -%d\n", i+1, change.Status, change.Node.Name, change.Node.Path, change.Node.Start, change.Cohort, len(change.RelationshipsAdded), len(change.RelationshipsRemoved)); err != nil {
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
