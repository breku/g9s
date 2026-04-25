# g9s

A terminal dashboard for Google Cloud Platform resources, inspired by k9s.

## Tech Stack

| Concern       | Library / Tool                                   |
|---------------|--------------------------------------------------|
| Language      | Go 1.22                                          |
| TUI framework | `github.com/rivo/tview` on top of `tcell/v2`     |
| GCP client    | `cloud.google.com/go` (per-service sub-modules)  |
| Auth          | Application Default Credentials via `golang.org/x/oauth2/google` |
| Config        | `github.com/spf13/viper` + `github.com/spf13/cobra` |
| Logging       | `github.com/rs/zerolog` (structured, stderr)     |
| Build/Release | `goreleaser` + Homebrew tap (`brekol/homebrew-tap`) |

## Repository Layout

```
main.go                  – entrypoint, calls cmd.Execute()
cmd/
  root.go                – cobra root command, flag wiring, viper/logger init
  version.go             – version sub-command
internal/
  config/config.go       – typed config struct, viper.Unmarshal
  gcp/client.go          – ADC credential helper, shared option.ClientOption slice
  ui/app.go              – tview.Application wrapper, root layout, key bindings
.goreleaser.yaml         – multi-platform builds + Homebrew tap formula
Makefile                 – build / run / test / lint / tidy / release-dry-run targets
```

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
- `tview` UI mutations **must** happen inside `app.QueueUpdateDraw(func(){…})` when called from goroutines.
- Cobra sub-commands are registered in their own file under `cmd/` and added to `rootCmd` via `rootCmd.AddCommand(…)` inside an `init()` function.
- Version string is injected at build time via `-ldflags "-X github.com/brekol/g9s/cmd.Version=…"`.

## Configuration

Config file default location: `~/.config/gcptui/config.yaml`

Env prefix: `GCPTUI_` (e.g. `GCPTUI_PROJECT=my-project`)

Key flags: `--project`, `--log-level`, `--config`

## Adding a New GCP Resource View

1. Add a GCP client wrapper in `internal/gcp/<resource>.go`.
2. Add a tview page/panel in `internal/ui/<resource>.go`.
3. Register a navigation key in `internal/ui/app.go`.
4. Optionally add a cobra sub-command in `cmd/<resource>.go` for non-interactive use.
