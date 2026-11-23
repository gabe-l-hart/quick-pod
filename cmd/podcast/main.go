// Command podcast produces a podcast episode from a topic.
//
// Usage:
//
//	podcast --topic "..." [--docs <paths...>] [--config podcasts.yaml] [--out out/] [--dry-run] [--mock]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/gabe-l-hart/podcasts/internal/cli"
)

type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprint([]string(*s)) }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	var (
		configPath  string
		topic      string
		outDir     string
		dryRun     bool
		mock       bool
		forceRender bool
		verbose    bool
		version    bool
	)
	var docs stringSlice
	flag.StringVar(&configPath, "config", "", "path to the config file (default: podcasts.yaml or ~/.config/podcasts/podcasts.yaml)")
	flag.StringVar(&topic, "topic", "", "topic description (required)")
	flag.Var(&docs, "docs", "source document or link (repeatable): local files, URLs, arXiv IDs")
	flag.StringVar(&outDir, "out", "", "output directory (default from config)")
	flag.BoolVar(&dryRun, "dry-run", false, "stop before audio rendering (no TTS spend)")
	flag.BoolVar(&mock, "mock", false, "offline dev mode: deterministic mock LLM, no network")
	flag.BoolVar(&forceRender, "force-render", false, "render audio even when the verdict is FAIL")
	flag.BoolVar(&verbose, "verbose", false, "log per-iteration details")
	flag.BoolVar(&version, "version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "podcast — research-driven podcast script → audio\n\n")
		fmt.Fprintf(os.Stderr, "Usage: podcast --topic \"<topic>\" [flags]\n\nFlags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  podcast --topic \"retrieval-augmented generation\" --docs ../papers/attention.pdf\n")
		fmt.Fprintf(os.Stderr, "  podcast --topic \"...\" --docs 1706.03762 --dry-run\n")
		fmt.Fprintf(os.Stderr, "  podcast --topic \"...\" --mock  # offline dev mode\n")
	}
	flag.Parse()

	opts := cli.Options{
		ConfigPath:  configPath,
		Topic:       topic,
		Docs:        docs,
		OutDir:      outDir,
		DryRun:      dryRun,
		Mock:        mock,
		ForceRender: forceRender,
		Verbose:     verbose,
		Version:     version,
		Stdout:      os.Stdout,
	}

	ctx := context.Background()
	if err := cli.Run(ctx, opts, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}
}
