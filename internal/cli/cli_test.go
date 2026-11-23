package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMockOfflineEndToEnd(t *testing.T) {
	outDir := t.TempDir()
	opts := Options{
		Topic:  "retrieval-augmented generation",
		OutDir: outDir,
		Mock:   true,
		Stdout: io.Discard,
	}
	err := Run(context.Background(), opts, io.Discard)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// artifacts written?
	episodeDir := filepath.Join(outDir, "retrieval-augmented-generation")
	for _, f := range []string{"script.md", "meta.json", "research.md", "critique.json"} {
		if _, err := os.Stat(filepath.Join(episodeDir, f)); err != nil {
			t.Errorf("missing artifact %s", f)
		}
	}
	// iterations artifact?
	if _, err := os.Stat(filepath.Join(episodeDir, "iterations")); err != nil {
		t.Errorf("missing iterations dir")
	}
}

func TestRunFailsWithoutTopic(t *testing.T) {
	err := Run(context.Background(), Options{Stdout: io.Discard, Mock: true}, os.Stdout)
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
	if !strings.Contains(err.Error(), "--topic") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunFailsWithoutConfig(t *testing.T) {
	err := Run(context.Background(), Options{Topic: "x", OutDir: t.TempDir(), Stdout: io.Discard}, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("error = %v", err)
	}
}
