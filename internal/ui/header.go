package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// globalHints are always shown in the middle column regardless of which view
// is active.
var globalHints = []Hint{
	{Key: ":", Desc: "command"},
	{Key: "/", Desc: "filter"},
	{Key: "q", Desc: "quit"},
}

// Header is the 3-row, 3-column bar at the top of the app.
// Col 1: project info (static, top-aligned).
// Col 2: global key hints (static).
// Col 3: per-view key hints (dynamic, updated via SetViewHints).
type Header struct {
	*tview.Flex
	viewHintsView *tview.TextView
}

// NewHeader creates the header bar for the given project string.
func NewHeader(project string) *Header {
	if project == "" {
		project = "[red](no project set)"
	}

	// Column 1 — project info.
	projectView := tview.NewTextView().
		SetText(" [white]g9s[darkgray] │ [yellow]project:[white] " + project).
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	projectView.SetBackgroundColor(tcell.ColorDefault)

	// Column 2 — global hints, static.
	var globalBuf strings.Builder
	for _, h := range globalHints {
		fmt.Fprintf(&globalBuf, " [yellow]<%s>[white] %s\n", h.Key, h.Desc)
	}
	globalHintsView := tview.NewTextView().
		SetText(globalBuf.String()).
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	globalHintsView.SetBackgroundColor(tcell.ColorDefault)

	// Column 3 — per-view hints, dynamic.
	viewHintsView := tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	viewHintsView.SetBackgroundColor(tcell.ColorDefault)

	flex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(projectView, 0, 2, false).
		AddItem(globalHintsView, 20, 0, false).
		AddItem(viewHintsView, 0, 2, false)

	return &Header{
		Flex:          flex,
		viewHintsView: viewHintsView,
	}
}

// SetViewHints updates the per-view hints column.
// Pass nil to clear it. Must be called on the tview main goroutine.
func (h *Header) SetViewHints(hp HintProvider) {
	if hp == nil {
		h.viewHintsView.SetText("")
		return
	}
	var b strings.Builder
	for _, hint := range hp.Hints() {
		fmt.Fprintf(&b, " [yellow]<%s>[white] %s\n", hint.Key, hint.Desc)
	}
	h.viewHintsView.SetText(b.String())
}
