package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/tathagata/coderead/internal/core"
)

func TestTourCLIUsesLocalCore(t *testing.T) {
	root := t.TempDir()
	put(t, root, "main.go", "package main\nfunc main(){work()}\nfunc work(){}\n")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	if err := runTour(context.Background(), []string{"--json", root}, &out); err != nil {
		t.Fatal(err)
	}
	var tour core.Tour
	if err := json.Unmarshal(out.Bytes(), &tour); err != nil || len(tour.Stops) != 2 {
		t.Fatalf("tour CLI: %s %v", out.String(), err)
	}
	if tour.Stops[0].Node.Name != "func main" || tour.Stops[1].Node.Name != "func work" {
		t.Fatal("CLI and source graph disagree")
	}
}
