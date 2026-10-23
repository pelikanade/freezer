package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigWithoutCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	data := "state_dir: state\npoll_interval: 15m\nrepositories:\n  - name: smoke\n    url: https://example.invalid/smoke.git\n    github: smoke/smoke\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cache != nil {
		t.Fatalf("expected no cache, got %+v", cfg.Cache)
	}
}

func TestRunJobWithoutCacheBuilds(t *testing.T) {
	system := map[string]string{"amd64": "x86_64-linux", "arm64": "aarch64-linux"}[runtime.GOARCH]
	dir := t.TempDir()
	flake := `{ outputs = { self }: { packages.` + system + `.hello = derivation { name = "hello"; system = "` + system + `"; builder = "/bin/sh"; args = [ "-c" "echo hi > $out" ]; }; }; }`
	if err := os.WriteFile(filepath.Join(dir, "flake.nix"), []byte(flake), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{GlobCap: 200, JobTimeout: time.Minute}
	result := RunJob(context.Background(), cfg, dir, "packages."+system+".hello", false, nil)
	if result.Status != "succeeded" {
		t.Fatalf("job: %+v", result)
	}
	if result.Published != 0 || result.CacheHit {
		t.Fatalf("unexpected cache accounting: %+v", result)
	}
	if !strings.Contains(result.Transcript, "nix build") {
		t.Fatalf("transcript: %s", result.Transcript)
	}
}
