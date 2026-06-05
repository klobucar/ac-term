// Command snapshot renders a few UI frames to stdout (forced truecolor) so the
// layout and animations can be eyeballed without a TTY. Dev-only.
//
// Run with AC_DETERMINISTIC=1 for tracks in theme order.
package main

import (
	"fmt"
	"strings"

	"github.com/klobucar/ac-term/internal/data"
	"github.com/klobucar/ac-term/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func main() {
	lipgloss.SetColorProfile(termenv.TrueColor)

	puzzles := data.All()
	m := ui.New(puzzles, nil, nil)
	feed := func(msg tea.Msg) {
		nm, _ := m.Update(msg)
		m = nm.(ui.Model)
	}

	feed(tea.WindowSizeMsg{Width: 110, Height: 44})
	feed(tea.KeyMsg{Type: tea.KeyEnter}) // start newest day (day 39 has a constraint)

	// Day 39 carries a constraint, so it opens with the DJ note over the deck.
	for i := 0; i < 6; i++ {
		m = m.Frame()
	}
	fmt.Println("===== DJ NOTE: card composited over the tapes =====")
	fmt.Println(m.View())
	feed(tea.KeyMsg{Type: tea.KeyEnter}) // confirm the note away

	fmt.Println("\n===== RESTING: factory labels covering every tape =====")
	fmt.Println(m.View())

	// Peel the cursor tile, then peel-and-reveal a couple more; write a label.
	feed(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // peel tile 0
	for i := 0; i < 3; i++ {                                 // mid-peel frame
		m = m.Frame()
	}
	fmt.Println("\n===== PEELING: tile0 label lifting, song emerging =====")
	fmt.Println(m.View())

	for i := 0; i < peelFinish; i++ {
		m = m.Frame()
	}
	feed(tea.KeyMsg{Type: tea.KeyRight})                     // -> tile 1
	feed(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // peel tile 1 fully
	for i := 0; i < peelFinish; i++ {
		m = m.Frame()
	}
	feed(tea.KeyMsg{Type: tea.KeyRight})                     // -> tile 2
	feed(tea.KeyMsg{Type: tea.KeySpace})                     // cue tile 2
	feed(tea.KeyMsg{Type: tea.KeyRight})                     // -> tile 3
	feed(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}) // write a label
	for _, r := range "synthy?" {
		feed(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	feed(tea.KeyMsg{Type: tea.KeyEnter})

	fmt.Println("\n===== MIXED: t0/t1 revealed, t2 cued, t3 hand-labelled =====")
	fmt.Println(m.View())

	// Spinning reels: tile 0 (track id 0 under AC_DETERMINISTIC) now playing.
	m = m.DebugPlay(0)
	fmt.Println("\n===== PLAYING: tile0 reels spinning across 4 frames =====")
	for f := 0; f < 4; f++ {
		fmt.Printf("frame %d: %s\n", f, firstReel(m.View()))
		m = m.Frame()
	}

	// Too-small guard.
	feed(tea.WindowSizeMsg{Width: 70, Height: 18})
	fmt.Println("\n===== TOO SMALL (70x18): guard screen =====")
	fmt.Println(m.View())
}

// firstReel pulls the first tape-reel line out of a rendered view for a tidy
// side-by-side of the spin frames.
func firstReel(view string) string {
	for _, ln := range strings.Split(view, "\n") {
		if strings.ContainsAny(ln, "▁▂▃▄▅▆▇█") {
			return strings.TrimSpace(ln)
		}
	}
	return "(no reel found)"
}

const peelFinish = 8
