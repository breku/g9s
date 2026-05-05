# AGENTS.md

Guidance for AI coding agents working on **g9s** — a terminal UI for Google
Cloud Platform resources, modeled after k9s.

`opencode.json` points opencode at this file, so it loads automatically.

---

## Project at a glance

- **Module:** `github.com/brekol/g9s`
- **Entry point:** `main.go` → `cmd.Execute()` (cobra)
- **Single binary:** `g9s` (root + `version` subcommand)
- **Go:** 1.25
- **Released by GoReleaser** on `v*` tag pushes (`.github/workflows/release.yml`)

There is no README; this file is the authoritative orientation doc.

## Repo layout

```
main.go              entry point
cmd/                 cobra CLI (root.go: flags/viper/logger; version.go)
internal/
  config/            Config{Project, LogLevel}; ADC fallback to ~/.config/gcloud
  gcp/clients.go     lazily-cached GCP clients (sync.Once + generic gcpClient[T])
  dao/               data access layer
    types.go         Accessor, Row, TableData, RowType, YAMLDescriber
    util.go          FormatTime, LastSegment
    cloudrun/        Cloud Run v2 services (+ YAMLDescriber, UpdateServiceFromYAML)
    cloudbuild/      Cloud Build triggers (+ RunTrigger)
    buildhistory/    Cloud Build executions (+ CancelBuild)
    vms/             Compute Engine instances (+ Delete, YAMLDescriber)
    migs/            Managed Instance Groups, zonal+regional (+ YAMLDescriber)
    secrets/         Secret Manager (+ AccessLatestSecret)
    logs/            Cloud Logging helpers (used by overlays, not in registry)
  model/             observer-pattern polling layer
    types.go         TableListener, ResourceMeta, DefaultPageSize=50
    table.go         Table: polling, pagination, merge-on-refresh
    registry.go      Registry, Aliases, Resolve, CompleteCommand
  ui/                tview UI
    app.go           App: tview.Application wrapper, view cache, overlay stack,
                     status bar, command bar, key dispatch
    views.go         ResourceView, Overlay, Hint, HintProvider, Filterable,
                     KeyHandler interfaces; newResourceView factory
    table.go         ResourceTable: reusable component every view embeds
    actions.go       generic y/c/PgDn handler + genericHints
    cloudrun.go vms.go cloudbuild.go buildhistory.go secrets.go   per-resource views
    describeview.go logview.go confirmoverlay.go runoverlay.go    overlays
    cmdbar.go header.go statusbar.go welcome.go colortheme.go yamlcolor.go
bin/                 local build output (g9s); bin/gcptui is a stale leftover
dist/                GoReleaser output (do not hand-edit)
Makefile             build/run/test/lint/tidy/clean/release-dry-run
.goreleaser.yaml     release config (linux/darwin/windows × amd64/arm64)
opencode.json        {"instructions": ["AGENTS.md"]}
```

## Common commands

| Task              | Command                                |
|-------------------|----------------------------------------|
| Build             | `make build` (→ `bin/g9s`)             |
| Run               | `make run`                             |
| Tests             | `make test` (currently no test files)  |
| Lint              | `make lint` (`golangci-lint run ./...`) |
| Vet               | `go vet ./...`                         |
| Tidy modules      | `make tidy`                            |
| Release dry-run   | `make release-dry-run`                 |

## Architecture (3 layers)

### DAO layer — `internal/dao/`

Each resource is a stateless empty struct implementing `dao.Accessor`:

```go
Resource() string
Header() []string
FetchPage(ctx, project, pageToken string, pageSize int) (*TableData, error)
```

- An empty `pageToken` means "first page". The returned `NextPageToken` feeds
  the next call.
- `dao.Row` carries `GetID`, `GetType` (drives row colour: Active=green,
  Error=red, NotActive=grey), `GetColumns`, `CopyColumnValue`.
- Optional capabilities are opted in via compile-time assertions and
  discovered via runtime type assertions. Currently:
  `dao.YAMLDescriber{ DescribeYAML(ctx, id) (string, error) }` enables `y`.
- DAOs may add resource-specific public methods (`CancelBuild`, `Delete`,
  `RunTrigger`, `AccessLatestSecret`, `UpdateServiceFromYAML`) — the matching
  view calls them directly.

### Model layer — `internal/model/`

`model.Table` is the *single* polling/pagination model used by every resource.
It owns:

- The polling goroutine (re-entrant `Watch(ctx)`, `Stop()`, internal `updater`
  ticker driven by `ResourceMeta.RefreshRate`).
- The pagination accumulator (`allRows`, `nextCursor`, `paginated`,
  `loadingPage`). `LoadNextPage()` is the API for PgDn.
- **Merge-on-refresh**: when the user has paginated past page 1, periodic
  polls refresh page 1 in place by ID instead of snapping the user back to the
  top. `nextCursor` is preserved across refreshes.

`Registry` (`internal/model/registry.go`) maps a resource key →
`ResourceMeta{DAO, RefreshRate}`. `Aliases` provides shorthand command names
plus `q`/`quit` sentinels. `DefaultPageSize = 50` is uniform across resources.

Listeners (`TableListener`) are invoked from background goroutines — see
goroutine rules below.

### UI layer — `internal/ui/`

- `App` wraps `tview.Application`, owns a root flex (header, cmdbar, pages,
  statusbar — rebuilt by `relayout()`), a `viewCache` keyed by resource, and
  a single-slot overlay stack.
- Global key dispatch: `:` (cmdbar) and `/` (filter) → `Ctrl-C` (quit) →
  `handleGenericKey` (`y`/`c`/`PgDn`) → active view's `KeyHandler.HandleKey`.
- `ResourceView` is the unified interface. `ResourceTable` is the embeddable
  component every concrete view uses; it wires `model.Table` + listener +
  render + filter + PgDn pagination.
- Overlays (`Overlay`: `Primitive`, `RenderLoading`, `Start`, `OnClose`) are
  pushed/popped via `App.PushOverlay` / `App.PopOverlay`.
- Hints: `Header.SetViewHints(viewHintProvider(view))` is called on every
  view/overlay change. `genericHints` advertises `y` (only if DAO is a
  `YAMLDescriber`), `c`, `PgDn`. Each view's `Hints()` returns *only* its own
  additions.

## Adding a new resource

Reference the simplest existing example: `internal/dao/secrets/` +
`internal/ui/secrets.go`. Steps:

1. **DAO** — `internal/dao/<name>/<name>.go`:
   - Empty struct `type Foo struct{}` plus `type FooRow struct { id string;
     rowType dao.RowType; columns []dao.Column; <typed fields> }`.
   - Compile-time assertions:
     `var (_ dao.Accessor = (*Foo)(nil); _ dao.Row = (*FooRow)(nil))`.
   - Implement `Resource()`, `Header()`, `FetchPage(...)`. Use cached client
     from `internal/gcp/clients.go` (add a new accessor there if needed),
     pass `PageToken`/`PageSize`, return
     `&dao.TableData{Header, Rows, NextPageToken: it.PageInfo().Token}`.
   - Wrap errors as `fmt.Errorf("<pkg>: <op>: %w", err)`.
   - Optional: `DescribeYAML` for `y` (proto → JSON via `protojson` → map →
     YAML via `yaml.v3`); see `cloudrun.DescribeYAML`.

2. **Register** — `internal/model/registry.go`:
   - Add to `Registry`: `"foo": {DAO: new(foo.Foo), RefreshRate: 30 * time.Second}`.
   - Add to `Aliases` (canonical + any shortcut).

3. **View** — `internal/ui/foo.go`:
   - Embed `*ResourceTable`; hold `app *App`, `dao *foo.Foo`.
   - Constructor: `NewResourceView(a, project, "foo", "Foo", "foo items", d)`.
   - Compile-time assertions for `ResourceView`, `KeyHandler`, `HintProvider`
     (only those you implement).
   - `Hints()` returns *only* resource-specific keys.
   - `HandleKey` returns `true` when the key is consumed.
   - Long-running actions: `v.app.TrackOp("Action name", func(ctx) error {...})`.
     Confirm/log/describe UX: `v.app.PushOverlay(...)`.

4. **Wire the factory** — `internal/ui/views.go` `newResourceView` switch:
   `case "foo": return NewFooView(a, project)`.

That's three files to edit (registry, factory, two new files).

## Conventions

- **Compile-time interface assertions** for every DAO, every view, every
  optional capability. `var _ dao.Accessor = (*Foo)(nil)`.
- **Goroutine boundaries** are strict:
  - `TableListener` callbacks fire on the model's background goroutine.
  - tview state mutations *must* run on the main goroutine.
  - Always dispatch via `app.runOnUI(fn)` — it already wraps
    `tview.QueueUpdateDraw` in a goroutine to avoid the
    main-goroutine-deadlock pattern. Do **not** call `QueueUpdateDraw`
    synchronously from the main goroutine.
- **Long-running ops** go through `app.TrackOp(name, fn)`, which runs `fn`
  on `App.ctx` (outlives view switches) and surfaces "running" / "succeeded"
  / "failed" on the status bar.
- **Typed Row fields** for every action handler — `vms.InstanceRow.NumericID`,
  `buildhistory.BuildRow.BuildID`, etc. Keep parsing in the DAO, not the UI.
- **Optimistic UI updates** (e.g. `BuildRow.SetStatusColumn("Cancelling...")`
  before the API replies) are intentional — the next poll tick replaces them
  with the authoritative status.
- **Error wrapping**: always `fmt.Errorf("<pkg>: <op>: %w", err)`.
- **Logging**: `github.com/rs/zerolog/log`. Logs go to a file under
  `os.UserCacheDir()/g9s/g9s.log` (truncated each session). Never log to
  stderr — it corrupts the tview screen.
- **Pagination** is owned by `model.Table`. The UI just renders and forwards
  PgDn. Don't reintroduce per-view cursor state.
- **No cache between view switches** — switching back always re-fetches the
  first page. This is intentional; preserve it.
- **Doc comments** are expected on every package, exported symbol, and
  architectural file (`internal/model/table.go`, `internal/ui/app.go`,
  `internal/ui/table.go`, `internal/gcp/clients.go`).

## GCP clients & auth

- `internal/gcp/clients.go` constructs each client lazily via a generic
  `gcpClient[T]` cell + `sync.Once`; both successes and failures are cached
  for the process lifetime. No `Close()` — the OS reclaims gRPC connections
  on exit (same pattern as k9s).
- Construction uses `context.Background()` so per-view ctx cancellation
  doesn't tear down a cached client; the caller's ctx is passed to RPCs.
- Auth is **Application Default Credentials** via
  `google.FindDefaultCredentials`. Project resolution
  (`internal/config/config.go`): `--project` / `G9S_PROJECT` first, then
  active gcloud configuration parsed from `~/.config/gcloud/active_config`.

## Testing

There are currently **no tests** anywhere in the repo. `make test` is a
no-op. The shape of `internal/gcp/clients.go` (global state, no interfaces
around clients) makes unit-testing DAOs hard without a refactor — flag this
to the user before adding test scaffolding.

## Gotchas — read before changing things

- **Never write to `os.Stderr`** from app code. `cmd/root.go` redirects it to
  the log file precisely because gRPC/oauth2 writing to stderr corrupts the
  tview screen.
- **Don't bind `q` globally as quit** — `Ctrl-C` is the only quit binding.
  Overlays may bind `q` locally.
- **Don't bind `Esc` globally** — overlays consume it for dismissal.
- **`Watch` is re-entrant by design** — it cancels the previous poller. Don't
  add "already watching" guards.
- **Adding a resource touches three files**: `internal/model/registry.go`
  (Registry + Aliases), `internal/ui/views.go` (factory switch), and the new
  DAO + view files.
- **Architectural backbone files** — modify only when intentionally extending
  the framework, since changes ripple through every resource:
  - `internal/dao/types.go`
  - `internal/model/table.go`, `internal/model/types.go`
  - `internal/ui/app.go`, `internal/ui/table.go`, `internal/ui/views.go`
- **`dist/` is generated** by GoReleaser. **`bin/gcptui`** is a stale binary
  from before the rename — ignore it.
- **Page size is uniform** (`model.DefaultPageSize = 50`). If a future
  resource needs a different size, that's a model-layer change, not a DAO
  override.
