package ui

import "github.com/gdamore/tcell/v2"

type ColorTheme struct {
	BackgroundColor        tcell.Color
	HighlightColor         tcell.Color
	TableColumnHeaderColor tcell.Color
	TableTitleColor        tcell.Color
}

var AppTheme = ColorTheme{
	BackgroundColor:        tcell.ColorBlack,
	HighlightColor:         tcell.ColorTurquoise,
	TableColumnHeaderColor: tcell.ColorWhite,
	TableTitleColor:        tcell.ColorWhite,
}
