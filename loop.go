package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Result struct {
	Entry          string
	Status         string
	CacheHit       bool
	MatchedNothing bool
	Published      int
	Error          string
	Transcript     string
}

type commandWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *commandWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

func runCommand(ctx context.Context, transcript io.Writer, name string, args ...string) (string, error) {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = arg
		if strings.ContainsAny(arg, " \t\n\"'\\$`&;()<>|*?[]{}#") {
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		}
	}
	if _, err := fmt.Fprintf(transcript, "\n$ %s %s\n", name, strings.Join(quoted, " ")); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	var stdout bytes.Buffer
	stream := &commandWriter{writer: transcript}
	cmd.Stdout = io.MultiWriter(stream, &stdout)
	cmd.Stderr = stream
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return strings.TrimSpace(stdout.String()), err
}

func runNix(ctx context.Context, transcript io.Writer, args ...string) (string, error) {
	args = append(args, "--extra-experimental-features", "nix-command flakes", "--option", "narinfo-cache-positive-ttl", "0", "--option", "narinfo-cache-negative-ttl", "0", "--no-write-lock-file")
	return runCommand(ctx, transcript, "nix", args...)
}

func RunLoop(ctx context.Context, cfg Config, flake string, entries []string, force bool) []Result {
	results := make([]Result, 0, len(entries))
	for _, entry := range entries {
		results = append(results, RunJob(ctx, cfg, flake, entry, force))
	}
	return results
}

func RunJob(ctx context.Context, cfg Config, flake, entry string, force bool) Result {
	ctx, cancel := context.WithTimeout(ctx, cfg.JobTimeout)
	defer cancel()
	result := Result{Entry: entry, Status: "succeeded"}
	var transcript bytes.Buffer
	out, err := runNix(ctx, &transcript, "eval", "--raw", flake+"#"+entry+".outPath", "--option", "allow-import-from-derivation", "false")
	if err == nil && !force {
		if _, err = runNix(ctx, &transcript, "path-info", "--store", cfg.Cache.StoreURL(), out); err == nil {
			result.CacheHit = true
			result.Transcript = transcript.String()
			return result
		}
	}
	_, err = runNix(ctx, &transcript, "copy", "--to", cfg.Cache.StoreURL(), flake+"#"+entry)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("nix copy: %v", err)
	} else {
		result.Published = 1
	}
	result.Transcript = transcript.String()
	return result
}
