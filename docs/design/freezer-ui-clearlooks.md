# Freezer UI (Clearlooks)

**Related:** [Freezer](../../README.md)

**Kind:** redesign SPEC (layout / tokens / copy / AC). **No application code.** Stack: Go, with `html/template` for the UI (ADR 0016). Freezer is being rewritten from scratch; this note is the UI target, not a patch plan against any existing codebase.

**Direction (Noa, 2026-10-08):** GNOME 2.x Clearlooks chrome as structure, not costume. One hand-written stylesheet carrying the tokens; no CSS framework. No JS required; tiny progressive enhancement optional. Light only. Thin ops surface (ADR 0015). No in-app auth (ADR 0011).

**Inputs:** the Clearlooks reference screenshots below; Quickshell Clearlooks prototype tokens (pelikanade/flake PR #25 `quickshell.prototype.html`).

**Mock:** [prototypes/freezer-clearlooks.html](prototypes/freezer-clearlooks.html) — single self-contained HTML, no CDN, no JS. Screens via `:target` (`#index`, `#job-running`, `#job-failed`, `#job-stopped`, `#job-interrupted`, `#job-nothing`, `#job-hit`, `#job-truncated`, `#empty`, `#e404`, `#e409`). Prefixed `#shot-*` hides the mock palette for screenshots.

---

## Reference lock

![Clearlooks 2.20 widget gallery](attachments/design-linux-clearlooks-widgets.png)

*Clearlooks 2.20 widget gallery (Commons) — titlebars, toolbar, notebook tabs, sunken list, selection, Fermer button.*

![Fedora Core 5 GNOME 2 with Clearlooks](attachments/design-linux-gnome2-fedora-clearlooks.png)

*Fedora Core 5 GNOME 2 + Clearlooks — dual panels, Nautilus chrome.*

![Ubuntu Clearlooks desktop](attachments/design-clearlooks-era-ubuntu-clearlooks.png)

*Ubuntu Clearlooks desktop — soft plastic panels + blue titlebars.*

Steal: soft grey window plate, Metacity blue titlebar, raised gradient buttons, sunken treeview with rules-hint, notebook/location buttons, GtkInfoBar strip, statusbar panes, DejaVu Sans ~10pt, monospace sunken text view. Reject: Adwaita flat, Compiz glass as default, Crystal candy icons, framework cards/pills, modern capsule chrome.

---

## Mock screenshots

![Index at 1280×800](attachments/design-freezer-clearlooks-index.png)

*Index @ 1280×800 — repositories treeview + recent jobs grouped by commit + Build-now infobar.*

![Running job](attachments/design-freezer-clearlooks-job-running.png)

*Running job — toolbar Stop, pulse progress, cache/paths still pending.*

![Failed job](attachments/design-freezer-clearlooks-job-failed.png)

*Failed job — error InfoBar + transcript opened at the end (no grey chips).*

![Index at 390×844](attachments/design-freezer-clearlooks-mobile.png)

*Index @ 390×844 — stacked chrome, status word under each entry, touch-sized Build/Stop.*

![Failed job at 390×844](attachments/design-freezer-clearlooks-mobile-job-failed.png)

*Failed job @ 390×844 — props collapse to one pair per row; transcript still opens at its end.*

Hero for the braid: **index desktop**. Failed + running prove the state vocabulary; mobile proves the collapse.

*The screenshots above are the 2026-10-08 review; the desktop ones predate the window frame added on 2026-10-16 (§1), which leaves every element inside the frame untouched. The mobile shots still match: ≤640px the frame collapses.*

---

## 1. Layout

### Shared chrome

The whole stack is one floating window on a desktop backdrop:

```
   ┌── body: --fz-desktop backdrop, padding = the desktop strip ───┐
   │  ┌─ .window: 1px --fz-frame, 5px/5px/2px/2px, --fz-cast ────┐ │
   │  │ Metacity titlebar (page title)                          │ │
   │  │ toolbar (Reload · Auto · Stop)                          │ │
   │  │ pathbar (job pages only)                                │ │
   │  │ optional GtkInfoBar                                     │ │
   │  │ content (repos + jobs / job)                            │ │
   │  │ statusbar                                               │ │
   │  └─────────────────────────────────────────────────────────┘ │
   └──────────────────────────────────────────────────────────────┘
```

- **Frame (2026-10-16):** `.window` floats on a GNOME 2 backdrop, as on nvirellia.im. `body` paints `--fz-desktop` (`fixed`, so the gradient is the viewport's, not the document's) and its padding is the visible desktop strip; `.window` is one flex column that fills what is left of the viewport and grows with the page, capped at 1240px. ≤640px the strip, border, corner radii and shadow all drop, so the mobile layout stays edge to edge.
- Navigation is toolbar + pathbar (GNOME-app grammar). Repos live in the index content as a treeview; `#repo-{Name}` anchors stay.
- Titlebar is CSS chrome (not a real WM) — so is the frame around it: nothing minimises, maximises or closes. Page title stays `{Title} · freezer`.
- Statusbar at the bottom: live counts, refresh note, last-rendered clock.

### Index (`GET /`)

| Region | Target |
|--------|--------|
| Repos | Treeview first (ops write surface): Name · Forge · Last built · Latest job · Build now |
| Jobs | **Recent jobs** as a sunken GtkTreeView below repos: columns Status · Entry · Job · Started · Duration · Result · Action; group rows by `(repo, commit)` with a rowspan identity column. The columns are fixed and sized to their content — Started carries a full `YYYY-MM-DD HH:MM:SS UTC` stamp and Duration an hour-long elapsed time, both `nowrap` — so between 641 and 1100px the Result column drops out (as it already does ≤640px) and the treeview scrolls sideways past 919px rather than scaling the rest: a scaled `nowrap` column paints over its neighbour |
| Feedback | Optional InfoBar from `?notice=` (see § Feedback) |
| Empty | Empty row inside the jobs treeview; repos empty-state is a separate dashed well only if config is empty |

### Job page (`GET /jobs/{id}`)

| Region | Target |
|--------|--------|
| Header | Hero: status icon + entry + one-line summary; Stop lives on the **toolbar** when running |
| KV | Label:value grid, 3×2 then 2×2 ≤1100px, 1×2 ≤640px; full SHA wraps as a block |
| Alerts | GtkInfoBar: **error** failed; **question** stopped; **warning** interrupted / matched-nothing; **info** Build-now / Stop acknowledged |
| Running | Pulse progress bar + “Ns so far”; Cache and Published show *pending*, not miss/0 |
| Transcript | Sunken text view (no grey per-line chips), **opens at the end** (CSS `column-reverse` or `#end` + optional tiny PE); truncated line callout; “Open as plain text” → `/jobs/{id}/log` |
| Cache hit | Empty text-view well: “Already in the cache…” |
| Interrupted, empty transcript | Empty well + warning InfoBar (restart explanation) |

### Error page

GtkMessageDialog card: icon + primary + hint + HTTP code + default Back button.

### Mobile ~390px

- The frame collapses to full bleed: no desktop strip, border, radius or shadow — the chrome keeps the whole viewport.
- Titlebar + toolbar compress (hide long labels).
- Pathbar crumbs truncate (entry crumb hides the monospace fragment).
- Treeview headers hide; each row becomes icon + entry link + secondary line (status · repo · short sha · when · duration); Build now / Stop stay visible.
- Props collapse to 2 columns. Statusbar drops the middle pane.

---

## 2. Token map

Custom properties in one hand-written stylesheet (and only there). No CSS framework. Values sourced below; implementer may nudge ±1–2 for AA if measured differently.

| Token | Value | Source |
|-------|-------|--------|
| `--fz-bg` | `#edeceb` | Clearlooks gtkrc `bg_color` (gnome-themes 2.x / Arch bbs Clearlooks threads) |
| `--fz-base` | `#ffffff` | gtkrc `base_color` |
| `--fz-stripe` | `#f4f3f2` | rules-hint row = mix(base, bg); sampled Clearlooks 2.20 list |
| `--fz-prelight` | `#eaf1fa` | row hover = base tinted toward selected |
| `--fz-text` | `#2e3436` | Tango Aluminium 6 (QS prototype `--text`) — **10.7:1** on bg |
| `--fz-muted` | `#555753` | Tango Aluminium 5 — **6.2:1** on bg |
| `--fz-dim` | `#888a85` | Aluminium 4 — decoration / line-numbers only, never primary text |
| `--fz-link` | `#204a87` | Tango Sky Blue 3 — **8.8:1** on base |
| `--fz-selected` | `#86abd9` | gtkrc `selected_bg_color` (classic Clearlooks) |
| `--fz-selected-hi` | `#a9c6ea` | top stop of selection / progress candy |
| `--fz-accent` / `--fz-focus` | `#3465a4` | Tango Sky Blue 2 (QS `--accent` / `--focus`) |
| `--fz-edge` | `#8a8680` | button outline — ≥ **3:1** on bg |
| `--fz-edge-soft` | `#bdbab8` | treeview header separators |
| `--fz-desktop` | `linear-gradient(#e3e6ea → #d2d6dc)` fixed | GNOME 2 backdrop the window floats on (nvirellia.im) |
| `--fz-frame` | `#5f5c58` | window frame outline (Metacity) |
| `--fz-cast` | `0 2px 4px rgba(0,0,0,.12), 0 10px 28px rgba(30,40,60,.18)` | the window's cast shadow |
| `--fz-hilight` | `#ffffff` | top bevel |
| `--fz-btn` | `linear-gradient(#fff → #f6f5f4 → #e3e1de)` | Clearlooks button fill (sampled + QS `--fill`) |
| `--fz-btn-hover` | mix toward selected (light blue wash) | gtkrc button PRELIGHT |
| `--fz-btn-active` | `linear-gradient(#c9c7c4 → #dedcd9)` | shade(0.85, bg) |
| `--fz-header` | light vertical gradient `#fcfcfc → #e7e6e5` | treeview column header (sampled) |
| `--fz-toolbar` | `#f4f3f2 → #e4e3e2` | toolbar strip (sampled Nautilus/Clearlooks) |
| `--fz-title` | `#4f80c2 → #3f72b3 → #3767a6 → #2c5a97` | Metacity active title sampled at `#7db6eb → #4c91d1` (FC5 Nautilus) and `#9cbde5 → #7ba2d2` (Clearlooks 2.20); darkened ~25% so white bold title text is ≈4.5–7:1 across the text band (white on `#86abd9` is only 2.4:1) |
| `--fz-select` | selected-hi → selected | tree selected row |
| `--fz-track` / `--fz-fill` | grey trough / blue candy | progress (QS `--track` / `--track-fill`) |
| InfoBar info | bg `#ffffbf` edge `#b8ad9d` | GtkInfoBar GTK 2.24 defaults |
| InfoBar warning | bg `#fcaf3e` edge `#b07a2b` | same |
| InfoBar question | bg `#c6d8ee` edge `#627bd9` | same (lightened fill for AA) |
| InfoBar error | bg `#f6cdc9` edge `#a82727` | lightened from GTK default `#f03838` for AA with `--fz-text` |
| Status inks | ok `#4e9a06` · fail `#cc0000` · run `#3465a4` · stop `#555753` · int `#ce5c00` · nothing `#8f5902` | Tango |
| `--fz-font` | `"DejaVu Sans", "Bitstream Vera Sans", Verdana, sans-serif` | GNOME 2 default face |
| `--fz-mono` | `"DejaVu Sans Mono", …` | transcript |
| `--fz-size` | `13px` (~ Sans 10 @ 96dpi) | |
| `--fz-radius` | `3px` | Clearlooks buttons/tabs |

**Deliberate deviation:** Clearlooks drew selected rows with white text on `#86abd9` (2.4:1, fails AA). Here, selected and focused rows keep `--fz-text` on the selection gradient (5.3–7.2:1).

**Contrast gates (must hold):** body ≥ 7:1; link ≥ 4.5:1; muted text ≥ 4.5:1 on bg and stripe; button border ≥ 3:1; focus ring 2px solid `--fz-focus` with offset; `:focus-visible` on all interactive controls.

**Out:** framework utility classes, structural status selectors, system-ui 15px stack, card hover lift.

---

## 3. Components

| Component | Class / pattern | Notes |
|-----------|-----------------|-------|
| Window frame | `.window` + `.titlebar` | One floating window: `.window` carries the frame, the corner radii and the cast shadow; the titlebar holds icon + page title. Both decorative — no minimise/maximise/close, because this is a page, not a WM. Not interactive. Skip link (`.skip`) sits outside the frame, above it. |
| Toolbar | `.toolbar` + `.tb` | Reload, Auto-refresh toggle (aria-pressed), Stop (running job), Jobs (detail). Real `<button>` / `<a>`. |
| Pathbar | `.pathbar` + `.btn-path` | Job pages: freezer → repo → `#id entry`. Current crumb `aria-current="page"`. Omit on index. |
| Treeview | `.tv table` | Sunken border, sticky header, rules-hint stripes, hover prelight, selected row uses `--fz-select`. Explicit `.fx-status` (or `.st`) class — **never** structural selectors. |
| Status icon+word | `.st` / `.st-{status}` | Icon + word; never colour alone. Words: `planned` · `running` · `succeeded` · `failed` · `stopped` · `interrupted` · `matched nothing` (display only; underlying status stays `succeeded` for matched-nothing). A job that never took a build slot (`started_at` zero, whatever status ended it) shows a dash for Started and Duration; a planned job's page says it waits for a build slot. |
| Progress | `.pbar` | Pulse candy for running (respect `prefers-reduced-motion`). |
| Button | `.btn` / `.btn-sm` / `.btn-default` | Gradient Clearlooks; default-button ring for the dialog primary. |
| InfoBar | `.infobar.{info,warning,question,error}` | Icon + primary + secondary + optional acts. `role="alert"` only for **failed**; running/notice use `role="status"`. |
| Statusbar | `.statusbar` | Panes: summary · refresh note · clock. |
| Transcript text view | `.textview` | Sunken; `max-height:min(50vh,400px)`; `display:flex; flex-direction:column-reverse` so it opens at the end; gutter line numbers + `.l.err` (lines starting `error:`) / `.l.cmd` (lines starting `$ `) are **P2** (need per-line spans and a `--first` counter offset); empty well for no-transcript cases. |
| Message dialog | `.dialog` | 404 / 409 / 400 / 500. |
| Icons | Inline SVG `<symbol>` + `<use>` | **Original drawings** in the Tango palette. Do **not** hotlink or vendor the Tango / Crystal icon *files*. Tango palette is public domain; note that in the CSS header. Size 16/20/24/48. |

---

## 4. Copy cuts & rewrites

| Where | Cut / change |
|-------|----------------|
| Index empty CTA | Shorten to one sentence + point at the repos treeview (no “Need help getting started?”). |
| Job danger alert for stopped | **Do not** use “The job did not succeed” for operator stop. Use question InfoBar: **“You stopped this build.”** |
| Interrupted | Warning InfoBar: freezer restarted; commit not recorded; Build now CTA. |
| Matched nothing | Warning InfoBar even though status is succeeded: **“The glob matched nothing.”** Cite the entry; point at `.freezer.yaml`. |
| Failed | Keep exact string **`The job did not succeed`**. Secondary line: the raw job error. |
| Running cache/paths | Replace premature `miss` / `0` with *decided when the job finishes* / *—*. |
| Cache hit body | Keep the explanation; render inside the empty text-view well, not a bare paragraph beside an empty pre. |
| 404 hint | Drop “deleted by retention” until retention exists; say the job is missing or never existed. |
| Index duration | New column (FinishedAt−StartedAt); running shows elapsed + pulse. |
| Index result | Truncated job error or a short derived phrase (`published N paths`, `cached — nothing built`, `nothing verified or published`, `stopped by the operator`, `freezer restarted mid-build`). |

---

## 5. States to cover

| State | Treatment |
|-------|-----------|
| No jobs | Empty well in jobs treeview + Build now still usable |
| No repos | Empty well (boot normally rejects this; keep for honesty) |
| Never-built repo | Italic “never built”; Latest = “no jobs” |
| Running | Spinner + pulse bar; Stop on row + toolbar; meta refresh 10s (job) / 15s (index if any running); Cache/Published pending |
| Succeeded (built) | Green check; duration; published count |
| Planned | Clock icon + “planned”; Started and Duration show —, the Cache cell reads “not yet probed”, Published stays —, and the transcript well says the job waits for a build slot. An event that ends first leaves the row `stopped`; a restart leaves it `interrupted` (ADR 0025) — those rows never took a build slot either, so they keep every one of those dashes. |
| Succeeded + cache hit | Distinct hit icon (check + badge); no transcript; empty well |
| Succeeded + matched nothing | Warning InfoBar + “matched nothing” label; underlying status remains succeeded; 0 paths; cache “not probed” |
| Failed | Error InfoBar with **`The job did not succeed`**; transcript opens at end; red error lines |
| Stopped | Question InfoBar (not danger); grey stop icon; Build now CTA |
| Interrupted | Warning InfoBar; orange triangle; empty transcript well; no FinishedAt |
| Truncated transcript | “Showing the last N of M lines.” + link to full plain text |
| 404 / 409 / 400 / 500 | Dialog chrome; preserve status codes |
| Mobile ~390px | Collapse as §1 |
| Feedback after Build now / Stop | InfoBar via `?notice=build&repo=` / `?notice=stop&id=` (see below) |

### Feedback (redirect target)

Actions answer with **303 See Other** and pages are not cached (`Cache-Control: no-store`):

- `POST /build/{repo}` → `303` to `/?notice=build&repo={name}`
- `POST /jobs/{id}/stop` → prefer `303` to `/jobs/{id}?notice=stop` (stay on the job); if `/` is kept, use `/?notice=stop&id={id}`
- Index/job render an InfoBar when those query params are present; links “Dismiss” → same path without query.

No cookies, no CSRF (unchanged).

### Grouping

Group consecutive jobs that share `(Repo, Commit)` with a rowspan identity cell (repo icon + short SHA). No new schema. Event/batch id remains unavailable.

### Auto-refresh (tradeoff)

Default stays no-JS: `<meta http-equiv="refresh">` on the running job page (10 s) and on the index only while a job runs (15 s). Add a visible toolbar toggle and statusbar note (“Reloads every 15 s while a job runs · Pause”) that links to the same URL with `?refresh=0`, which suppresses the meta tag. Cost: a full reload resets scroll on long lists and makes screen readers re-read the page, which is why the running notice uses `role="status"` (not `role="alert"`) and why Pause exists. An optional script of 20 lines or fewer could restore scroll position after reload (sessionStorage). Recommendation: don't ship it in v1, because the page works without it and the product is no-JS.

### Transcript opens at the bottom

Primary: CSS-only — wrap the transcript in a flex column-reverse scroller so the last line is initially visible. Secondary optional PE (≤20 lines): if `#end` is in the URL or the job failed, `scrollIntoView` once; document the tradeoff (AT focus jump, must be optional). Do **not** reverse the transcript text.

---

## 6. Acceptance criteria

1. One hand-written stylesheet carrying the Clearlooks tokens above; no CSS framework; AA contrast held for body, link, muted, borders, focus.
2. Index shows repos treeview + jobs treeview; jobs grouped by `(repo, commit)` when contiguous; Duration and a Result column present.
3. Status icons distinguish **failed / stopped / interrupted / planned / matched-nothing / cache-hit / running / succeeded**; words remain; colour never alone.
4. Matched-nothing jobs carry a warning InfoBar and do not read as ordinary success.
5. Stopped jobs never use the failed danger copy; interrupted jobs explain restart and show an empty-well (no blank transcript).
6. Failed jobs say exactly **`The job did not succeed`**; transcript has no per-line grey chips; initial scroll is at the end.
7. Running jobs do not claim Cache miss / 0 paths; show a pulse progress affordance.
8. Build now / Stop answer with **303**; redirect carries a notice the next HTML render can show as an InfoBar.
9. Live HTML pages are not cached (`Cache-Control: no-store`); a missing job is **404**.
10. Job page shows the full commit SHA and, when a transcript exists, a `$ nix copy` (or `$ nix copy --to`) substring.
11. Mobile ≤640px: usable without horizontal overflow; Build now / Stop reachable; status word still present (in the secondary line if the icon column hides the word).
12. Skip link to main content; `:focus-visible` rings; real submit buttons; status `role="alert"` only where appropriate.
13. Icons are original SVG or public-domain Tango-palette drawings — no hotlinked icon themes.
14. Stop redirects to `/jobs/{id}?notice=stop` and the job page shows the stopped InfoBar.
15. Queued builds render as per-repo "queued" rows (not job rows); distinct status icon and the word "queued".
16. Job list pages (Newer/Older) with a fixed page size; grouping holds within a page.
17. Transcript shows line numbers and tinted error lines with a non-colour marker; one text node per line.

---

## 7. Non-goals

- Auth, CSRF, users/roles (ADR 0011).
- Metrics, retries, store GC, retention UI (until retention exists).
- Live streaming transcripts / HTMX / SPA / JS framework.
- Dark mode.
- Filters/search on the job list (paging only in v1).
- Forging links into GitHub beyond showing `owner/name`.
- Vendoring the Tango or Crystal **icon theme files**.
- Changing nix / polling / SQLite behaviour.

---

## 8. Decisions (Noa, 2026-10-08 and 2026-10-10)

**Decided:**

1. **Stop redirect:** Stop stays on the job page. `POST /jobs/{id}/stop` → `303` to `/jobs/{id}?notice=stop`.
2. **Auto-refresh:** keep the no-JS `<meta refresh>` with the visible Pause link (`?refresh=0`). No scroll-restore script in v1.
3. **Queued builds:** show them. Expose the build waiting for its turn per repo (read-only) so the index can show a "queued" row per repo (received time, plus the pushed sha if the event carries one; the head is still resolved through git at build start) above that repo's running job. With triggering by polling (ADR 0024) an event never carries one, so the row shows the received time and, for a manual event, that it is Build now. A queued row is not a job row — give it its own status icon and the word "queued". Superseded events simply disappear (the slot holds only the newest).
4. **Paging:** the job list gets paging. Keep 100 (or fewer) per page; add Newer / Older links in the jobs treeview footer and statusbar (`/?page=N` or `?before={id}`; keyset on id preferred). Commit grouping applies within a page.
5. **Transcript line numbers + error tint:** ship in v1. One element per line (≤2000); error lines (`error:` prefix, `failed with exit code`) get the red tint plus a non-colour marker (bold or ⚠ gutter) so colour isn't the only cue. The plain-text transcript link stays.
6. **Titlebar:** keep the CSS Metacity-style title bar, as in the mock.
7. **Icons:** original Tango-palette SVGs, as in the mock. No icon-theme files vendored.
8. **Index order:** repositories above jobs, as in the mock and the README's home order.

---

## 9. Build notes

1. No JS required; progressive enhancement optional (scroll-to-end, scroll-restore after meta refresh) and must degrade cleanly.
2. Icons as original SVGs in the Tango palette — do not vendor icon theme files.
3. Open the mock while implementing: `docs/design/prototypes/freezer-clearlooks.html` (static HTML; fake data is fine).
4. Detect matched-nothing as succeeded with no cache hit, zero paths, and a transcript that says the glob matched nothing (or store a flag later); do not invent a new persisted status without an ADR.

## 10. Implementation verification (2026-10-10)

The production templates and stylesheet are in `web/`, embedded in the binary. All seventeen acceptance items were exercised against the running service at desktop (1280×800) and mobile (390×844) sizes. The real build fixtures produced succeeded, failed, cache-hit, matched-nothing, running, stopped and interrupted jobs. The crash-before-first-transcript empty-well case used a constructed persisted running row, then the real restart recovery path.

Browser measurements: body contrast 10.72:1, link 8.78:1, muted 6.19:1 (6.59:1 on stripes), button border 3.07:1; keyboard focus has a 2px ring. Mobile views had no page-level horizontal overflow. Build now and paging worked with JavaScript disabled. The failed job's 2000-line transcript viewport initially showed its final line; plain text retained the entire 2105-line build and command output.

`devenv test` also covers 303 action redirects, status-specific copy, full transcript persistence through restart, HTML truncation, and keyset paging with within-page grouping. Nix publication uses `--print-build-logs` so the stored transcript includes complete build output. Local browser evidence and screenshots are linked from `.project-map/` and remain untracked.

## 11. Frame change (2026-10-16)

The window frame of §1 was added at the owner's direction, referencing nvirellia.im. `web/pages.html` wraps the chrome in `.window` (the skip link stays outside it), and `web/style.css` carries `--fz-desktop`, `--fz-frame` and `--fz-cast` plus the frame rule; the ≤640px block returns the page to full bleed. The mock in `prototypes/` mirrors both.

Verified against the running service in headless Chromium at 1440×900, 1280×800, 750×800, 660×800 and 390×844, on the index, a failed job page and the 404 page: the frame, its corner radii and its shadow render at every width above 640px, the window caps at 1240px, and ≤640px the page is edge to edge with neither strip nor frame. No page-level horizontal overflow at any of those widths.

That strip costs the 641–1100px band 34px, which the repositories table's unspecified Last-built column had been absorbing: it scaled to zero and painted over Latest job. `.tv.repos table` therefore took the same `min-width` floor the jobs table already had, so the treeview scrolls instead of overlapping.
