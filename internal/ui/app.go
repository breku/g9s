// Package ui provides the tview-based terminal UI.
package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// App wraps tview.Application with gcptui state.
type App struct {
	tview *tview.Application
	pages *tview.Pages
}

// New creates and initialises a new App.
func New() *App {
	tv := tview.NewApplication()
	pages := tview.NewPages()

	// Root layout: header bar + main content area
	header := tview.NewTextView().
		SetText(" gcptui — GCP Terminal Dashboard ").
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	header.SetBackgroundColor(tcell.ColorDarkBlue)

	help := tview.NewTextView().
		SetText(" <q> quit  <?>  help ").
		SetTextAlign(tview.AlignRight).
		SetDynamicColors(true)
	help.SetBackgroundColor(tcell.ColorDarkBlue)

	headerRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(header, 0, 1, false).
		AddItem(help, 24, 0, false)

	placeholder := tview.NewTextView().
		SetText("\n  Loading resources…").
		SetDynamicColors(true)
	pages.AddPage("main", placeholder, true, true)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(headerRow, 1, 0, false).
		AddItem(pages, 0, 1, true)

	tv.SetRoot(root, true).EnableMouse(true)

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC {
			tv.Stop()
			return nil
		}
		return event
	})

	return &App{tview: tv, pages: pages}
}

// Run starts the blocking event loop.
func (a *App) Run() error {
	return a.tview.Run()
}
