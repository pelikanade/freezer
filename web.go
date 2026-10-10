package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var webFiles embed.FS

const pageSize = 50
const transcriptLines = 2000

type repositoryView struct {
	Repository
	State  RepositoryState
	Latest *Job
	Queued *BuildEvent
	Active bool
}
type jobGroup struct {
	Repo, Commit string
	Jobs         []Job
	Queued       *BuildEvent
}
type transcriptLine struct {
	Number      int
	Text, Class string
}
type pageData struct {
	Kind, Title, Path, ToggleURL, ReloadURL string
	Paused, Busy                            bool
	Refresh, Running, Queued                int
	Repositories                            []repositoryView
	Groups                                  []jobGroup
	Job                                     *Job
	Lines                                   []transcriptLine
	TotalLines                              int
	Older, Newer                            string
	Notice, NoticeDetail                    string
	Code                                    int
	ErrorTitle, ErrorDetail                 string
	Now                                     string
}

var pages = template.Must(template.New("pages").Funcs(template.FuncMap{
	"short": func(s string) string {
		if len(s) > 8 {
			return s[:8]
		}
		return s
	},
	"clock":    func(t time.Time) string { return t.Format("2006-01-02 15:04:05 UTC") },
	"duration": jobDuration,
	"label": func(r Result) string {
		if r.MatchedNothing {
			return "matched nothing"
		}
		return r.Status
	},
	"icon":    resultIcon,
	"summary": resultSummary,
	"clip": func(s string) string {
		r := []rune(s)
		if len(r) > 100 {
			return string(r[:100]) + "…"
		}
		return s
	},
}).ParseFS(webFiles, "web/*.html"))

func jobDuration(j Job) string {
	if j.Status == "interrupted" && j.FinishedAt == nil {
		return "interrupted"
	}
	end := time.Now()
	if j.FinishedAt != nil {
		end = *j.FinishedAt
	}
	d := end.Sub(j.StartedAt).Truncate(time.Second)
	if d < 0 {
		d = 0
	}
	if j.Status == "running" {
		return d.String() + " so far"
	}
	return d.String()
}

func resultIcon(r Result) string {
	if r.MatchedNothing {
		return "none"
	}
	if r.CacheHit {
		return "hit"
	}
	switch r.Status {
	case "running":
		return "run"
	case "succeeded":
		return "ok"
	case "failed":
		return "fail"
	case "stopped":
		return "stopped"
	case "interrupted":
		return "int"
	default:
		return "clock"
	}
}

func resultSummary(r Result) string {
	if r.MatchedNothing {
		return "nothing verified or published"
	}
	if r.CacheHit {
		return "cached — nothing built"
	}
	switch r.Status {
	case "running":
		return "decided when the job finishes"
	case "succeeded":
		return fmt.Sprintf("published %d paths", r.Published)
	case "stopped":
		return "stopped by the operator"
	case "interrupted":
		return "freezer restarted mid-build"
	default:
		summary, _, _ := strings.Cut(r.Error, "\n")
		return summary
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /static/style.css", func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, webFiles, "web/style.css") })
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /jobs/{id}", s.jobPage)
	mux.HandleFunc("GET /jobs/{id}/log", s.jobTranscript)
	mux.HandleFunc("POST /hooks/{repo}", s.webhook)
	mux.HandleFunc("POST /build/{repo}", s.buildNow)
	mux.HandleFunc("POST /jobs/{id}/stop", s.stopJob)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.errorPage(w, r, 404, "Page not found", "Return to the job list.")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func pageFor(r *http.Request, title, kind string) pageData {
	paused := r.URL.Query().Get("refresh") == "0"
	query := r.URL.Query()
	query.Del("notice")
	query.Del("repo")
	query.Del("id")
	reload := r.URL.Path
	if encoded := query.Encode(); encoded != "" {
		reload += "?" + encoded
	}
	if paused {
		query.Del("refresh")
	} else {
		query.Set("refresh", "0")
	}
	toggle := r.URL.Path
	if encoded := query.Encode(); encoded != "" {
		toggle += "?" + encoded
	}
	p := pageData{Title: title, Kind: kind, Path: r.URL.Path, Paused: paused, ToggleURL: toggle, ReloadURL: reload, Now: time.Now().UTC().Format("2006-01-02 15:04:05 UTC")}
	switch r.URL.Query().Get("notice") {
	case "build":
		p.Notice = "Build requested for " + r.URL.Query().Get("repo") + "."
		p.NoticeDetail = "The current default branch will be published, even if already built."
	case "stop":
		p.Notice = "Stop requested."
		p.NoticeDetail = "Running jobs are stopping; no further jobs of this event will start."
	}
	return p
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, code int, p pageData) {
	var body bytes.Buffer
	if err := pages.ExecuteTemplate(&body, "page", p); err != nil {
		http.Error(w, "Could not render page", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	w.Write(body.Bytes())
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	p := pageFor(r, "Jobs", "index")
	before, after := r.URL.Query().Get("before"), r.URL.Query().Get("after")
	if before != "" && after != "" {
		s.errorPage(w, r, 400, "Invalid page", "Use either Newer or Older.")
		return
	}
	for _, id := range []string{before, after} {
		if id != "" {
			if n, err := strconv.ParseUint(id, 10, 64); err != nil || n == 0 {
				s.errorPage(w, r, 400, "Invalid page", "The page cursor must be a positive job number.")
				return
			}
		}
	}
	query := s.db.Omit("transcript").Order("id DESC").Limit(pageSize)
	if before != "" {
		query = query.Where("id < ?", before)
	}
	if after != "" {
		query = s.db.Omit("transcript").Order("id ASC").Limit(pageSize).Where("id > ?", after)
	}
	var jobs []Job
	if err := query.Find(&jobs).Error; err != nil {
		s.errorPage(w, r, 500, "Could not read jobs", err.Error())
		return
	}
	if after != "" {
		for i, j := 0, len(jobs)-1; i < j; i, j = i+1, j-1 {
			jobs[i], jobs[j] = jobs[j], jobs[i]
		}
	}
	for _, repo := range s.cfg.Repositories {
		view := repositoryView{Repository: repo}
		if err := s.db.First(&view.State, "name = ?", repo.Name).Error; err != nil {
			s.errorPage(w, r, 500, "Could not read repository", err.Error())
			return
		}
		var latest []Job
		if err := s.db.Omit("transcript").Where("repo = ?", repo.Name).Order("id DESC").Limit(1).Find(&latest).Error; err != nil {
			s.errorPage(w, r, 500, "Could not read repository", err.Error())
			return
		}
		if len(latest) > 0 {
			view.Latest = &latest[0]
		}
		s.mu.Lock()
		run := s.repos[repo.Name]
		view.Active = run.active
		if run.queued != nil {
			event := *run.queued
			view.Queued = &event
			p.Queued++
		}
		s.mu.Unlock()
		if view.Active || view.Queued != nil {
			p.Busy = true
		}
		p.Repositories = append(p.Repositories, view)
	}
	var running int64
	if err := s.db.Model(&Job{}).Where("status = ?", "running").Count(&running).Error; err != nil {
		s.errorPage(w, r, 500, "Could not read jobs", err.Error())
		return
	}
	p.Running = int(running)
	if p.Running > 0 && !p.Paused {
		p.Refresh = 15
	}
	for _, job := range jobs {
		n := len(p.Groups)
		if n == 0 || p.Groups[n-1].Repo != job.Repo || p.Groups[n-1].Commit != job.Commit {
			p.Groups = append(p.Groups, jobGroup{Repo: job.Repo, Commit: job.Commit})
		}
		group := &p.Groups[len(p.Groups)-1]
		group.Jobs = append(group.Jobs, job)
	}
	for _, repo := range p.Repositories {
		if repo.Queued == nil {
			continue
		}
		found := false
		for i := range p.Groups {
			if p.Groups[i].Repo == repo.Name {
				p.Groups[i].Queued = repo.Queued
				found = true
				break
			}
		}
		if !found {
			p.Groups = append([]jobGroup{{Repo: repo.Name, Queued: repo.Queued}}, p.Groups...)
		}
	}
	if len(jobs) > 0 {
		var count int64
		if err := s.db.Model(&Job{}).Where("id > ?", jobs[0].ID).Count(&count).Error; err != nil {
			s.errorPage(w, r, 500, "Could not read page", err.Error())
			return
		}
		if count > 0 {
			p.Newer = "/?after=" + strconv.FormatUint(jobs[0].ID, 10)
		}
		if err := s.db.Model(&Job{}).Where("id < ?", jobs[len(jobs)-1].ID).Count(&count).Error; err != nil {
			s.errorPage(w, r, 500, "Could not read page", err.Error())
			return
		}
		if count > 0 {
			p.Older = "/?before=" + strconv.FormatUint(jobs[len(jobs)-1].ID, 10)
		}
	}
	if p.Paused {
		if p.Newer != "" {
			p.Newer += "&refresh=0"
		}
		if p.Older != "" {
			p.Older += "&refresh=0"
		}
	}
	s.render(w, r, 200, p)
}

func (s *Server) jobPage(w http.ResponseWriter, r *http.Request) {
	job, err := s.findJob(r.PathValue("id"))
	if err != nil {
		s.jobError(w, r, err)
		return
	}
	p := pageFor(r, fmt.Sprintf("Job #%d", job.ID), "job")
	p.Job = &job
	if job.Status == "running" {
		p.Running = 1
		if !p.Paused {
			p.Refresh = 10
		}
	}
	if job.Transcript != "" && !job.CacheHit {
		text := strings.TrimSuffix(job.Transcript, "\n")
		p.TotalLines = strings.Count(text, "\n") + 1
		start := 0
		if p.TotalLines > transcriptLines {
			position := len(text)
			for range transcriptLines {
				j := strings.LastIndexByte(text[:position], '\n')
				if j < 0 {
					break
				}
				position = j
			}
			start = position + 1
		}
		lines := strings.Split(text[start:], "\n")
		number := p.TotalLines - len(lines) + 1
		for _, line := range lines {
			class := ""
			if strings.HasPrefix(line, "$ ") {
				class = "cmd"
			}
			if strings.HasPrefix(strings.TrimSpace(line), "error:") || strings.Contains(line, "failed with exit code") {
				class = "err"
			}
			p.Lines = append(p.Lines, transcriptLine{Number: number, Text: line, Class: class})
			number++
		}
	}
	s.render(w, r, 200, p)
}

func (s *Server) jobTranscript(w http.ResponseWriter, r *http.Request) {
	job, err := s.findJob(r.PathValue("id"))
	if err != nil {
		s.jobError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, job.Transcript)
}

func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, code int, title, detail string) {
	p := pageFor(r, http.StatusText(code), "error")
	p.Code = code
	p.ErrorTitle = title
	p.ErrorDetail = detail
	s.render(w, r, code, p)
}
