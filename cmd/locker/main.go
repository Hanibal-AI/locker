package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/Hanibal-AI/locker/internal/config"
	"github.com/Hanibal-AI/locker/internal/pii"
	"github.com/Hanibal-AI/locker/internal/providers"
	"github.com/Hanibal-AI/locker/internal/proxy"
)

// version, commit, and date are set at build time via -ldflags by
// goreleaser (.goreleaser.yaml) — see Docs/roadmap.md Phase 7.1. They
// stay at these defaults for a local `go build`.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Fprint(os.Stdout, versionString())
			return
		case "validate-config":
			os.Exit(runValidateConfig(args[1:], os.Stdout, os.Stderr))
		case "completion":
			os.Exit(runCompletion(args[1:], os.Stdout, os.Stderr))
		case "start":
			args = args[1:]
		}
	}
	runStart(args)
}

// runStart parses args and blocks forever serving the proxy, or exits
// the process on a fatal startup error — the one command here not
// structured as a testable pure function, since its job is to run
// http.ListenAndServe until the process is killed.
func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config.yaml")
	_ = fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	provider, err := providers.New(cfg.Provider, cfg.ActiveProvider())
	if err != nil {
		log.Fatalf("provider error: %v", err)
	}

	piiEngine, err := pii.NewEngine(cfg.PII)
	if err != nil {
		log.Fatalf("pii config error: %v", err)
	}

	server := proxy.New(proxy.Options{
		Provider:            provider,
		RequestTimeout:      cfg.RequestTimeout,
		AllowedModels:       cfg.AllowedModels,
		PII:                 piiEngine,
		StreamLookbackBytes: cfg.PII.StreamLookbackBytes,
		StreamIdleTimeout:   cfg.StreamIdleTimeout,
	})

	log.Printf("locker %s listening on %s (provider=%s, pii_masking=%t)", version, cfg.ListenAddr, cfg.Provider, !cfg.PII.Disabled)
	if err := http.ListenAndServe(cfg.ListenAddr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}

// runValidateConfig loads and validates config the same way runStart
// would, without starting a server, and reports the outcome to stdout/
// stderr. Returns the process exit code.
func runValidateConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate-config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "config.yaml", "path to config.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "config error: %v\n", err)
		return 1
	}
	if _, err := providers.New(cfg.Provider, cfg.ActiveProvider()); err != nil {
		fmt.Fprintf(stderr, "provider error: %v\n", err)
		return 1
	}
	if _, err := pii.NewEngine(cfg.PII); err != nil {
		fmt.Fprintf(stderr, "pii config error: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "config OK: listen_addr=%s provider=%s pii_masking=%t\n", cfg.ListenAddr, cfg.Provider, !cfg.PII.Disabled)
	return 0
}

func versionString() string {
	return fmt.Sprintf("locker version %s (commit %s, built %s)\n", version, commit, date)
}

func runCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "bash" {
		fmt.Fprintln(stderr, "usage: locker completion bash")
		return 1
	}
	fmt.Fprint(stdout, bashCompletionScript)
	return 0
}

const bashCompletionScript = `_locker_completions() {
    local cur cmds
    cur="${COMP_WORDS[COMP_CWORD]}"
    cmds="start version validate-config completion"
    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "$cmds" -- "$cur") )
    fi
}
complete -F _locker_completions locker
`
