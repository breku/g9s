// Package ui provides the tview-based terminal UI.
package ui

import (
	"context"

	"github.com/brekol/g9s/internal/config"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// App wraps tview.Application with g9s state.
type App struct {
	tview       *tview.Application
	pages       *tview.Pages
	root        *tview.Flex
	header      *Header
	cmdbar      *CmdBar
	statusbar   *StatusBar
	cmdbarShown bool
	cfg         *config.Config
	ctx         context.Context
	cancel      context.CancelFunc

	// activeView is the currently displayed resource view.
	// Used to forward filter keystrokes and optional key bindings.
	activeView ResourceView

	// activeOverlay is the overlay currently on top, if any.
	// Used for hint rendering; nil when no overlay is shown.
	activeOverlay Overlay

	// viewCache stores ResourceView instances by resource key so that
	// navigating back to an already-mounted page restores the correct view.
	viewCache map[string]ResourceView

	// headerShown tracks whether the header is currently in the root layout.
	headerShown bool
}

// New creates and initialises a new App.
func New(cfg *config.Config) *App {
	tv := tview.NewApplication()
	pages := tview.NewPages()
	ctx, cancel := context.WithCancel(context.Background())

	cmdbar := NewCmdBar()
	header := NewHeader(cfg.Project)

	a := &App{
		tview:     tv,
		pages:     pages,
		header:    header,
		cmdbar:    cmdbar,
		cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
		viewCache: make(map[string]ResourceView),
	}
	a.statusbar = NewStatusBar(a)

	// Wire cmdbar callbacks — all called on the main goroutine by tview.
	cmdbar.OnCommand(a.handleCommand)
	cmdbar.OnFilter(a.handleFilter)
	cmdbar.OnDismiss(a.hideCmdBar)

	pages.AddPage("home", NewWelcomeView(), true, true)

	// Root layout is built fresh by relayout() on every visibility change so
	// the row order is always: header? cmdbar? pages statusbar.
	a.root = tview.NewFlex().SetDirection(tview.FlexRow)
	a.relayout()

	tv.SetRoot(a.root, true).EnableMouse(true)

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Let the cmdbar consume all input when it is shown.
		// We use a.cmdbarShown rather than cmdbar.HasFocus() because in
		// recent tview versions an InputField removed from the layout can
		// still report focus, which would permanently swallow ':' and '/'.
		if a.cmdbarShown {
			return event
		}

		switch event.Rune() {
		case 'q':
			a.stop()
			return nil
		case ':':
			a.showCmdBar(modeCommand)
			return nil
		case '/':
			a.showCmdBar(modeFilter)
			return nil
		}
		if event.Key() == tcell.KeyCtrlC {
			a.stop()
			return nil
		}
		// Generic cross-resource keys (d, y, c) handled centrally so each
		// view doesn't reimplement them. Runs before the view's own
		// KeyHandler so views can't accidentally shadow them.
		if a.activeOverlay == nil && a.activeView != nil {
			if handleGenericKey(a, a.activeView, event) {
				return nil
			}
		}
		// Forward to the active view's KeyHandler if it implements one.
		if a.activeView != nil {
			if kh, ok := a.activeView.(KeyHandler); ok {
				if kh.HandleKey(event) {
					return nil
				}
			}
		}
		return event
	})

	return a
}

// relayout rebuilds the root flex from scratch in canonical row order:
//
//	[header (5)]  [cmdbar (3)]  pages (flex)  statusbar (1)
//
// Header and cmdbar are conditional. Calling relayout after toggling
// a.headerShown / a.cmdbarShown is the single source of truth for what's
// on screen, so individual show/hide methods don't have to coordinate
// row ordering. Must be called on the tview main goroutine.
func (a *App) relayout() {
	a.root.Clear()
	if a.headerShown {
		a.root.AddItem(a.header, 5, 0, false)
	}
	if a.cmdbarShown {
		a.root.AddItem(a.cmdbar, 3, 0, false)
	}
	a.root.AddItem(a.pages, 0, 1, true)
	a.root.AddItem(a.statusbar, statusBarHeight, 0, false)
}

// Status posts a message to the app-wide status bar at the given level.
// Safe to call from any goroutine. The full message is also written to the
// log file at the matching zerolog level so long messages remain reviewable.
func (a *App) Status(level StatusLevel, msg string) {
	a.statusbar.Set(level, msg)
}

// runOnUI schedules fn to run on the tview main goroutine. Always dispatches
// asynchronously via a helper goroutine + QueueUpdateDraw so it is safe to
// call both from the main goroutine (where a synchronous QueueUpdateDraw
// would deadlock — the loop can't drain its own queue while the caller
// blocks it) and from background goroutines.
func (a *App) runOnUI(fn func()) {
	go a.tview.QueueUpdateDraw(fn)
}

// TrackOp runs fn on the App context (NOT a view context, so the operation
// outlives view switches), reporting progress to the status bar:
//
//   - On start: "<name>… (running)" at Info level.
//   - On success: "<name> succeeded" at Success level.
//   - On error: "<name> failed: <err>" at Error level (sticky on the bar;
//     full error in the log file).
//
// Returns immediately; fn runs in a goroutine. Use this for any user-initiated
// action whose outcome the user cares about (deploy, delete, cancel, trigger).
func (a *App) TrackOp(name string, fn func(ctx context.Context) error) {
	a.Status(StatusInfo, name+"… (running)")
	go func() {
		if err := fn(a.ctx); err != nil {
			a.Status(StatusError, name+" failed: "+err.Error())
			return
		}
		a.Status(StatusSuccess, name+" succeeded")
	}()
}

// showHeader inserts the header into the root layout above pages.
// No-op if already shown. Must be called on the tview main goroutine.
func (a *App) showHeader() {
	if a.headerShown {
		return
	}
	a.headerShown = true
	a.relayout()
}

// hideHeader removes the header from the root layout.
// No-op if already hidden. Must be called on the tview main goroutine.
func (a *App) hideHeader() {
	if !a.headerShown {
		return
	}
	a.headerShown = false
	a.relayout()
}

// Run starts the blocking event loop.
func (a *App) Run() error {
	return a.tview.Run()
}

// stop cancels background goroutines and stops tview.
func (a *App) stop() {
	a.cancel()
	a.tview.Stop()
}

// showCmdBar inserts the command bar into the layout, sets its mode, and
// focuses it. Safe to call if already shown.
func (a *App) showCmdBar(mode cmdMode) {
	if mode == modeCommand {
		a.cmdbar.ActivateCommand()
	} else {
		a.cmdbar.ActivateFilter()
	}
	if !a.cmdbarShown {
		a.cmdbarShown = true
		a.relayout()
	}
	a.tview.SetFocus(a.cmdbar.InputField)
}

// hideCmdBar removes the command bar from the layout and returns focus to pages.
func (a *App) hideCmdBar() {
	if !a.cmdbarShown {
		return
	}
	// Transfer focus to pages BEFORE removing the cmdbar from the layout.
	// Application.SetFocus walks the focus chain to find the currently
	// focused primitive (so it can blur it and call screen.HideCursor).
	// If we remove the cmdbar first, it's no longer reachable from root,
	// the blur chain comes up empty, and the cursor stays painted from
	// the cmdbar's last draw.
	a.tview.SetFocus(a.pages)
	a.cmdbarShown = false
	a.relayout()
}

// handleCommand is called when the user submits a ':' command.
func (a *App) handleCommand(text string) {
	if text == "q" || text == "quit" {
		a.stop()
		return
	}
	meta, ok := model.Resolve(text)
	if !ok {
		log.Warn().Str("cmd", text).Msg("unknown resource command")
		return
	}
	a.showResource(meta.DAO.Resource())
}

// handleFilter is called on every keystroke in '/' mode.
// If an overlay is active and implements Filterable, the filter is forwarded
// there; otherwise it is forwarded to the active resource view.
func (a *App) handleFilter(text string) {
	if a.activeOverlay != nil {
		if f, ok := a.activeOverlay.(Filterable); ok {
			f.SetFilter(text)
			return
		}
	}
	if a.activeView != nil {
		a.activeView.SetFilter(text)
	}
}

// PushOverlay mounts an Overlay on top of the current page, updates hints,
// disables mouse, and starts the overlay's background work.
// Safe to call on the tview main goroutine.
func (a *App) PushOverlay(o Overlay) {
	const overlayPage = "overlay"
	o.OnClose(func() { a.PopOverlay() })
	a.activeOverlay = o
	a.tview.EnableMouse(false)
	a.pages.AddPage(overlayPage, o.Primitive(), true, true)
	a.tview.SetFocus(o.Primitive())
	o.RenderLoading()
	if hp, ok := o.(HintProvider); ok {
		a.header.SetViewHints(hp)
	} else {
		a.header.SetViewHints(nil)
	}
	go o.Start(a.ctx)
}

// PopOverlay removes the overlay, restores mouse + focus, and reverts hints
// to the active resource view.
func (a *App) PopOverlay() {
	const overlayPage = "overlay"
	a.activeOverlay = nil
	a.pages.RemovePage(overlayPage)
	a.tview.SetFocus(a.pages)
	a.tview.EnableMouse(true)
	a.header.SetViewHints(viewHintProvider(a.activeView))
}

// showResource navigates to the view for the given resource key, creating it
// on first call. Stops the previously active view's polling so background
// fetches don't accumulate as the user moves between resources, and
// (re)starts the new view's polling — re-entrant Watch handles the
// hand-off. Switch-back triggers a fresh fetch and a brief "Loading…"
// frame; the page itself (selection, scroll position) is preserved.
//
// Called on the main goroutine — must not block.
func (a *App) showResource(resource string) {
	a.showHeader()

	if a.cfg.Project == "" {
		log.Warn().Msg("no project configured; set --project or G9S_PROJECT")
		tv := tview.NewTextView().
			SetText("\n  [red]No GCP project set.[white] Use --project flag or G9S_PROJECT env var.").
			SetDynamicColors(true)
		a.pages.AddAndSwitchToPage(resource, tv, true)
		if a.activeView != nil {
			a.activeView.Stop()
		}
		a.activeView = nil
		a.header.SetViewHints(nil)
		return
	}

	// Stop the previous view's poller before switching. The page is kept in
	// the pages cache so the table state (selection, scroll) is preserved;
	// only the background fetch loop is paused.
	if a.activeView != nil {
		a.activeView.Stop()
	}

	if a.pages.HasPage(resource) {
		a.pages.SwitchToPage(resource)
		a.activeView = a.viewCache[resource]
		a.header.SetViewHints(viewHintProvider(a.activeView))
		// Resume polling with a fresh ctx; the cache will serve the last
		// known data instantly while a background revalidate runs if the
		// entry is stale.
		go func() {
			if err := a.activeView.Watch(a.ctx); err != nil {
				log.Error().Err(err).Str("resource", resource).Msg("resume watch failed")
			}
		}()
		return
	}

	view := newResourceView(a, resource, a.cfg.Project)
	if view == nil {
		log.Warn().Str("resource", resource).Msg("no view registered")
		return
	}

	a.viewCache[resource] = view
	a.activeView = view

	view.RenderLoading()
	a.pages.AddAndSwitchToPage(resource, view.Primitive(), true)
	a.header.SetViewHints(viewHintProvider(view))

	go func() {
		if err := view.Watch(a.ctx); err != nil {
			log.Error().Err(err).Str("resource", resource).Msg("initial load failed")
		}
	}()
}
