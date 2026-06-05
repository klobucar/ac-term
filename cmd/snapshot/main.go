// Command snapshot renders a few UI frames to stdout so the layout and
// animations can be eyeballed without a TTY. lipgloss v2 emits truecolor by
// default, so no profile setup is needed. Dev-only.
//
// Run with AC_DETERMINISTIC=1 for tracks in theme order.
package main

import (
	"fmt"
	"strings"

	"github.com/klobucar/ac-term/internal/data"
	"github.com/klobucar/ac-term/internal/ui"

	tea "charm.land/bubbletea/v2"
)

const peelFinish = 8

// key builds a key-press message; one-rune strings become a text key.
func key(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: text}
}

func main() {
	m := ui.New(data.All(), nil, nil)
	feed := func(msg tea.Msg) {
		nm, _ := m.Update(msg)
		m = nm.(ui.Model)
	}
	typeRune := func(r rune) { feed(key(r, string(r))) }
	special := func(code rune) { feed(tea.KeyPressMsg{Code: code}) }

	feed(tea.WindowSizeMsg{Width: 110, Height: 44})
	special(tea.KeyEnter) // start newest day (day 39 has a constraint)

	// Day 39 carries a constraint, so it opens with the DJ note over the deck.
	for i := 0; i < 6; i++ {
		m = m.Frame()
	}
	fmt.Println("===== DJ NOTE: card composited over the tapes =====")
	fmt.Println(m.Content())
	special(tea.KeyEnter) // confirm the note away

	fmt.Println("\n===== RESTING: factory labels covering every tape =====")
	fmt.Println(m.Content())

	// Peel the cursor tile, then peel-and-reveal a couple more; write a label.
	typeRune('x') // peel tile 0
	for i := 0; i < 3; i++ {
		m = m.Frame()
	}
	fmt.Println("\n===== PEELING: tile0 label lifting, song emerging =====")
	fmt.Println(m.Content())

	for i := 0; i < peelFinish; i++ {
		m = m.Frame()
	}
	special(tea.KeyRight) // -> tile 1
	typeRune('x')         // peel tile 1 fully
	for i := 0; i < peelFinish; i++ {
		m = m.Frame()
	}
	special(tea.KeyRight) // -> tile 2
	special(tea.KeySpace) // cue tile 2
	special(tea.KeyRight) // -> tile 3
	typeRune('e')         // write a label
	for _, r := range "synthy?" {
		typeRune(r)
	}
	special(tea.KeyEnter)

	fmt.Println("\n===== MIXED: t0/t1 revealed, t2 cued, t3 hand-labelled =====")
	fmt.Println(m.Content())

	// Spinning reels: tile 0 (track id 0 under AC_DETERMINISTIC) now playing.
	m = m.DebugPlay(0)
	fmt.Println("\n===== PLAYING: tile0 reels spinning across 4 frames =====")
	for f := 0; f < 4; f++ {
		fmt.Printf("frame %d: %s\n", f, firstReel(m.Content()))
		m = m.Frame()
	}

	// Too-small guard.
	feed(tea.WindowSizeMsg{Width: 70, Height: 18})
	fmt.Println("\n===== TOO SMALL (70x18): guard screen =====")
	fmt.Println(m.Content())
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
