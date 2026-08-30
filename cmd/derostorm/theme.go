package main

// The theme table lives in internal/ui, because every widget in the new
// console is drawn there and a widget package that has to be handed its own
// palette by the caller is not a widget package.
//
// What is left here is the vocabulary the rest of this program already speaks:
// a Theme type, a themes map and PickTheme. They are aliases rather than
// wrappers so that a *Theme obtained here and a *ui.Theme obtained there are
// the same pointer to the same value, and neither side has to convert.

import "github.com/notoriousjoshyb/derostorm/internal/ui"

// Theme is ui.Theme. Aliased, not redefined: a defined type would need a
// conversion at every call into the widget package.
type Theme = ui.Theme

var themes = ui.Themes

func themeNames() []string { return ui.ThemeNames() }

// PickTheme resolves the --theme value against the environment. It returns the
// theme actually used and, when it had to override the request, why.
func PickTheme(requested string, stdoutIsTTY bool) (*Theme, string) {
	return ui.PickTheme(requested, stdoutIsTTY)
}
