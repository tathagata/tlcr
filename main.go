package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tathagata/coderead/internal/core"
)

// version is set at build time via -ldflags "-X main.version=...";
// it stays "dev" for local builds that don't pass that flag.
var version = "dev"

type Config = core.Config

func loadConfig(path string) (Config, error) {
	c := Config{MaxInputTokens: 6000, MaxOutputTokens: 800, SessionInputBudget: 24000}
	if path == "" {
		binary, err := os.Executable()
		if err != nil {
			return c, err
		}
		path = filepath.Join(filepath.Dir(binary), "config.json")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is either the --config flag the user passed themselves, or beside our own binary
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	if c.MaxInputTokens < 500 || c.MaxInputTokens > 50000 || c.MaxOutputTokens < 100 || c.MaxOutputTokens > 10000 || c.SessionInputBudget < c.MaxInputTokens {
		return c, fmt.Errorf("invalid token limits in %s", path)
	}
	if c.Provider != "" && c.Provider != "openai" && c.Provider != "anthropic" {
		return c, fmt.Errorf("provider must be openai or anthropic")
	}
	return c, nil
}

func main() {
	if runLocalCommand() {
		return
	}
	flag.Usage = func() {
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "Usage: tlcr [options] [repository] | tlcr tour [--kind architecture] [repository] | tlcr review [--base REV] [--head REV | --commit REV | --staged | --unstaged] [repository]")
		flag.PrintDefaults()
	}
	configPath := flag.String("config", "", "path to config JSON (default: beside binary)")
	noBrowser := flag.Bool("no-browser", false, "print URL without opening browser")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("tlcr " + version)
		return
	}
	if flag.NArg() > 1 {
		log.Fatal("usage: tlcr [--config file] [--no-browser] [repository]")
	}
	root := "."
	if flag.NArg() == 1 {
		root = flag.Arg(0)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		log.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	idx, err := core.Scan(root)
	if err != nil {
		log.Fatal(err)
	}
	app := NewApp(idx, cfg)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	url := "http://" + listener.Addr().String()
	fmt.Printf("tlcr: %s\nReading: %s\n", url, root)
	if !*noBrowser {
		go func() { time.Sleep(250 * time.Millisecond); openBrowser(url) }()
	}
	server := &http.Server{Handler: app.Routes(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.Serve(listener))
}

func openBrowser(url string) {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{url}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		command, args = "xdg-open", []string{url}
	}
	if err := exec.CommandContext(context.Background(), command, args...).Start(); err != nil && !strings.Contains(err.Error(), "executable file not found") { // #nosec G204 -- command/args come from a fixed runtime.GOOS switch and our own loopback URL, not external input
		log.Printf("browser: %v", err)
	}
}

func runLocalCommand() bool {
	if len(os.Args) < 2 {
		return false
	}
	var err error
	switch os.Args[1] {
	case "tour":
		err = runTour(context.Background(), os.Args[2:], os.Stdout)
	case "review":
		err = runReview(context.Background(), os.Args[2:], os.Stdout)
	default:
		return false
	}
	if err != nil {
		log.Fatal(err)
	}
	return true
}
