package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// composite overlays fg onto bg starting at cell (x, y), ANSI-aware so the
// colored cells behind fg are sliced on visual-width boundaries rather than
// byte offsets. fg is treated as opaque — wherever it has cells, they replace
// what's behind. Rows/cols outside bg are clipped.
func composite(bg, fg string, x, y int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	for i, fl := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		line := bgLines[row]
		lineW := ansi.StringWidth(line)
		fw := ansi.StringWidth(fl)

		left := ansi.Cut(line, 0, x)
		if w := ansi.StringWidth(left); w < x {
			left += strings.Repeat(" ", x-w) // pad short background rows
		}
		var right string
		if x+fw < lineW {
			right = ansi.Cut(line, x+fw, lineW)
		}
		// Reset between segments so neither side bleeds its SGR into the other.
		bgLines[row] = left + "\x1b[0m" + fl + "\x1b[0m" + right
	}
	return strings.Join(bgLines, "\n")
}

// centerOver composites fg centered over bg.
func centerOver(bg, fg string) string {
	x := (lipgloss.Width(bg) - lipgloss.Width(fg)) / 2
	y := (lipgloss.Height(bg) - lipgloss.Height(fg)) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return composite(bg, fg, x, y)
}

// noteCardWidth picks a card width that fits the current terminal.
func (m Model) noteCardWidth() int {
	w := 54
	if m.width > 0 && m.width-6 < w {
		w = m.width - 6
	}
	if w < 28 {
		w = 28
	}
	return w
}

// renderNoteCard is the "DJ left a note" modal shown over the deck when a puzzle
// carries a constraint. Border hue drifts and a tape waveform runs underneath,
// so the card feels alive while it waits for you to confirm it away. The card
// keeps a single uniform background (the terminal's) — only the border and the
// gradient text carry color, so there's no mismatched band behind the lines.
func (m Model) renderNoteCard() string {
	// inner is the content width; the box auto-sizes around it (no explicit
	// Width, which in lipgloss v2 would include the border and wrap the lines).
	inner := m.noteCardWidth() - 4

	hue := float64(m.frame * 2) // slow spectral drift
	a := hsl(hue, 0.85, 0.62)
	b := hsl(hue+50, 0.85, 0.62)

	center := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center)

	titleRow := center.Render(gradientText("♫  THE DJ LEFT A NOTE", a, b))
	note := center.Foreground(colInk).Bold(true).Render("“" + m.puzzle.Constraint + "”")
	wave := reelBar(7, m.frame, inner, true)
	hint := center.Render(
		helpStyle.Render("press ") + keyStyle.Render("enter") + helpStyle.Render(" to drop the needle"))

	body := titleRow + "\n\n" + note + "\n\n" + wave + "\n" + hint

	return lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.DoubleBorder()).
		BorderForeground(a.color()).
		Render(body)
}

// renderEraseCard is the "erase tape?" confirmation shown over the deck. Hot
// (red) border, since it discards a saved result.
func (m Model) renderEraseCard() string {
	inner := m.noteCardWidth() - 4
	center := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center)

	title := center.Foreground(colHot).Bold(true).Render("🧲  ERASE TAPE")
	body := center.Foreground(colInk).Render("Wipe this tape's recording and\nreplay " + m.puzzleName() + " from scratch?")
	choice := center.Render(
		keyStyle.Render("y") + helpStyle.Render(" erase    ") +
			keyStyle.Render("n") + helpStyle.Render(" keep"))

	return lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.DoubleBorder()).
		BorderForeground(colHot).
		Render(title + "\n\n" + body + "\n\n" + choice)
}
