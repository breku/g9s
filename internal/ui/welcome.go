package ui

import (
	"github.com/rivo/tview"
)

// ascii art for "g9s" — 6 rows tall, each row same width.
var G9sAscii = `
┏━╸┏━┓┏━┓
┃╺┓┗━┫┗━┓
┗━┛┗━┛┗━┛
`

// welcomeHint is shown below the logo.
const welcomeHint = "\n" +
	"Press [yellow]:[white] to open a resource\n" +
	"Press [yellow]/[white] to filter the active view"

// WelcomeView is the placeholder shown before any resource is navigated to.
// The g9s logo and hint text are centred horizontally and vertically.
type WelcomeView struct {
	*tview.Flex
}

// NewWelcomeView creates the welcome screen with centred ASCII art.
func NewWelcomeView() *WelcomeView {
	logo := tview.NewTextView().
		SetText("[green]" + G9sAscii).
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	logo.SetBackgroundColor(AppTheme.BackgroundColor)

	hint := tview.NewTextView().
		SetText(welcomeHint).
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	hint.SetBackgroundColor(AppTheme.BackgroundColor)

	// Inner flex: logo (6 lines) + hint (3 lines) = 9 lines total.
	inner := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(logo, 5, 0, false).
		AddItem(hint, 5, 0, false)
	inner.SetBackgroundColor(AppTheme.BackgroundColor)

	// Outer flex: vertical spacers sandwich the inner block for vertical centering.
	outer := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 0, 1, false).
		AddItem(inner, 10, 0, false).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 0, 1, false)
	outer.SetBackgroundColor(AppTheme.BackgroundColor)

	return &WelcomeView{Flex: outer}
}
