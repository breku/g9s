// Package ui provides the tview-based terminal UI.
package ui

import (
	"context"

	"github.com/brekol/g9s/internal/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// App wraps tview.Application with g9s state.
type App struct {
	tview  *tview.Application
	pages  *tview.Pages
	cfg    *config.Config
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates and initialises a new App.
func New(cfg *config.Config) *App {
	tv := tview.NewApplication()
	pages := tview.NewPages()

	ctx, cancel := context.WithCancel(context.Background())

	a := &App{
		tview:  tv,
		pages:  pages,
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	// Root layout: header bar + main content area
	header := tview.NewTextView().
		SetText(" g9s — GCP Terminal Dashboard ").
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	header.SetBackgroundColor(tcell.ColorDarkBlue)

	help := tview.NewTextView().
		SetText(" <q> quit  <r> Cloud Run ").
		SetTextAlign(tview.AlignRight).
		SetDynamicColors(true)
	help.SetBackgroundColor(tcell.ColorDarkBlue)

	headerRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(header, 0, 1, false).
		AddItem(help, 30, 0, false)

	placeholder := tview.NewTextView().
		SetText("\n  Press <r> to view Cloud Run services.").
		SetDynamicColors(true)
	pages.AddPage("home", placeholder, true, true)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(headerRow, 1, 0, false).
		AddItem(pages, 0, 1, true)

	tv.SetRoot(root, true).EnableMouse(true)

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC:
			a.stop()
			return nil
		case event.Rune() == 'r':
			a.showCloudRun()
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

// showCloudRun navigates to the Cloud Run view, creating it on first call.
// Called from the tview input handler (main goroutine) — must not block.
func (a *App) showCloudRun() {
	const pageName = "cloudrun"

	if a.cfg.Project == "" {
		log.Warn().Msg("no project configured; set --project or G9S_PROJECT")
		tv := tview.NewTextView().
			SetText("\n  [red]No GCP project set.[white] Use --project flag or G9S_PROJECT env var.").
			SetDynamicColors(true)
		a.pages.AddAndSwitchToPage(pageName, tv, true)
		return
	}

	// If the page already exists just switch to it.
	if a.pages.HasPage(pageName) {
		a.pages.SwitchToPage(pageName)
		return
	}

	view := NewCloudRunView(a, a.cfg.Project)
	// Show the loading state immediately — we are on the main goroutine,
	// so direct tview mutation is safe (no QueueUpdateDraw needed).
	view.renderLoading()
	a.pages.AddAndSwitchToPage(pageName, view.Table, true)

	// Start polling in a background goroutine so we never block the UI loop.
	go func() {
		if err := view.model.Watch(a.ctx); err != nil {
			log.Error().Err(err).Msg("cloud run initial load failed")
		}
	}()
}
