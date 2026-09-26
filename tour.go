package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/tathagata/coderead/internal/core"
)

// runTour is a thin CLI surface over the same local graph used by the browser.
func runTour(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("tlcr tour", flag.ContinueOnError)
	flags.SetOutput(out)
	kind := flags.String("kind", "architecture", "architecture, execution, state, testing, or recent")
	asJSON := flags.Bool("json", false, "emit the shared Tour API as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: tlcr tour [--kind architecture] [--json] [repository]")
	}
	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	idx, err := core.Scan(root)
	if err != nil {
		return err
	}
	tour, err := core.NewRepository(idx).Tour(ctx, *kind)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(out).Encode(tour)
	}
	if _, err := fmt.Fprintf(out, "%s · %d stops · local analysis\n\n", tour.Title, len(tour.Stops)); err != nil {
		return err
	}
	for i, stop := range tour.Stops {
		if _, err := fmt.Fprintf(out, "%d. %s\n   %s:%d–%d\n   %s\n   Evidence: %s · %s:%d\n\n", i+1, stop.Node.Name, stop.Node.Path, stop.Node.Start, stop.Node.End, stop.Reason, stop.Provenance.Provider, stop.Provenance.Path, stop.Provenance.Start); err != nil {
			return err
		}
	}
	for _, limitation := range tour.Limitations {
		if _, err := fmt.Fprintln(out, limitation); err != nil {
			return err
		}
	}
	return nil
}
