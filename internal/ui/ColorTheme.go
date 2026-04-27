package ui

import "github.com/gdamore/tcell/v2"

type ColorTheme struct {
	BackgroundColor        tcell.Color
	BorderColor            tcell.Color
	TableColumnHeaderColor tcell.Color
	TableTitleColor        tcell.Color
}

var AppTheme = ColorTheme{
	BackgroundColor:        tcell.ColorBlack,
	BorderColor:            tcell.ColorTurquoise,
	TableColumnHeaderColor: tcell.ColorWhite,
	TableTitleColor:        tcell.ColorWhite,
}
