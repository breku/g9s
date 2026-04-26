// Package ui provides the tview-based terminal UI.
package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/brekol/g9s/internal/config"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// globalHints are always shown in the middle column regardless of which view
// is active.
var globalHints = []Hint{
	{Key: ":", Desc: "command"},
	{Key: "/", Desc: "filter"},
	{Key: "q", Desc: "quit"},
}

// App wraps tview.Application with g9s state.
type App struct {
	tview       *tview.Application
	pages       *tview.Pages
	root        *tview.Flex
	cmdbar      *CmdBar
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

	// viewHintsView is the third header column showing per-view key hints.
	viewHintsView *tview.TextView
}

// New creates and initialises a new App.
func New(cfg *config.Config) *App {
	tv := tview.NewApplication()
	pages := tview.NewPages()
	ctx, cancel := context.WithCancel(context.Background())

	cmdbar := NewCmdBar()

	// Column 1 — project info, top-aligned.
	project := cfg.Project
	if project == "" {
		project = "[red](no project set)"
	}
	projectView := tview.NewTextView().
		SetText(" [white]g9s[darkgray] │ [yellow]project:[white] " + project).
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	projectView.SetBackgroundColor(tcell.ColorDefault)

	// Column 2 — global hints, static, one per line.
	var globalBuf strings.Builder
	for _, h := range globalHints {
		fmt.Fprintf(&globalBuf, " [yellow]<%s>[white] %s\n", h.Key, h.Desc)
	}
	globalHintsView := tview.NewTextView().
		SetText(globalBuf.String()).
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	globalHintsView.SetBackgroundColor(tcell.ColorDefault)

	// Column 3 — per-view hints, dynamic, one per line.
	viewHintsView := tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	viewHintsView.SetBackgroundColor(tcell.ColorDefault)

	headerRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(projectView, 0, 2, false).
		AddItem(globalHintsView, 20, 0, false).
		AddItem(viewHintsView, 0, 2, false)

	a := &App{
		tview:         tv,
		pages:         pages,
		cmdbar:        cmdbar,
		cfg:           cfg,
		ctx:           ctx,
		cancel:        cancel,
		viewCache:     make(map[string]ResourceView),
		viewHintsView: viewHintsView,
	}

	// Wire cmdbar callbacks — all called on the main goroutine by tview.
	cmdbar.OnCommand(a.handleCommand)
	cmdbar.OnFilter(a.handleFilter)
	cmdbar.OnDismiss(a.hideCmdBar)

	placeholder := tview.NewTextView().
		SetText("\n  Press [yellow]:[white] and type a resource name (e.g. [yellow]run[white]) to navigate.\n  Press [yellow]/[white] to filter the active view.").
		SetDynamicColors(true)
	pages.AddPage("home", placeholder, true, true)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(headerRow, 3, 0, false).
		AddItem(pages, 0, 1, true)

	a.root = root

	tv.SetRoot(root, true).EnableMouse(true)

	// No active view yet — clear the view hints column.
	a.updateHints(nil)

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Let the cmdbar consume all input when it is focused.
		if cmdbar.HasFocus() {
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

// updateHints sets the per-view hints column. Pass nil to clear it.
// Must be called on the tview main goroutine.
func (a *App) updateHints(hp HintProvider) {
	if hp == nil {
		a.viewHintsView.SetText("")
		return
	}
	var b strings.Builder
	for _, h := range hp.Hints() {
		fmt.Fprintf(&b, " [yellow]<%s>[white] %s\n", h.Key, h.Desc)
	}
	a.viewHintsView.SetText(b.String())
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

// showCmdBar inserts the command bar into the layout at row 1 (below the
// header), sets its mode, and focuses it. Safe to call if already shown.
func (a *App) showCmdBar(mode cmdMode) {
	if mode == modeCommand {
		a.cmdbar.ActivateCommand()
	} else {
		a.cmdbar.ActivateFilter()
	}
	if !a.cmdbarShown {
		// Insert between header (index 0) and pages (index 1).
		// RemoveItem+re-add pages so the cmdbar sits at index 1.
		a.root.RemoveItem(a.pages)
		a.root.AddItem(a.cmdbar, 3, 0, false)
		a.root.AddItem(a.pages, 0, 1, false)
		a.cmdbarShown = true
	}
	a.tview.SetFocus(a.cmdbar.InputField)
}

// hideCmdBar removes the command bar from the layout and returns focus to pages.
func (a *App) hideCmdBar() {
	if a.cmdbarShown {
		a.root.RemoveItem(a.cmdbar)
		a.cmdbarShown = false
	}
	a.tview.SetFocus(a.pages)
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
func (a *App) handleFilter(text string) {
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
		a.updateHints(hp)
	} else {
		a.updateHints(nil)
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
	if hp, ok := a.activeView.(HintProvider); ok {
		a.updateHints(hp)
	} else {
		a.updateHints(nil)
	}
}

// showResource navigates to the view for the given resource key, creating it
// on first call. Called on the main goroutine — must not block.
func (a *App) showResource(resource string) {
	if a.cfg.Project == "" {
		log.Warn().Msg("no project configured; set --project or G9S_PROJECT")
		tv := tview.NewTextView().
			SetText("\n  [red]No GCP project set.[white] Use --project flag or G9S_PROJECT env var.").
			SetDynamicColors(true)
		a.pages.AddAndSwitchToPage(resource, tv, true)
		a.activeView = nil
		a.updateHints(nil)
		return
	}

	if a.pages.HasPage(resource) {
		a.pages.SwitchToPage(resource)
		a.activeView = a.viewCache[resource]
		if hp, ok := a.activeView.(HintProvider); ok {
			a.updateHints(hp)
		} else {
			a.updateHints(nil)
		}
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
	if hp, ok := view.(HintProvider); ok {
		a.updateHints(hp)
	} else {
		a.updateHints(nil)
	}

	go func() {
		if err := view.Watch(a.ctx); err != nil {
			log.Error().Err(err).Str("resource", resource).Msg("initial load failed")
		}
	}()
}
