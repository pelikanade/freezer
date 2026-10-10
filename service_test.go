package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type statusRequest struct {
	State, Description, Context string
	Path                        string
}
type serviceFixture struct {
	fixture
	url, configPath, binary string
	cmd                     *exec.Cmd
	output                  bytes.Buffer
	statuses                chan statusRequest
	client                  *http.Client
}

var binaryOnce sync.Once
var serviceBinary string
var binaryError error

func newService(t *testing.T, entries ...string) *serviceFixture {
	t.Helper()
	f := newFixture(t)
	if len(entries) > 0 {
		f.setEntries(t, entries...)
	}
	s := &serviceFixture{fixture: f, statuses: make(chan statusRequest, 100), client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("invalid GitHub status request")
			http.Error(w, "invalid request", 400)
			return
		}
		var status statusRequest
		if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
			t.Error(err)
			http.Error(w, "invalid JSON", 400)
			return
		}
		status.Path = r.URL.Path
		s.statuses <- status
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(api.Close)
	binaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "freezer-service-binary-")
		if err != nil {
			binaryError = err
			return
		}
		serviceBinary = filepath.Join(dir, "freezer")
		output, err := exec.Command("go", "build", "-o", serviceBinary, ".").CombinedOutput()
		if err != nil {
			binaryError = fmt.Errorf("build binary: %w\n%s", err, output)
		}
	})
	if binaryError != nil {
		t.Fatal(binaryError)
	}
	s.binary = serviceBinary
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	s.url = "http://" + address
	s.configPath = filepath.Join(f.dir, "server.yaml")
	config := fmt.Sprintf("listen: %s\nstate_dir: %s\ngithub_api_base: %s\ngithub_token: fixture-token\nglobal_build_cap: 2\njob_timeout: 1m\ncache:\n  bucket: %s\n  endpoint: %s\n  signing_key: %s\nrepositories:\n  - name: demo\n    url: %s\n    github: owner/demo\n    webhook_secret: fixture-secret\n", address, filepath.Join(f.dir, "state"), api.URL, f.bucket, f.endpoint, f.key, f.repo())
	if err := os.WriteFile(s.configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	t.Cleanup(func() { s.stop(t) })
	return s
}

func (f fixture) setEntries(t *testing.T, entries ...string) string {
	t.Helper()
	var list strings.Builder
	list.WriteString("entries:\n")
	for _, entry := range entries {
		fmt.Fprintf(&list, "  - %q\n", strings.ReplaceAll(entry, "@system@", f.system))
	}
	if err := os.WriteFile(filepath.Join(f.repo(), ".freezer.yaml"), []byte(list.String()), 0600); err != nil {
		t.Fatal(err)
	}
	command(t, f.repo(), "git", "add", ".freezer.yaml")
	command(t, f.repo(), "git", "-c", "user.name=Freezer test", "-c", "user.email=test@localhost", "commit", "--allow-empty", "-m", "Build list")
	return command(t, f.repo(), "git", "rev-parse", "HEAD")
}

func (s *serviceFixture) start(t *testing.T) {
	t.Helper()
	s.cmd = exec.Command(s.binary, s.configPath)
	s.cmd.Stdout = &s.output
	s.cmd.Stderr = &s.output
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := s.client.Get(s.url + "/")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.stop(t)
	t.Fatalf("service did not start: %s", s.output.String())
}

func (s *serviceFixture) stop(t *testing.T) {
	t.Helper()
	if s.cmd == nil {
		return
	}
	_ = s.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
	s.cmd = nil
}

func (s *serviceFixture) get(t *testing.T, path string) (int, string) {
	t.Helper()
	response, err := s.client.Get(s.url + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(data)
}

func (s *serviceFixture) push(t *testing.T, ref, secret string) int {
	t.Helper()
	body := fmt.Sprintf(`{"ref":%q,"after":%q}`, ref, command(t, s.repo(), "git", "rev-parse", "HEAD"))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	request, err := http.NewRequest("POST", s.url+"/hooks/demo", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-GitHub-Event", "push")
	request.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response, err := s.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func (s *serviceFixture) nextStatus(t *testing.T, want string) statusRequest {
	t.Helper()
	select {
	case status := <-s.statuses:
		if status.State != want || status.Context != "freezer" {
			t.Fatalf("want %s status, got %+v", want, status)
		}
		return status
	case <-time.After(120 * time.Second):
		_, page := s.get(t, "/")
		t.Fatalf("timed out waiting for %s status\n%s", want, page)
		return statusRequest{}
	}
}

func TestSignedPushPublishesOverHTTP(t *testing.T) {
	s := newService(t)
	if code := s.push(t, "refs/heads/trunk", "fixture-secret"); code != http.StatusAccepted {
		t.Fatalf("push returned %d", code)
	}
	pending := s.nextStatus(t, "pending")
	final := s.nextStatus(t, "success")
	sha := command(t, s.repo(), "git", "rev-parse", "HEAD")
	if pending.Path != "/repos/owner/demo/statuses/"+sha || final.Path != pending.Path {
		t.Fatalf("wrong resolved commit statuses: %+v %+v", pending, final)
	}
	nix(t, "path-info", "--store", s.store, s.out(t, "hello"))
	nix(t, "store", "verify", "--store", s.store, "--sigs-needed", "1", "--option", "trusted-public-keys", s.publicKey, s.out(t, "hello"))
	code, page := s.get(t, "/jobs/1")
	if code != 200 || !strings.Contains(page, sha) || !strings.Contains(page, "succeeded") {
		t.Fatalf("job page %d: %s", code, page)
	}
	code, transcript := s.get(t, "/jobs/1/log")
	if code != 200 || !strings.Contains(transcript, "$ nix copy") {
		t.Fatalf("transcript %d: %s", code, transcript)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if serviceBinary != "" {
		_ = os.RemoveAll(filepath.Dir(serviceBinary))
	}
	os.Exit(code)
}

func (s *serviceFixture) waitPage(t *testing.T, path, contains string) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var page string
	for time.Now().Before(deadline) {
		code, body := s.get(t, path)
		page = body
		if code == 200 && strings.Contains(body, contains) {
			return body
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never contained %q: %s", path, contains, page)
	return ""
}

func (s *serviceFixture) idle(t *testing.T) { t.Helper(); s.waitPage(t, "/", `data-busy="false"`) }

func (s *serviceFixture) noStatus(t *testing.T) {
	t.Helper()
	s.idle(t)
	select {
	case status := <-s.statuses:
		t.Fatalf("unexpected status: %+v", status)
	default:
	}
}

func (s *serviceFixture) post(t *testing.T, path, location string) {
	t.Helper()
	response, err := s.client.Post(s.url+path, "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 303 || response.Header.Get("Location") != location {
		t.Fatalf("POST %s: status %d, location %q", path, response.StatusCode, response.Header.Get("Location"))
	}
}

func (s *serviceFixture) build(t *testing.T) {
	t.Helper()
	if code := s.push(t, "refs/heads/trunk", "fixture-secret"); code != 202 {
		t.Fatalf("push: %d", code)
	}
	s.nextStatus(t, "pending")
	s.nextStatus(t, "success")
	s.idle(t)
}

func TestSameCommitDoesNotBuildTwice(t *testing.T) {
	s := newService(t)
	s.build(t)
	if code := s.push(t, "refs/heads/trunk", "fixture-secret"); code != 202 {
		t.Fatalf("push: %d", code)
	}
	s.noStatus(t)
	if code, _ := s.get(t, "/jobs/2"); code != 404 {
		t.Fatalf("redelivery created a second job: %d", code)
	}
}

func TestRapidQueuedPushesCoalesce(t *testing.T) {
	s := newService(t, "packages.@system@.slow")
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	superseded := s.setEntries(t, s.entry("hello"))
	s.push(t, "refs/heads/trunk", "fixture-secret")
	newest := s.setEntries(t, s.entry("second"))
	s.push(t, "refs/heads/trunk", "fixture-secret")
	page := s.waitPage(t, "/", "queued")
	if !strings.Contains(page, newest[:8]) || strings.Contains(page, superseded[:8]) {
		t.Fatalf("queued event did not replace the older push: %s", page)
	}
	s.post(t, "/jobs/1/stop", "/jobs/1?notice=stop")
	s.nextStatus(t, "error")
	pending := s.nextStatus(t, "pending")
	final := s.nextStatus(t, "success")
	s.idle(t)
	if !strings.HasSuffix(pending.Path, newest) || final.Path != pending.Path {
		t.Fatalf("coalesced status: %+v %+v", pending, final)
	}
	if code, _ := s.get(t, "/jobs/3"); code != 404 {
		t.Fatalf("superseded event created a job: %d", code)
	}
	s.noStatus(t)
}

func TestNonDefaultBranchIsIgnored(t *testing.T) {
	s := newService(t)
	if code := s.push(t, "refs/heads/feature", "fixture-secret"); code != 204 {
		t.Fatalf("non-default push: %d", code)
	}
	s.noStatus(t)
	if code, _ := s.get(t, "/jobs/1"); code != 404 {
		t.Fatalf("ignored branch created a job: %d", code)
	}
}

func TestBadSignatureCreatesNoJob(t *testing.T) {
	s := newService(t)
	if code := s.push(t, "refs/heads/trunk", "wrong-secret"); code != 401 {
		t.Fatalf("bad signature: %d", code)
	}
	s.noStatus(t)
	if code, _ := s.get(t, "/jobs/1"); code != 404 {
		t.Fatalf("bad signature created a job: %d", code)
	}
}

func TestEventDuringBuildWaitsForRepository(t *testing.T) {
	s := newService(t, "packages.@system@.slow")
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	next := s.setEntries(t, s.entry("hello"))
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.waitPage(t, "/", `class="queued-row"`)
	if code, _ := s.get(t, "/jobs/2"); code != 404 {
		t.Fatalf("same repository ran concurrently: %d", code)
	}
	select {
	case status := <-s.statuses:
		t.Fatalf("waiting event posted prematurely: %+v", status)
	default:
	}
	s.post(t, "/jobs/1/stop", "/jobs/1?notice=stop")
	s.nextStatus(t, "error")
	pending := s.nextStatus(t, "pending")
	s.nextStatus(t, "success")
	if !strings.HasSuffix(pending.Path, next) {
		t.Fatalf("queued head: %+v", pending)
	}
}

func TestFailedEntryCountReachesGitHub(t *testing.T) {
	s := newService(t, "packages.@system@.hello", "packages.@system@.notDerivation")
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.nextStatus(t, "pending")
	status := s.nextStatus(t, "failure")
	if status.Description != "1 of 2 entries failed" {
		t.Fatalf("failure count: %+v", status)
	}
	page := s.waitPage(t, "/jobs/2", "The job did not succeed")
	if !strings.Contains(page, "derivation") {
		t.Fatalf("missing copy error: %s", page)
	}
}

func TestAllCacheHitsStillReportSuccess(t *testing.T) {
	s := newService(t)
	s.build(t)
	s.setEntries(t, s.entry("hello"))
	s.build(t)
	_, page := s.get(t, "/jobs/2")
	if !strings.Contains(page, "Already in the cache") {
		t.Fatalf("expected cache-hit event: %s", page)
	}
	_, transcript := s.get(t, "/jobs/2/log")
	if strings.Contains(transcript, "$ nix copy") {
		t.Fatalf("cache-hit event published: %s", transcript)
	}
}

func TestBuildNowBypassesLastBuiltAndProbe(t *testing.T) {
	s := newService(t)
	s.build(t)
	s.post(t, "/build/demo", "/?notice=build&repo=demo")
	s.nextStatus(t, "pending")
	s.nextStatus(t, "success")
	_, transcript := s.get(t, "/jobs/2/log")
	if !strings.Contains(transcript, "$ nix copy") || strings.Contains(transcript, "$ nix path-info") {
		t.Fatalf("Build now did not bypass probe: %s", transcript)
	}
}

func TestStopBoundsRunningEvent(t *testing.T) {
	s := newService(t, "packages.@system@.slow")
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	started := time.Now()
	s.post(t, "/jobs/1/stop", "/jobs/1?notice=stop")
	status := s.nextStatus(t, "error")
	if time.Since(started) > 5*time.Second || status.Description != "stopped by the operator" {
		t.Fatalf("Stop did not abort promptly: %+v", status)
	}
	s.waitPage(t, "/jobs/1?notice=stop", "You stopped this build.")
}

func TestRestartPreservesLastBuilt(t *testing.T) {
	s := newService(t)
	s.build(t)
	s.stop(t)
	s.start(t)
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.noStatus(t)
	if code, _ := s.get(t, "/jobs/2"); code != 404 {
		t.Fatalf("restart rebuilt last-built commit: %d", code)
	}
}

func TestRestartInterruptsRunningJob(t *testing.T) {
	s := newService(t, "packages.@system@.slow")
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	s.stop(t)
	s.start(t)
	_, page := s.get(t, "/jobs/1")
	if !strings.Contains(page, "freezer restarted mid-build") || !strings.Contains(page, "The commit was not recorded") {
		t.Fatalf("interrupted job: %s", page)
	}
	_, transcript := s.get(t, "/jobs/1/log")
	if !strings.Contains(transcript, "$ nix copy") {
		t.Fatalf("lost interrupted transcript: %s", transcript)
	}
	s.setEntries(t, s.entry("hello"))
	s.build(t)
}

func TestMissingBuildListFailsWithoutJobs(t *testing.T) {
	s := newService(t)
	command(t, s.repo(), "git", "rm", ".freezer.yaml")
	command(t, s.repo(), "git", "-c", "user.name=Freezer test", "-c", "user.email=test@localhost", "commit", "-m", "Remove build list")
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.nextStatus(t, "pending")
	s.nextStatus(t, "failure")
	if code, _ := s.get(t, "/jobs/1"); code != 404 {
		t.Fatalf("missing build list created a job: %d", code)
	}
}

func TestFullTranscriptSurvivesHTMLTruncation(t *testing.T) {
	s := newService(t, "packages.@system@.verbose")
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.nextStatus(t, "pending")
	s.nextStatus(t, "failure")
	code, transcript := s.get(t, "/jobs/1/log")
	if code != 200 || !strings.Contains(transcript, "transcript-line-1\n") || !strings.Contains(transcript, "transcript-line-2105") {
		t.Fatalf("build output was lost: %d %s", code, transcript)
	}
	_, page := s.get(t, "/jobs/1")
	if !strings.Contains(page, "Showing the last 2000 of") || strings.Count(page, `data-line="`) != 2000 || !strings.Contains(page, "transcript-line-2105") || strings.Contains(page, "transcript-line-1</span>") {
		t.Fatalf("HTML truncation did not preserve the final 2000 lines: %s", page)
	}
	s.stop(t)
	s.start(t)
	_, after := s.get(t, "/jobs/1/log")
	if after != transcript {
		t.Fatal("restart changed the full transcript")
	}
}

func TestJobPagesPreserveContiguousGroups(t *testing.T) {
	entries := make([]string, 51)
	for i := range entries {
		entries[i] = "selected.@system@.absent*"
	}
	s := newService(t, entries...)
	s.build(t)
	_, page := s.get(t, "/")
	if !strings.Contains(page, `rowspan="50"`) || !strings.Contains(page, `href="/?before=2"`) {
		t.Fatalf("first job page lost grouping or cursor: %s", page)
	}
	_, older := s.get(t, "/?before=2")
	if !strings.Contains(older, `href="/jobs/1"`) || strings.Contains(older, `href="/jobs/2"`) || !strings.Contains(older, `href="/?after=1"`) {
		t.Fatalf("older page contains wrong jobs: %s", older)
	}
	_, newer := s.get(t, "/?after=1")
	if !strings.Contains(newer, `rowspan="50"`) || !strings.Contains(newer, `href="/jobs/51"`) {
		t.Fatalf("Newer did not recover the first page: %s", newer)
	}
}

func TestGlobalCapAndStopPreventLaterJobs(t *testing.T) {
	s := newService(t, "packages.@system@.slow", "packages.@system@.hello")
	s.stop(t)
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("global_build_cap: 2"), []byte("global_build_cap: 1"))
	if err := os.WriteFile(s.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	s.push(t, "refs/heads/trunk", "fixture-secret")
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	if code, _ := s.get(t, "/jobs/2"); code != 404 {
		t.Fatalf("global cap did not hold: %d", code)
	}
	s.post(t, "/jobs/1/stop", "/jobs/1?notice=stop")
	s.nextStatus(t, "error")
	s.idle(t)
	if code, _ := s.get(t, "/jobs/2"); code != 404 {
		t.Fatalf("Stop allowed a later job: %d", code)
	}
	s.setEntries(t, s.entry("hello"))
	s.build(t)
	if code, _ := s.get(t, "/jobs/2"); code != 200 {
		t.Fatalf("Stop leaked the global build slot: %d", code)
	}
}
