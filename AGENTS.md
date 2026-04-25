# g9s

A terminal dashboard for Google Cloud Platform resources, inspired by k9s.

## Tech Stack

| Concern       | Library / Tool                                           |
|---------------|----------------------------------------------------------|
| Language      | Go 1.22+                                                 |
| TUI framework | `github.com/rivo/tview` on top of `tcell/v2`             |
| GCP client    | `cloud.google.com/go` (per-service sub-modules)          |
| Auth          | Application Default Credentials via `golang.org/x/oauth2/google` |
| Config        | `github.com/spf13/viper` + `github.com/spf13/cobra`      |
| Logging       | `github.com/rs/zerolog` (structured, stderr)             |
| Build/Release | `goreleaser` + Homebrew tap (`brekol/homebrew-tap`)      |

## Repository Layout

```
main.go                      – entrypoint, calls cmd.Execute()
cmd/
  root.go                    – cobra root command, flag wiring, viper/logger init
  version.go                 – version sub-command
internal/
  cache/cache.go             – thread-safe TTL cache keyed by (resource, project)
  config/config.go           – typed config struct, viper.Unmarshal
  dao/
    types.go                 – Accessor interface + optional capability interfaces (Describer, …)
    cloudrun.go              – Cloud Run v2 DAO (implements Accessor)
    cloudbuild.go            – Cloud Build triggers DAO (implements Accessor)
    buildhistory.go          – Cloud Build history DAO (implements Accessor)
  gcp/client.go              – ADC credential helper, shared option.ClientOption slice
  model/
    types.go                 – TableListener observer interface + ResourceMeta struct
    registry.go              – Registry map: resource key → ResourceMeta (DAO + TTL)
    table.go                 – Table model: polling loop, cache-first refresh, observer fan-out
  ui/
    app.go                   – tview.Application wrapper, root layout, key bindings
    views.go                 – ResourceView + Filterable interfaces, newResourceView() factory
    table.go                 – ResourceTable: reusable tview.Table widget, Render(TableData)
    cmdbar.go                – CmdBar: ':' command / '/' filter input with native dropdown autocomplete
    cloudrun.go              – CloudRunView: implements ResourceView
    cloudbuild.go            – CloudBuildView: implements ResourceView
    buildhistory.go          – BuildHistoryView: implements ResourceView
.goreleaser.yaml             – multi-platform builds + Homebrew tap formula
Makefile                     – build / run / test / lint / tidy / release-dry-run targets
```

## Architecture: DAO → Model → View

g9s mirrors the k9s three-layer pattern, adapted for GCP's poll-based APIs:

```
Key press
  └─ app.showResource(key)       (main goroutine — no blocking, no QueueUpdateDraw)
       ├─ render loading state directly (safe: already on main goroutine)
       ├─ mount tview page
       └─ go model.Table.Watch() (background goroutine)
            └─ refresh loop (every 30 s by default)
                 ├─ cache.Get(resource, project)
                 │    hit  → fire TableDataChanged immediately
                 │    miss → DAO.List() → cache.Set(TTL) → fire TableDataChanged
                 └─ TableListener.TableDataChanged()
                      └─ app.QueueUpdateDraw → ResourceTable.Render(data)
```

### Layer 1 — DAO (`internal/dao/`)

- `Accessor` is the single required interface: `Resource()`, `Header()`, `List(ctx, project)`.
- Optional capability interfaces (`Describer`, etc.) are added per-resource and discovered via runtime type assertion.
- Each DAO owns its own GCP client construction via `gcp.ClientOptions(ctx)` (ADC).
- One file per resource type: `cloudrun.go`, `cloudbuild.go`, future: `gce.go`, `gcs.go`, etc.

### Layer 2 — Model (`internal/model/`)

- `Registry` maps resource keys to `ResourceMeta{DAO, TTL}`. Adding a resource is one line.
- `Table` polls on a configurable interval (default 30 s). On each tick it checks the shared TTL cache first; only on a miss does it call the DAO and hit the GCP API.
- `TableListener` is the observer interface views implement to receive data/error events.

### Cache TTLs (`internal/cache/`)

TTLs are set per resource type in `model/registry.go`:

| Resource       | TTL  | Rationale                                              |
|----------------|------|--------------------------------------------------------|
| `cloudrun`     | 60 s | Services are long-lived; List calls are quota-weighted |
| `cloudbuild`   | 30 s | Triggers change more often; API calls are lightweight  |
| `buildhistory` | 15 s | Builds are transient; users expect near-real-time view |

When adding a new resource, choose a TTL based on change frequency, API quota cost, and user expectation of data freshness.

### Layer 3 — View (`internal/ui/`)

- `ResourceView` is the common interface all resource views implement: `Primitive()`, `Watch(ctx)`, `RenderLoading()`, plus `Filterable` and `TableListener`. Defined in `views.go`.
- `newResourceView()` in `views.go` is the factory that maps a registry key to the correct view constructor.
- `ResourceTable` is a reusable `tview.Table` wrapper with a `Render(*dao.TableData)` method and status-aware row colouring.
- Each resource has its own view struct (e.g. `CloudRunView`, `CloudBuildView`) that embeds `ResourceTable` and implements `ResourceView`.
- `TableDataChanged` and `TableLoadFailed` always dispatch to tview via `QueueUpdateDraw` — they are called from background goroutines.
- `app.showResource(key)` is the single generic routing method — called on the main goroutine, must never call `QueueUpdateDraw` or block.

## Build & Run Commands

```bash
make build          # produces bin/g9s
make run            # go run with version ldflags
make test           # go test ./...
make lint           # golangci-lint run ./...
make tidy           # go mod tidy
make release-dry-run  # goreleaser snapshot
```

## Code Conventions

- All packages live under `internal/`; nothing outside `cmd/` and `main.go` is public API.
- GCP clients are always constructed with `gcp.ClientOptions(ctx)` to ensure ADC is used.
- Log with `zerolog` at the call site — import `github.com/rs/zerolog/log` and use `log.Info().Str("k","v").Msg("…")`.
- `tview` UI mutations **must** happen inside `app.QueueUpdateDraw(func(){…})` when called from goroutines. When already on the main goroutine (e.g. inside an input capture handler), mutate tview primitives directly — calling `QueueUpdateDraw` from the main goroutine deadlocks.
- Cobra sub-commands are registered in their own file under `cmd/` and added to `rootCmd` via `rootCmd.AddCommand(…)` inside an `init()` function.
- Version string is injected at build time via `-ldflags "-X github.com/brekol/g9s/cmd.Version=…"`.

## Configuration

Config file default location: `~/.config/g9s/config.yaml`

Env prefix: `G9S_` (e.g. `G9S_PROJECT=my-project`)

Key flags: `--project`, `--log-level`, `--config`

## Adding a New GCP Resource View

1. Add a DAO in `internal/dao/<resource>.go` implementing `dao.Accessor`. Implement optional interfaces (`Describer`, etc.) as needed.
2. Register the resource in `internal/model/registry.go` with an appropriate TTL and aliases.
3. Add a view in `internal/ui/<resource>.go` embedding `ResourceTable` and implementing `ResourceView`.
4. Add a case to `newResourceView()` in `internal/ui/views.go` — no changes to `app.go` needed.
5. Optionally add a cobra sub-command in `cmd/<resource>.go` for non-interactive use.
