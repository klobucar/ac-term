package ui

import (
	"fmt"
	"strings"

	"github.com/klobucar/ac-term/internal/data"
	"github.com/klobucar/ac-term/internal/save"

	"github.com/charmbracelet/lipgloss"
)

const gridWidth = 4*(tileW+2) + 3 // four tiles (content+border) + inter-tile gaps

func (m Model) View() string {
	if m.screen == screenPicker {
		return m.viewPicker()
	}
	return m.viewGame()
}

// ── picker ────────────────────────────────────────────────────────────────

func (m Model) viewPicker() string {
	var b strings.Builder
	b.WriteString(header("AUDIO CONNECTIONS", "find four groups of four — by ear"))
	b.WriteString("\n\n")

	// Window of rows that fits the viewport.
	const reserve = 8
	rows := m.height - reserve
	if rows < 5 {
		rows = 5
	}
	top := m.pickerCur - rows/2
	if top < 0 {
		top = 0
	}
	if top+rows > len(m.puzzles) {
		top = len(m.puzzles) - rows
	}
	if top < 0 {
		top = 0
	}
	end := min(top+rows, len(m.puzzles))

	for i := top; i < end; i++ {
		p := m.puzzles[i]
		glyph := m.statusGlyph(i)
		label := dayLabel(p)
		by := fmt.Sprintf("  by %s", p.Author)
		if i == m.pickerCur {
			line := glyph + " " + pickerSel.Render("▸ "+label) + bylineStyle.Render(by)
			b.WriteString(line + "\n")
		} else {
			b.WriteString(glyph + " " + pickerRow.Render("  "+label) + pickerDim.Render(by) + "\n")
		}
	}
	if end < len(m.puzzles) {
		b.WriteString(helpStyle.Render(fmt.Sprintf("   … %d more below", len(m.puzzles)-end)) + "\n")
	}

	b.WriteString("\n")
	if m.refreshNote != "" {
		b.WriteString("  " + statusStyle.Render(m.refreshNote) + "\n\n")
	}
	b.WriteString(helpStyle.Render("  "))
	b.WriteString(keyHint("↑/↓", "browse") + "   " + keyHint("enter", "play") + "   " + keyHint("q", "quit"))
	if !m.canPlay {
		b.WriteString("\n\n  " + helpStyle.Render("(no audio player found — runs muted; install mpv/ffplay/vlc for sound)"))
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

// statusGlyph renders a colored progress marker for the day at picker index i,
// derived from its saved record (or fresh/today when there's no save).
func (m Model) statusGlyph(i int) string {
	p := m.puzzles[i]
	st := save.StatusUnplayed
	if rec, ok := m.saves[p.ID]; ok {
		st = rec.Status()
	} else if p.Day > 0 && p.Day == m.latestDay() {
		st = save.StatusToday
	}
	switch st {
	case save.StatusDone:
		return lipgloss.NewStyle().Foreground(colGreen).Bold(true).Render("✓")
	case save.StatusDoneMistakes:
		return lipgloss.NewStyle().Foreground(colGold).Bold(true).Render("✓")
	case save.StatusFailed:
		return lipgloss.NewStyle().Foreground(colHot).Bold(true).Render("✗")
	case save.StatusInProgress:
		return lipgloss.NewStyle().Foreground(colAmber).Render("◔")
	case save.StatusToday:
		return lipgloss.NewStyle().Foreground(colGold).Render("●")
	default:
		return mistakeOff.Render("·")
	}
}

// ── game ──────────────────────────────────────────────────────────────────

// minBoardH is the rows the deck needs before the layout starts clipping.
const minBoardH = 26

// minBoardW is the columns the 4-wide grid needs (grid + the view's padding).
const minBoardW = gridWidth + 4

// tooSmall reports whether the terminal can't fit the game board.
func (m Model) tooSmall() bool {
	return m.width > 0 && (m.width < minBoardW || m.height < minBoardH)
}

func (m Model) renderTooSmall() string {
	title := lipgloss.NewStyle().Foreground(colGold).Bold(true).Render("⤢  terminal too small")
	detail := bylineStyle.Render(fmt.Sprintf("need ≥ %d×%d  ·  have %d×%d", minBoardW, minBoardH, m.width, m.height))
	hint := helpStyle.Render("resize the window to drop the needle")
	box := lipgloss.JoinVertical(lipgloss.Center, title, "", detail, "", hint)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewGame() string {
	if m.tooSmall() {
		return m.renderTooSmall()
	}

	var b strings.Builder

	// Heading row.
	sub := fmt.Sprintf("by %s · %s", m.puzzle.Author, m.puzzle.PrettyDate())
	if m.puzzle.Day == 0 {
		sub = "by " + m.puzzle.Author + " · backlog"
	}
	b.WriteString(header(strings.ToUpper(m.puzzleName()), sub))
	if m.puzzle.Constraint != "" {
		b.WriteString("\n" + deckBadge.Render("DJ note") + " " + bylineStyle.Italic(true).Render(m.puzzle.Constraint))
	}
	b.WriteString("\n\n")

	// Solved/revealed group banners.
	for _, ti := range m.solvedOrder() {
		b.WriteString(m.renderBanner(ti) + "\n")
	}
	if len(m.solvedOrder()) > 0 && len(m.order) > 0 {
		b.WriteString("\n")
	}

	// The grid of remaining tiles.
	if len(m.order) > 0 {
		b.WriteString(m.renderGrid())
		b.WriteString("\n\n")
	}

	// Mistakes + cue meter.
	b.WriteString(m.renderStatusBar() + "\n")

	// Status line.
	if m.status != "" {
		b.WriteString(statusStyle.Render(m.status) + "\n")
	} else {
		b.WriteString("\n")
	}

	// End card or help.
	if m.gameOver {
		b.WriteString("\n" + m.renderEndCard())
	} else {
		b.WriteString("\n" + m.renderHelp())
	}

	view := lipgloss.NewStyle().Padding(1, 2).Render(b.String())
	switch {
	case m.noteOpen:
		// Composite the DJ note over the deck, tapes showing around it.
		view = centerOver(view, m.renderNoteCard())
	case m.confirmErase:
		view = centerOver(view, m.renderEraseCard())
	}
	return view
}

func (m Model) renderBanner(themeIdx int) string {
	tracks := m.tracksForTheme(themeIdx)
	names := make([]string, len(tracks))
	for i, t := range tracks {
		names[i] = t.Title
	}
	style := solvedBanner.Background(themeColor(themeIdx)).Width(gridWidth)
	body := fmt.Sprintf("%s  —  %s", strings.ToUpper(m.puzzle.Themes[themeIdx]), strings.Join(names, " · "))
	return style.Render(body)
}

func (m Model) renderGrid() string {
	var rows []string
	for r := 0; r*4 < len(m.order); r++ {
		var cells []string
		for c := 0; c < 4 && r*4+c < len(m.order); c++ {
			idx := r*4 + c
			cells = append(cells, m.renderTile(idx))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// Tile interior column budgets (content width = tileW - 2 padding).
const (
	tileInner = tileW - 2
	titleW    = tileInner - 3 // caret + marker + space
	artistW   = tileInner - 2 // two-space indent
	reelW     = tileInner
)

func (m Model) renderTile(idx int) string {
	id := m.order[idx]
	t := m.byID[id]
	cued := m.selected[id]
	cursor := idx == m.cursor
	revealed := m.revealed[id]
	playing := m.playingID == id
	hasNote := m.notes[id] != ""

	bright := 0.85
	if cursor {
		bright = 1.2
	}
	hueA, hueB := tapeColors(idx, m.frame/2, bright)

	// Border style/color encodes selection state on top of the tape's hue.
	border := lipgloss.RoundedBorder()
	borderCol := hueA.color()
	switch {
	case cursor && cued:
		border, borderCol = lipgloss.DoubleBorder(), colAmber
	case cursor:
		border, borderCol = lipgloss.DoubleBorder(), colInk
	case cued:
		border, borderCol = lipgloss.ThickBorder(), colCueWire
	}

	// Cue state reads from the thick blue border + ● marker; we deliberately
	// avoid a fill color here — inner gradient/label text emits SGR resets that
	// would leave a partial, patchy black background.
	box := lipgloss.NewStyle().
		Width(tileW).Height(3).Padding(0, 1).
		Border(border).BorderForeground(borderCol).
		MarginRight(1)

	// Markers: cursor caret + play/cue indicator.
	caret := " "
	if cursor {
		caret = lipgloss.NewStyle().Foreground(colGold).Render("▸")
	}
	marker := " "
	switch {
	case playing:
		marker = playingFlag.Render("♪")
	case cued:
		marker = lipgloss.NewStyle().Foreground(colCueWire).Render("●")
	case hasNote && !revealed:
		marker = lipgloss.NewStyle().Foreground(colAmber).Render("✎")
	}

	// Reels keep turning: per-tile phase offset so the deck looks alive.
	pos := m.frame/3 + idx*4

	// Peel progress p: 0 = label covering the song, 1 = song fully revealed.
	p := 0.0
	if revealed {
		p = 1.0
	}
	if st, ok := m.animStart[id]; ok {
		if q := float64(m.frame-st) / float64(peelDur); q < 1 {
			if revealed {
				p = q // peeling off → cover lifts to reveal
			} else {
				p = 1 - q // sliding back on → cover slides over song
			}
		}
	}

	const dots = "· · · · · · ·"
	cover := m.coverText(id)
	if m.editing && cursor {
		cover = m.notes[id] + "▏"
	}

	var line1, line2 string
	switch {
	case p >= 1:
		// Revealed and resting: the real song, scrolling if long.
		line1 = caret + marker + " " + gradientText(marquee(t.Title, titleW, pos), hueA, hueB)
		line2 = "  " + tileArtist.Render(marquee(t.Artist, artistW, pos))
	case p <= 0:
		// Covered and resting: the label (factory sticker or your own).
		var lab string
		if hasNote || (m.editing && cursor) {
			lab = lipgloss.NewStyle().Foreground(colAmber).Bold(true).Render(marquee(cover, titleW, pos))
		} else {
			lab = tileNote.Render(marquee(cover, titleW, 0))
		}
		line1 = caret + marker + " " + lab
		line2 = "  " + tileNote.Render(marquee(dots, artistW, 0))
	default:
		// Mid peel/cover: song emerging from (or vanishing under) the label.
		line1 = caret + marker + " " + peelReveal(t.Title, cover, titleW, p, hueA, hueB)
		line2 = "  " + peelReveal(t.Artist, dots, artistW, p, mutedRGB, mutedRGB)
	}
	line3 := reelBar(idx, m.frame, reelW, playing)

	return box.Render(line1 + "\n" + line2 + "\n" + line3)
}

func (m Model) renderStatusBar() string {
	// Mistakes as burnt tape dots.
	dots := make([]string, maxMistakes)
	for i := 0; i < maxMistakes; i++ {
		if i < m.mistakes {
			dots[i] = mistakeOn.Render("✗")
		} else {
			dots[i] = mistakeOff.Render("•")
		}
	}
	mistakes := bylineStyle.Render("mistakes ") + strings.Join(dots, " ")

	// Cue meter: 4 slots.
	slots := make([]string, 4)
	cued := len(m.selected)
	for i := 0; i < 4; i++ {
		if i < cued {
			slots[i] = lipgloss.NewStyle().Foreground(colCueWire).Render("▰")
		} else {
			slots[i] = mistakeOff.Render("▱")
		}
	}
	cue := bylineStyle.Render("cued ") + strings.Join(slots, "")
	gap := strings.Repeat(" ", max(2, gridWidth-lipgloss.Width(mistakes)-lipgloss.Width(cue)))
	return mistakes + gap + cue
}

func (m Model) renderHelp() string {
	if m.editing {
		return statusStyle.Render("✎ writing label — ") +
			keyHint("enter", "save") + "  " + keyHint("esc", "cancel")
	}
	parts := []string{
		keyHint("←↑↓→", "move"),
		keyHint("space", "cue"),
		keyHint("p", "play"),
		keyHint("s", "submit"),
		keyHint("x", "peel/cover"),
		keyHint("e", "write"),
		keyHint("d", "clear"),
		keyHint("r", "shuffle"),
		keyHint("E", "erase"),
		keyHint("q", "back"),
	}
	return helpStyle.Render(strings.Join(parts, helpStyle.Render(" · ")))
}

func (m Model) renderEndCard() string {
	var b strings.Builder
	if m.won {
		b.WriteString(winStyle.Render("◆ SOLVED ◆"))
	} else {
		b.WriteString(loseStyle.Render("◇ OUT OF TAPE ◇"))
	}
	b.WriteString("\n\n")
	b.WriteString(bylineStyle.Render(m.puzzleName()) + "\n")
	for _, g := range m.guesses {
		row := make([]string, len(g))
		for i, ti := range g {
			row[i] = themeEmoji[ti]
		}
		b.WriteString(strings.Join(row, "") + "\n")
	}
	b.WriteString("\n" + keyHint("enter", "back to deck") + "   " + keyHint("E", "replay") + "   " + keyHint("q", "quit"))
	return b.String()
}

// ── helpers ───────────────────────────────────────────────────────────────

// tracksForTheme returns a theme's four tracks in their original puzzle order.
func (m Model) tracksForTheme(themeIdx int) []data.Track {
	var out []data.Track
	for _, t := range m.puzzle.Tracks {
		if t.ThemeIdx == themeIdx {
			out = append(out, t)
		}
	}
	return out
}

// puzzleName is the display name: "Audio Connections N" for scheduled days,
// "Backlog · <id>" for unscheduled backlog puzzles (Day 0).
func (m Model) puzzleName() string {
	if m.puzzle.Day == 0 {
		return "Backlog · " + m.puzzle.ID
	}
	return fmt.Sprintf("Audio Connections %d", m.puzzle.Day)
}

// dayLabel is the picker row label for a puzzle. Both the day-number and the
// date/id columns are fixed-width so the trailing "by <author>" lines up across
// every row (longest date is "May 10, 2026" = 12).
func dayLabel(p data.Puzzle) string {
	if p.Day == 0 {
		return fmt.Sprintf("Backlog  %-12s", p.ID)
	}
	return fmt.Sprintf("Day %-3d  %-12s", p.Day, p.PrettyDate())
}

// latestDay is the highest scheduled day number in the list (0 if none).
func (m Model) latestDay() int {
	max := 0
	for _, p := range m.puzzles {
		if p.Day > max {
			max = p.Day
		}
	}
	return max
}

func header(title, sub string) string {
	// Spectrum sweep across the wordmark for the cassette-deck pop.
	t := lipgloss.NewStyle().Bold(true).Render(gradientText(title, hsl(30, 0.9, 0.6), hsl(280, 0.8, 0.66)))
	if sub == "" {
		return t
	}
	return t + "\n" + bylineStyle.Render(sub)
}

func keyHint(key, label string) string {
	return keyStyle.Render(key) + " " + helpStyle.Render(label)
}
