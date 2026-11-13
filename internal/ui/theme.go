// SPDX-License-Identifier: MPL-2.0

package ui

import "github.com/gdamore/tcell/v2"

// commandTheme keeps colour choices semantic so the UI can stay legible on
// terminals with a limited colour palette as well as true-colour terminals.
var commandTheme = struct {
	Background tcell.Color
	Surface    tcell.Color
	Panel      tcell.Color
	Border     tcell.Color
	Text       tcell.Color
	Muted      tcell.Color
	Focus      tcell.Color
	Success    tcell.Color
	Warning    tcell.Color
	Danger     tcell.Color
}{
	Background: tcell.ColorBlack,
	Surface:    tcell.NewRGBColor(8, 18, 31),
	Panel:      tcell.NewRGBColor(14, 31, 48),
	Border:     tcell.NewRGBColor(35, 70, 92),
	Text:       tcell.NewRGBColor(225, 241, 255),
	Muted:      tcell.NewRGBColor(123, 153, 173),
	Focus:      tcell.NewRGBColor(72, 202, 231),
	Success:    tcell.NewRGBColor(103, 221, 177),
	Warning:    tcell.NewRGBColor(237, 190, 95),
	Danger:     tcell.NewRGBColor(240, 132, 132),
}
