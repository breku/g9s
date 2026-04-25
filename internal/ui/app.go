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
	cmdbar      *CmdBar
	cmdbarShown bool
	cfg         *config.Config
	ctx         context.Context
	cancel      context.CancelFunc

	// activeView is the currently displayed resource view, if it implements
	// Filterable. Used to forward filter keystrokes.
	activeView Filterable
}

// New creates and initialises a new App.
func New(cfg *config.Config) *App {
	tv := tview.NewApplication()
	pages := tview.NewPages()
	ctx, cancel := context.WithCancel(context.Background())

	cmdbar := NewCmdBar()

	a := &App{
		tview:  tv,
		pages:  pages,
		cmdbar: cmdbar,
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	// Wire cmdbar callbacks — all called on the main goroutine by tview.
	cmdbar.OnCommand(a.handleCommand)
	cmdbar.OnFilter(a.handleFilter)
	cmdbar.OnDismiss(a.hideCmdBar)

	// Root layout: header | pages
	// The cmdbar row is inserted/removed dynamically between header and pages.
	project := cfg.Project
	if project == "" {
		project = "[red](no project set)"
	}

	header := tview.NewTextView().
		SetText(" [white]g9s[darkgray] │ [yellow]project:[white] " + project + " ").
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	header.SetBackgroundColor(tcell.ColorDarkBlue)

	help := tview.NewTextView().
		SetText("  <:> command  </> filter  <q> quit ").
		SetTextAlign(tview.AlignRight).
		SetDynamicColors(true)
	help.SetBackgroundColor(tcell.ColorDarkBlue)

	headerRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(header, 0, 1, false).
		AddItem(help, 38, 0, false)

	placeholder := tview.NewTextView().
		SetText("\n  Press [yellow]:[white] and type a resource name (e.g. [yellow]run[white]) to navigate.\n  Press [yellow]/[white] to filter the active view.").
		SetDynamicColors(true)
	pages.AddPage("home", placeholder, true, true)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(headerRow, 1, 0, false).
		AddItem(pages, 0, 1, true)

	a.root = root

	tv.SetRoot(root, true).EnableMouse(true)

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
		return event
	})

	return a
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
// It resolves the input through the model alias table so routing and
// autocomplete always stay in sync.
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
// It forwards the filter string to the active view if it supports filtering.
func (a *App) handleFilter(text string) {
	if a.activeView != nil {
		a.activeView.SetFilter(text)
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
		return
	}

	if a.pages.HasPage(resource) {
		a.pages.SwitchToPage(resource)
		return
	}

	view := newResourceView(a, resource, a.cfg.Project)
	if view == nil {
		log.Warn().Str("resource", resource).Msg("no view registered")
		return
	}

	view.RenderLoading()
	a.pages.AddAndSwitchToPage(resource, view.Primitive(), true)
	a.activeView = view

	go func() {
		if err := view.Watch(a.ctx); err != nil {
			log.Error().Err(err).Str("resource", resource).Msg("initial load failed")
		}
	}()
}
