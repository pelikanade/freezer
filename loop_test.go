package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var nixTestOptions = []string{"--extra-experimental-features", "nix-command flakes", "--option", "narinfo-cache-positive-ttl", "0", "--option", "narinfo-cache-negative-ttl", "0"}

func RunLoop(ctx context.Context, cfg Config, flake string, entries []string, force bool) []Result {
	results := expandEntries(ctx, cfg, flake, entries)
	for i, planned := range results {
		if planned.Status == "" {
			results[i] = RunJob(ctx, cfg, flake, planned.Entry, force, nil)
			results[i].Transcript = planned.Transcript + results[i].Transcript
		}
	}
	return results
}

type fixture struct {
	dir, system, key, publicKey, store, endpoint, bucket string
}

func command(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %q: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func nix(t *testing.T, args ...string) string {
	t.Helper()
	return command(t, "", "nix", append(args, nixTestOptions...)...)
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	endpoint, bucket, busybox := os.Getenv("FREEZER_TEST_ENDPOINT"), os.Getenv("FREEZER_TEST_BUCKET"), os.Getenv("FREEZER_TEST_BUSYBOX")
	if endpoint == "" || bucket == "" || busybox == "" {
		t.Fatal("run the acceptance suite with devenv test")
	}
	f := fixture{dir: t.TempDir(), endpoint: endpoint, bucket: bucket, system: map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH] + "-" + runtime.GOOS}
	command(t, "", "mc", "rm", "--recursive", "--force", "local/"+bucket)
	repo := filepath.Join(f.dir, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	replace := strings.NewReplacer("@system@", f.system, "@busyboxRoot@", filepath.Dir(filepath.Dir(busybox)), "@nonce@", fmt.Sprint(time.Now().UnixNano()))
	for _, name := range []string{"flake.nix", ".freezer.yaml"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(replace.Replace(string(data))), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command(t, repo, "git", "init", "-b", "trunk")
	command(t, repo, "git", "add", ".")
	nix(t, "flake", "lock", repo)
	command(t, repo, "git", "add", "flake.lock")
	command(t, repo, "git", "-c", "user.name=Freezer test", "-c", "user.email=test@localhost", "commit", "-m", "Fixture")
	f.key = filepath.Join(f.dir, "cache.key")
	public := filepath.Join(f.dir, "cache.pub")
	command(t, "", "nix-store", "--generate-binary-cache-key", "freezer-test", f.key, public)
	if err := os.Chmod(f.key, 0600); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(public)
	if err != nil {
		t.Fatal(err)
	}
	f.publicKey = strings.TrimSpace(string(key))
	query := url.Values{"endpoint": {endpoint}, "region": {"auto"}, "addressing-style": {"path"}, "compression": {"zstd"}, "secret-key": {f.key}, "multipart-upload": {"true"}}
	f.store = "s3://" + bucket + "?" + query.Encode()
	return f
}

func (f fixture) repo() string             { return filepath.Join(f.dir, "repo") }
func (f fixture) entry(name string) string { return "packages." + f.system + "." + name }
func (f fixture) config() Config {
	return Config{Cache: CacheConfig{Bucket: f.bucket, Endpoint: f.endpoint, SigningKey: f.key}, GlobCap: 200, JobTimeout: time.Minute}
}
func (f fixture) out(t *testing.T, name string) string {
	return nix(t, "eval", "--raw", f.repo()+"#"+f.entry(name)+".outPath")
}

func TestExactEntryPublishes(t *testing.T) {
	f := newFixture(t)
	data := fmt.Sprintf("cache:\n  bucket: %s\n  endpoint: %s\n  signing_key: %s\nglob_cap: 200\njob_timeout: 1m\n", f.bucket, f.endpoint, f.key)
	configPath := filepath.Join(f.dir, "server.yaml")
	if err := os.WriteFile(configPath, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := LoadBuildList(f.repo())
	if err != nil {
		t.Fatal(err)
	}
	results := RunLoop(context.Background(), cfg, f.repo(), entries, false)
	if len(results) != 1 || results[0].Status != "succeeded" || results[0].CacheHit {
		t.Fatalf("expected published entry, got %+v", results)
	}
	out := f.out(t, "hello")
	if got := nix(t, "path-info", "--store", f.store, out); got != out {
		t.Fatalf("probe returned %q, want %q", got, out)
	}
}

func TestPublishedPathSubstitutesWithSignature(t *testing.T) {
	f := newFixture(t)
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("hello")}, false)
	if results[0].Status != "succeeded" {
		t.Fatalf("publish: %+v", results[0])
	}
	out := f.out(t, "hello")
	root := filepath.Join(f.dir, "scratch")
	t.Cleanup(func() {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return os.Chmod(path, 0700)
			}
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	})
	store := "local?root=" + root
	nix(t, "copy", "--from", f.store, "--to", store, "--option", "trusted-public-keys", f.publicKey, out)
	nix(t, "store", "verify", "--store", store, "--sigs-needed", "1", "--option", "trusted-public-keys", f.publicKey, out)
	data, err := os.ReadFile(filepath.Join(root, out, "message"))
	if err != nil || string(data) != "freezer fixture: freezer-hello\n" {
		t.Fatalf("substituted content %q: %v", data, err)
	}
}

func TestSecondRunIsCacheHit(t *testing.T) {
	f := newFixture(t)
	first := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("hello")}, false)
	if first[0].Status != "succeeded" {
		t.Fatalf("first publish: %+v", first)
	}
	second := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("hello")}, false)
	if second[0].Status != "succeeded" || !second[0].CacheHit || second[0].Published != 0 || strings.Contains(second[0].Transcript, "$ nix copy") {
		t.Fatalf("expected cache hit without publication: %+v", second)
	}
}

func TestGlobPublishesEveryMatch(t *testing.T) {
	f := newFixture(t)
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{"selected." + f.system + ".*"}, false)
	if len(results) != 2 {
		t.Fatalf("expected two matched jobs: %+v", results)
	}
	for _, result := range results {
		if result.Status != "succeeded" || result.CacheHit {
			t.Fatalf("glob job: %+v", result)
		}
	}
	for _, name := range []string{"hello", "second"} {
		nix(t, "path-info", "--store", f.store, f.out(t, name))
	}
}

func TestGlobOverCapBuildsNothing(t *testing.T) {
	f := newFixture(t)
	cfg := f.config()
	cfg.GlobCap = 1
	results := RunLoop(context.Background(), cfg, f.repo(), []string{"selected." + f.system + ".*"}, false)
	if len(results) != 1 || results[0].Status != "failed" || !strings.Contains(results[0].Error, "cap") {
		t.Fatalf("expected whole pattern failure: %+v", results)
	}
	for _, name := range []string{"hello", "second"} {
		nixFails(t, "path-info", "--store", f.store, f.out(t, name))
	}
}

func nixFails(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nix", append(args, nixTestOptions...)...).CombinedOutput()
	if err == nil {
		t.Fatalf("nix %q unexpectedly succeeded: %s", args, out)
	}
	if ctx.Err() != nil {
		t.Fatalf("nix %q timed out: %s", args, out)
	}
	return string(out)
}

func TestMissingPrefixDoesNotBlockOtherEntries(t *testing.T) {
	f := newFixture(t)
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{"missing." + f.system + ".*", f.entry("hello")}, false)
	if len(results) != 2 || results[0].Status != "failed" || results[1].Status != "succeeded" {
		t.Fatalf("expected isolated pattern failure: %+v", results)
	}
	nix(t, "path-info", "--store", f.store, f.out(t, "hello"))
}

func TestNonDerivationReportsCopyError(t *testing.T) {
	f := newFixture(t)
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("notDerivation")}, false)
	if len(results) != 1 || results[0].Status != "failed" || !strings.Contains(results[0].Transcript, "$ nix copy") || !strings.Contains(results[0].Error, "derivation") {
		t.Fatalf("expected copy's derivation error: %+v", results)
	}
}

func TestIFDBuildsOnlyAtCopy(t *testing.T) {
	f := newFixture(t)
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("ifd")}, false)
	result := results[0]
	if result.Status != "succeeded" || result.CacheHit || !strings.Contains(result.Transcript, "allow-import-from-derivation' is disabled") {
		t.Fatalf("expected failed IFD evaluation followed by successful copy: %+v", result)
	}
	nix(t, "path-info", "--store", f.store, f.out(t, "ifd"))
}

func TestMultiOutputPublishesDefaultOnly(t *testing.T) {
	f := newFixture(t)
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("multi")}, false)
	if results[0].Status != "succeeded" {
		t.Fatalf("publish: %+v", results)
	}
	nix(t, "path-info", "--store", f.store, f.out(t, "multi"))
	nixFails(t, "path-info", "--store", f.store, f.out(t, "multi.dev"))
}

func TestRepairRequiresDeletingNarinfo(t *testing.T) {
	f := newFixture(t)
	publish := func(force bool) Result {
		r := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("hello")}, force)[0]
		if r.Status != "succeeded" {
			t.Fatalf("publish: %+v", r)
		}
		return r
	}
	publish(false)
	out := f.out(t, "hello")
	narinfo := strings.SplitN(filepath.Base(out), "-", 2)[0] + ".narinfo"
	command(t, "", "mc", "rm", "local/"+f.bucket+"/"+narinfo)
	if r := publish(false); r.CacheHit || r.Published != 1 {
		t.Fatalf("missing narinfo must republish: %+v", r)
	}
	nix(t, "store", "verify", "--store", f.store, "--no-trust", out)
	command(t, "", "mc", "rm", "--recursive", "--force", "local/"+f.bucket+"/nar/")
	if r := publish(false); !r.CacheHit {
		t.Fatalf("narinfo remains a cache hit: %+v", r)
	}
	if r := publish(true); r.CacheHit || r.Published != 0 {
		t.Fatalf("copy cannot repair only a deleted nar: %+v", r)
	}
	nixFails(t, "store", "verify", "--store", f.store, "--no-trust", out)
	command(t, "", "mc", "rm", "local/"+f.bucket+"/"+narinfo)
	publish(true)
	nix(t, "store", "verify", "--store", f.store, "--no-trust", out)
}

func TestMatchedNothingAndTimeout(t *testing.T) {
	f := newFixture(t)
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{"selected." + f.system + ".absent*"}, false)
	if len(results) != 1 || results[0].Status != "succeeded" || !results[0].MatchedNothing || results[0].CacheHit || results[0].Published != 0 {
		t.Fatalf("expected matched nothing: %+v", results)
	}
	cfg := f.config()
	cfg.JobTimeout = time.Second
	started := time.Now()
	results = RunLoop(context.Background(), cfg, f.repo(), []string{f.entry("slow")}, false)
	if results[0].Status != "failed" || !strings.Contains(results[0].Error, "deadline exceeded") || time.Since(started) > 5*time.Second {
		t.Fatalf("timeout did not bound the job: %+v", results)
	}
}

func TestOptionLikeOutputCannotBecomeCacheHit(t *testing.T) {
	f := newFixture(t)
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("optionPath")}, false)
	if len(results) != 1 || results[0].CacheHit || results[0].Status != "failed" || !strings.Contains(results[0].Transcript, "$ nix copy") {
		t.Fatalf("option-like output bypassed publication: %+v", results)
	}
}

func TestWhitespaceOutputCannotMatchAnotherStorePath(t *testing.T) {
	f := newFixture(t)
	published := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("hello")}, false)
	if published[0].Status != "succeeded" {
		t.Fatalf("publish: %+v", published)
	}
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("whitespacePath")}, false)
	if results[0].CacheHit || !strings.Contains(results[0].Transcript, "$ nix copy") {
		t.Fatalf("probe normalized an invalid output into a different published path: %+v", results)
	}
}
