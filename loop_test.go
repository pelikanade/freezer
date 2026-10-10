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
	results := RunLoop(context.Background(), f.config(), f.repo(), []string{f.entry("hello")}, false)
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
