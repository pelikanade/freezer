package main

import (
	"bytes"
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

// fixtureRepository is the repository list the service tests start with: one
// registered repository, reached by polling like any other (ADR 0024).
const fixtureRepository = "\nrepositories:\n  - name: demo\n    url: %s\n    github: owner/demo\n"

// setEntries rewrites the fixture repository's build list and commits it, which
// is what gives the next tick a head the service has not built yet.
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

func newService(t *testing.T, entries ...string) *serviceFixture {
	t.Helper()
	return newServiceTicking(t, "1h", entries...)
}

// newServiceTicking builds the fixture configuration with the given poll
// interval. The poll at startup is always the first trigger, so the tests start
// the service when they want an event to happen; a short interval adds the ticks
// that the tests needing a second automatic event wait for.
func newServiceTicking(t *testing.T, interval string, entries ...string) *serviceFixture {
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
	config := fmt.Sprintf("listen: %s\nstate_dir: %s\ngithub_api_base: %s\ngithub_token: fixture-token\nglobal_build_cap: 2\njob_timeout: 1m\npoll_interval: %s\ncache:\n  bucket: %s\n  endpoint: %s\n  signing_key: %s"+fixtureRepository, address, filepath.Join(f.dir, "state"), api.URL, interval, f.bucket, f.endpoint, f.key, f.repo())
	if err := os.WriteFile(s.configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.stop(t) })
	return s
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

// kill stops the service without letting it shut down, which is what a crash or
// a SIGKILL looks like: the database keeps whatever the running and planned jobs
// had, and the next start has to account for them.
func (s *serviceFixture) kill(t *testing.T) {
	t.Helper()
	if s.cmd == nil {
		return
	}
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
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

// build waits for the next event the service starts on its own: the poll at
// startup, or the first tick that finds a head other than the last built.
func (s *serviceFixture) build(t *testing.T) {
	t.Helper()
	s.nextStatus(t, "pending")
	s.nextStatus(t, "success")
	s.idle(t)
}

// The poll at startup is the trigger, and the whole publish path hangs off it.
func TestStartupPollPublishes(t *testing.T) {
	s := newService(t)
	s.start(t)
	pending := s.nextStatus(t, "pending")
	final := s.nextStatus(t, "success")
	head := command(t, s.repo(), "git", "rev-parse", "HEAD")
	if pending.Path != "/repos/owner/demo/statuses/"+head || final.Path != pending.Path {
		t.Fatalf("polled statuses name the wrong commit: %+v %+v", pending, final)
	}
	nix(t, "path-info", "--store", s.store, s.out(t, "hello"))
	nix(t, "store", "verify", "--store", s.store, "--sigs-needed", "1", "--option", "trusted-public-keys", s.publicKey, s.out(t, "hello"))
	code, page := s.get(t, "/jobs/1")
	if code != 200 || !strings.Contains(page, head) || !strings.Contains(page, "succeeded") {
		t.Fatalf("job page %d: %s", code, page)
	}
	code, transcript := s.get(t, "/jobs/1/log")
	if code != 200 || !strings.Contains(transcript, "$ nix copy") {
		t.Fatalf("transcript %d: %s", code, transcript)
	}
}

// The last-built check is what keeps polling civil: an unchanged head ends every
// later event without a job and without a commit status.
func TestPollingLeavesAnUnchangedHeadAlone(t *testing.T) {
	s := newServiceTicking(t, "1s")
	s.start(t)
	s.build(t)
	time.Sleep(3 * time.Second)
	select {
	case status := <-s.statuses:
		t.Fatalf("an unchanged head produced another commit status: %+v", status)
	default:
	}
	if code, _ := s.get(t, "/jobs/2"); code != 404 {
		t.Fatalf("an unchanged head produced a second job: %d", code)
	}
}

// A waiting Build now keeps its place. A tick must not replace it, or the repair
// path would silently degrade into a probe run (ADR 0019).
func TestQueuedBuildNowIsNotSuperseded(t *testing.T) {
	s := newServiceTicking(t, "1s", "packages.@system@.slow", "packages.@system@.hello")
	s.start(t)
	s.nextStatus(t, "pending")
	// The fast entry publishes while the slow one keeps the event running, so a
	// later ordinary event for the same head would be a cache hit.
	s.waitPage(t, "/jobs/2", "succeeded")
	s.post(t, "/build/demo", "/?notice=build&repo=demo")
	s.waitPage(t, "/", `class="queued-row"`)
	s.post(t, "/jobs/1/stop", "/jobs/1?notice=stop")
	s.nextStatus(t, "error")
	s.nextStatus(t, "pending")
	page := s.waitPage(t, "/jobs/4", "succeeded")
	if strings.Contains(page, "cached — nothing built") {
		t.Fatalf("the waiting Build now was replaced by a tick:\n%s", page)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if serviceBinary != "" {
		_ = os.RemoveAll(filepath.Dir(serviceBinary))
	}
	os.Exit(code)
}

func TestEventDuringBuildWaitsForRepository(t *testing.T) {
	s := newServiceTicking(t, "1s", "packages.@system@.slow")
	s.start(t)
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	next := s.setEntries(t, s.entry("hello"))
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
	// Several ticks arrived while the event ran and coalesced into the one
	// waiting event, so the queue produced exactly one event's jobs.
	if code, _ := s.get(t, "/jobs/3"); code != 404 {
		t.Fatalf("coalesced ticks created a second event: %d", code)
	}
}

func TestFailedEntryCountReachesGitHub(t *testing.T) {
	s := newService(t, "packages.@system@.hello", "packages.@system@.notDerivation")
	s.start(t)
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
	s := newServiceTicking(t, "1s")
	s.start(t)
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
	s.start(t)
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
	s.start(t)
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
	s.start(t)
	s.build(t)
	s.stop(t)
	s.start(t)
	s.noStatus(t)
	if code, _ := s.get(t, "/jobs/2"); code != 404 {
		t.Fatalf("restart rebuilt last-built commit: %d", code)
	}
}

func TestRestartInterruptsRunningJob(t *testing.T) {
	s := newService(t, "packages.@system@.slow", "packages.@system@.hello")
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.configPath, bytes.ReplaceAll(data, []byte("global_build_cap: 2"), []byte("global_build_cap: 1")), 0600); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	// The cap holds the second entry back, so it is planned when the service dies.
	s.waitPage(t, "/jobs/2", "waiting for a build slot")
	s.kill(t)
	s.start(t)
	_, page := s.get(t, "/jobs/1")
	if !strings.Contains(page, "freezer restarted mid-build") || !strings.Contains(page, "The commit was not recorded") {
		t.Fatalf("interrupted job: %s", page)
	}
	_, transcript := s.get(t, "/jobs/1/log")
	if !strings.Contains(transcript, "$ nix copy") {
		t.Fatalf("lost interrupted transcript: %s", transcript)
	}
	_, planned := s.get(t, "/jobs/2")
	if !strings.Contains(planned, "freezer restarted before this job started") || !strings.Contains(planned, "interrupted") {
		t.Fatalf("a job the restart found unstarted is not reported interrupted: %s", planned)
	}
	// The commit was never recorded, so the poll after the restart starts it
	// again: an interrupted event recovers without an operator.
	s.nextStatus(t, "pending")
}

func TestMissingBuildListFailsWithoutJobs(t *testing.T) {
	s := newService(t)
	s.start(t)
	s.build(t)
	command(t, s.repo(), "git", "rm", ".freezer.yaml")
	command(t, s.repo(), "git", "-c", "user.name=Freezer test", "-c", "user.email=test@localhost", "commit", "-m", "Remove build list")
	s.stop(t)
	s.start(t)
	s.nextStatus(t, "pending")
	s.nextStatus(t, "failure")
	if code, _ := s.get(t, "/jobs/2"); code != 404 {
		t.Fatalf("missing build list created a job: %d", code)
	}
}

func TestFullTranscriptSurvivesHTMLTruncation(t *testing.T) {
	s := newService(t, "packages.@system@.verbose")
	s.start(t)
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
	s.start(t)
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

// The whole event is visible while it runs: the entries the cap holds back are
// listed as planned, each with its own page, from the moment the event starts
// (ADR 0025).
func TestPlannedEntriesAreVisibleBeforeTheyRun(t *testing.T) {
	s := newService(t, "packages.@system@.slow", "packages.@system@.hello", "packages.@system@.second")
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.configPath, bytes.ReplaceAll(data, []byte("global_build_cap: 2"), []byte("global_build_cap: 1")), 0600); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	for _, id := range []int{2, 3} {
		page := s.waitPage(t, fmt.Sprintf("/jobs/%d", id), "waiting for a build slot")
		if !strings.Contains(page, "planned") || !strings.Contains(page, "not yet probed") {
			t.Fatalf("job %d is not shown as planned: %s", id, page)
		}
	}
	_, index := s.get(t, "/")
	for _, id := range []int{1, 2, 3} {
		if !strings.Contains(index, fmt.Sprintf(`href="/jobs/%d"`, id)) {
			t.Fatalf("the index does not list job %d while its event runs:\n%s", id, index)
		}
	}
	s.post(t, "/jobs/1/stop", "/jobs/1?notice=stop")
	s.nextStatus(t, "error")
	s.idle(t)
	if page := s.waitPage(t, "/jobs/3", "The event ended before this job started."); !strings.Contains(page, "stopped") {
		t.Fatalf("a job the event never started did not end as stopped: %s", page)
	}
}

func TestGlobalCapAndStopPreventLaterJobs(t *testing.T) {
	s := newService(t, "packages.@system@.slow", "packages.@system@.hello")
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("global_build_cap: 2"), []byte("global_build_cap: 1"))
	if err := os.WriteFile(s.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	if page := s.waitPage(t, "/jobs/2", "waiting for a build slot"); !strings.Contains(page, "planned") {
		t.Fatalf("the entry the cap holds back is not shown as planned: %s", page)
	}
	s.post(t, "/jobs/1/stop", "/jobs/1?notice=stop")
	s.nextStatus(t, "error")
	s.idle(t)
	if page := s.waitPage(t, "/jobs/2", "The event ended before this job started."); !strings.Contains(page, "stopped") {
		t.Fatalf("a job the event never started did not end as stopped: %s", page)
	}
	// The new event starts with the nonce-named slow fixture, so its probe cannot
	// answer from a stale local narinfo cache and the copy really runs.
	s.setEntries(t, s.entry("slow"), s.entry("hello"))
	s.post(t, "/build/demo", "/?notice=build&repo=demo")
	s.nextStatus(t, "pending")
	// The slot the stopped event released is usable again: the new event's first
	// job runs, and the one the cap holds back is planned rather than missing.
	if page := s.waitPage(t, "/jobs/3/log", "$ nix copy"); !strings.Contains(page, "slow") {
		t.Fatalf("the new event's first job did not run: %s", page)
	}
	if page := s.waitPage(t, "/jobs/4", "waiting for a build slot"); !strings.Contains(page, "planned") {
		t.Fatalf("the new event's second entry is not planned: %s", page)
	}
	s.post(t, "/jobs/3/stop", "/jobs/3?notice=stop")
	s.nextStatus(t, "error")
}

func TestRetentionDeletesJobsAndTranscripts(t *testing.T) {
	s := newServiceTicking(t, "1s")
	s.start(t)
	s.build(t)
	s.setEntries(t, s.entry("second"))
	s.build(t)
	s.setEntries(t, s.entry("hello"))
	s.build(t)
	transcripts := make(map[int]string, 2)
	for _, id := range []int{2, 3} {
		code, body := s.get(t, fmt.Sprintf("/jobs/%d/log", id))
		if code != 200 {
			t.Fatalf("recent transcript %d: HTTP %d", id, code)
		}
		transcripts[id] = body
	}
	s.stop(t)
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("retention: 2\n")...)
	if err := os.WriteFile(s.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	for _, path := range []string{"/jobs/1", "/jobs/1/log"} {
		if code, _ := s.get(t, path); code != 404 {
			t.Fatalf("retention kept %s: %d", path, code)
		}
	}
	for _, id := range []int{2, 3} {
		if code, body := s.get(t, fmt.Sprintf("/jobs/%d/log", id)); code != 200 || body != transcripts[id] {
			t.Fatalf("retention changed recent transcript %d: HTTP %d", id, code)
		}
	}
	s.setEntries(t, s.entry("second"))
	s.build(t)
	if code, _ := s.get(t, "/jobs/2/log"); code != 404 {
		t.Fatalf("retention was not applied after an event: %d", code)
	}
	s.noStatus(t)
}

func TestRepositoriesBuildConcurrently(t *testing.T) {
	s := newService(t, "packages.@system@.slow")
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(fmt.Sprintf("  - name: other\n    url: %s\n    github: owner/other\n", s.repo()))...)
	if err := os.WriteFile(s.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/1/log", "$ nix copy")
	s.nextStatus(t, "pending")
	s.waitPage(t, "/jobs/2/log", "$ nix copy")
	for _, id := range []int{1, 2} {
		_, page := s.get(t, fmt.Sprintf("/jobs/%d", id))
		if !strings.Contains(page, `class="running-box"`) {
			t.Fatalf("repository did not run concurrently: %s", page)
		}
	}
	s.post(t, "/jobs/1/stop", "/jobs/1?notice=stop")
	s.nextStatus(t, "error")
	_, other := s.get(t, "/jobs/2")
	if !strings.Contains(other, `class="running-box"`) {
		t.Fatalf("stopping one repository stopped another: %s", other)
	}
	s.post(t, "/jobs/2/stop", "/jobs/2?notice=stop")
	s.nextStatus(t, "error")
}
