// Package ui is the Bubbletea front-end for Audio Connections: a cassette-deck
// take on the daily song-grouping puzzle. Cue four tracks, hit submit, find the
// four hidden groups of four before you burn through four mistakes.
package ui

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/klobucar/ac-term/internal/audio"
	"github.com/klobucar/ac-term/internal/data"
	"github.com/klobucar/ac-term/internal/save"

	tea "charm.land/bubbletea/v2"
)

// tickRate drives marquee scrolling, reel shimmer and VU animation.
const tickRate = 140 * time.Millisecond

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(tickRate, func(time.Time) tea.Msg { return tickMsg{} })
}

const maxMistakes = 4

// themeEmoji mirrors the web game's share-grid squares, indexed by themeIdx.
var themeEmoji = []string{"🟨", "🟩", "🟦", "🟪"}

type screen int

const (
	screenPicker screen = iota
	screenGame
)

// Sender lets background goroutines (audio end-of-clip callbacks) inject
// messages back into the running program. main wires this to *tea.Program.
type Sender interface{ Send(tea.Msg) }

// ── async messages ──────────────────────────────────────────────────────────

type playStartedMsg struct{ trackID int }
type playStoppedMsg struct{ trackID int } // clip finished on its own
type playErrMsg struct {
	trackID int
	err     error
}

// CatalogueMsg delivers a refreshed (released) puzzle list from the background
// fetch. Exported so main can Send it into the program.
type CatalogueMsg struct{ Puzzles []data.Puzzle }

// Model is the whole app state machine.
type Model struct {
	puzzles []data.Puzzle
	player  *audio.Player
	send    Sender
	canPlay bool

	screen      screen
	pickerCur   int
	pickerTop   int // scroll offset
	width       int
	height      int
	refreshNote string // picker footer note after a background catalogue refresh

	saves map[string]save.Record // persisted progress by puzzle id

	// active game ─────────
	puzzle    data.Puzzle
	byID      map[int]data.Track
	fullOrder []int        // all 16 track ids in board order (for persistence)
	order     []int        // unsolved track ids, in board order
	selected  map[int]bool // cued track ids
	cursor    int          // index into order
	mistakes  int
	solved    []int   // themeIdx in solve order
	guesses   [][]int // each guess: themeIdx of the 4 picked tiles (share grid)
	sigs      map[string]bool
	gameOver  bool
	won       bool
	playingID int // track id currently sounding, -1 if silent
	status    string

	frame        int            // animation counter
	revealed     map[int]bool   // tape whose label is peeled, song showing (default false)
	animStart    map[int]int    // frame the current peel/cover animation began
	stickers     map[int]string // factory label covering each tape
	notes        map[int]string // hand-written labels (replace the factory sticker)
	editing      bool           // writing a label on the focused tape
	noteOpen     bool           // DJ-note card is up over the deck, awaiting confirm
	confirmErase bool           // "erase tape?" confirm card is up
}

// peelDur is how many ticks the label peel/cover animation lasts.
const peelDur = 7

// coverText is what's printed on the focused tape's label — the player's own
// scrawl if they've written one, otherwise the factory sticker.
func (m Model) coverText(id int) string {
	if n := m.notes[id]; n != "" {
		return n
	}
	return m.stickers[id]
}

// New builds the initial model on the day picker.
func New(puzzles []data.Puzzle, player *audio.Player, send Sender) Model {
	return Model{
		puzzles:   puzzles,
		player:    player,
		send:      send,
		canPlay:   audio.Available() && player != nil,
		screen:    screenPicker,
		pickerCur: defaultPick(puzzles), // newest scheduled day by default
		playingID: -1,
		saves:     save.LoadAll(),
	}
}

// defaultPick lands the picker on the newest scheduled day, skipping any
// backlog rows appended after it.
func defaultPick(puzzles []data.Puzzle) int {
	for i := len(puzzles) - 1; i >= 0; i-- {
		if puzzles[i].Day > 0 {
			return i
		}
	}
	return max(0, len(puzzles)-1)
}

func (m Model) Init() tea.Cmd { return tick() }

// Frame advances the animation clock one tick. Exposed for dev snapshots that
// render mid-animation without a running event loop.
func (m Model) Frame() Model { m.frame++; return m }

// DebugPlay forces a track into the "now playing" state. Dev-snapshots only.
func (m Model) DebugPlay(trackID int) Model { m.playingID = trackID; return m }

// Content returns the rendered screen string (what View wraps in a tea.View).
// Exposed for dev snapshots that print frames without a running program.
func (m Model) Content() string { return m.screenContent() }

// ── game setup ────────────────────────────────────────────────────────────

func (m *Model) startGame(p data.Puzzle) {
	m.puzzle = p
	m.byID = make(map[int]data.Track, len(p.Tracks))
	ids := make([]int, 0, len(p.Tracks))
	for _, t := range p.Tracks {
		m.byID[t.ID] = t
		ids = append(ids, t.ID)
	}

	// Resume from a saved record when one matches this puzzle's tracks;
	// otherwise deal a fresh, shuffled board.
	rec, hasSave := m.saves[p.ID]
	restoring := hasSave && validTrackOrder(rec.TrackOrder, len(ids))

	if restoring {
		ids = append(ids[:0], rec.TrackOrder...)
	} else if os.Getenv("AC_DETERMINISTIC") == "" {
		// AC_DETERMINISTIC keeps tracks in theme order (0-3, 4-7, …) so dev
		// snapshots and any future tests can cue a known group by index.
		rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	}
	m.fullOrder = append([]int(nil), ids...)

	m.selected = make(map[int]bool)
	m.cursor = 0
	m.mistakes = 0
	m.solved = nil
	m.guesses = nil
	m.sigs = make(map[string]bool)
	m.gameOver = false
	m.won = false
	m.playingID = -1
	m.status = ""
	m.revealed = make(map[int]bool, len(ids))
	m.animStart = make(map[int]int, len(ids))
	m.stickers = make(map[int]string, len(ids))
	m.notes = make(map[int]string)
	for i, id := range ids {
		m.stickers[id] = stickerLabel(i)
	}
	m.editing = false
	m.noteOpen = false

	if restoring {
		m.restoreFrom(rec)
	} else {
		m.order = ids
		// Tapes rest under a factory label — the song's a mystery you solve by
		// ear. A puzzle with a constraint greets you with the DJ's note.
		m.noteOpen = p.Constraint != ""
	}
	m.screen = screenGame

	// Warm the preview cache in the background so the first PLAY is instant.
	if m.canPlay {
		for _, t := range p.Tracks {
			id := t.ItunesID
			go m.player.Prefetch(context.Background(), id)
		}
	}
}

// validTrackOrder reports whether a saved order is a usable permutation of the
// 0..n-1 track ids (mirrors the web loader's set-equality staleness check).
func validTrackOrder(order []int, n int) bool {
	if len(order) != n {
		return false
	}
	seen := make([]bool, n)
	for _, id := range order {
		if id < 0 || id >= n || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// restoreFrom rebuilds session state from a saved record. ids/maps are assumed
// already initialized for rec.TrackOrder.
func (m *Model) restoreFrom(rec save.Record) {
	solvedSet := map[int]bool{}
	for _, ti := range rec.SolvedThemes {
		solvedSet[ti] = true
	}
	m.solved = append([]int(nil), rec.SolvedThemes...)

	// The board holds only tracks whose theme isn't solved yet.
	for _, id := range m.fullOrder {
		if !solvedSet[m.byID[id].ThemeIdx] {
			m.order = append(m.order, id)
		}
	}
	for _, id := range rec.Selected {
		m.selected[id] = true
	}
	for _, n := range rec.Notes {
		m.notes[n.ID] = n.Text
	}
	for _, g := range rec.GuessHistory {
		m.guesses = append(m.guesses, append([]int(nil), g.Themes...))
	}
	for _, sig := range rec.GuessSignatures {
		m.sigs[sig] = true
	}
	m.mistakes = rec.Mistakes
	m.gameOver = rec.GameOver
	m.won = rec.GameOver && rec.Won()
	if len(m.order) > 0 {
		m.cursor = 0
	}
}

// ── update ────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.frame++
		return m, tick()

	case CatalogueMsg:
		if len(msg.Puzzles) > 0 {
			grew := len(msg.Puzzles) - len(m.puzzles)
			m.puzzles = msg.Puzzles
			if m.pickerCur >= len(m.puzzles) {
				m.pickerCur = len(m.puzzles) - 1
			}
			if m.pickerCur < 0 {
				m.pickerCur = 0
			}
			switch {
			case grew == 1:
				m.refreshNote = "↻ 1 new day added"
			case grew > 1:
				m.refreshNote = fmt.Sprintf("↻ %d new days added", grew)
			default:
				m.refreshNote = "↻ catalogue up to date"
			}
		}
		return m, nil

	case playStartedMsg:
		m.playingID = msg.trackID
		return m, nil

	case playStoppedMsg:
		if m.playingID == msg.trackID {
			m.playingID = -1
		}
		return m, nil

	case playErrMsg:
		m.playingID = -1
		m.status = "⚠ preview unavailable for that track"
		return m, nil

	case tea.KeyPressMsg:
		if m.screen == screenPicker {
			return m.updatePicker(msg)
		}
		return m.updateGame(msg)
	}
	return m, nil
}

func (m Model) updatePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.pickerCur > 0 {
			m.pickerCur--
		}
	case "down", "j":
		if m.pickerCur < len(m.puzzles)-1 {
			m.pickerCur++
		}
	case "home", "g":
		m.pickerCur = 0
	case "end", "G":
		m.pickerCur = len(m.puzzles) - 1
	case "enter", "space":
		m.startGame(m.puzzles[m.pickerCur])
	}
	return m, nil
}

func (m Model) updateGame(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The DJ-note card captures input until confirmed away.
	if m.noteOpen {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter", "space", "esc":
			m.noteOpen = false
		}
		return m, nil
	}

	// The erase-tape confirm captures input until answered.
	if m.confirmErase {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "y", "Y", "enter":
			return m.eraseTape()
		default: // n / N / esc / anything else cancels
			m.confirmErase = false
		}
		return m, nil
	}

	// Label-writing mode swallows keystrokes into the focused tape's note.
	if m.editing {
		return m.updateEditing(msg), nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc":
		// Back to the picker; hush the deck.
		if m.canPlay {
			m.player.Stop()
		}
		m.playingID = -1
		m.screen = screenPicker
		return m, nil
	}

	if m.gameOver {
		// On the end card: back to picker, or erase to replay.
		switch msg.String() {
		case "enter", "space":
			if m.canPlay {
				m.player.Stop()
			}
			m.screen = screenPicker
		case "E":
			m.confirmErase = true
		}
		return m, nil
	}

	switch msg.String() {
	case "left", "h":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right", "l":
		if m.cursor < len(m.order)-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor-4 >= 0 {
			m.cursor -= 4
		}
	case "down", "j":
		if m.cursor+4 < len(m.order) {
			m.cursor += 4
		}
	case "space":
		return m.toggleCue(), nil
	case "p", "enter":
		return m.togglePlay()
	case "d":
		m.selected = make(map[int]bool)
		m.status = "Tape ejected — picks cleared"
	case "x":
		return m.peelLabel(), nil
	case "e", "w":
		return m.beginEdit(), nil
	case "E":
		m.confirmErase = true
	case "r":
		rand.Shuffle(len(m.order), func(i, j int) { m.order[i], m.order[j] = m.order[j], m.order[i] })
		if m.cursor >= len(m.order) {
			m.cursor = len(m.order) - 1
		}
		m.status = "Reels shuffled"
	case "s":
		return m.submit()
	}
	return m, nil
}

func (m Model) toggleCue() Model {
	if m.cursor < 0 || m.cursor >= len(m.order) {
		return m
	}
	id := m.order[m.cursor]
	if m.selected[id] {
		delete(m.selected, id)
	} else {
		if len(m.selected) >= 4 {
			m.status = "Deck's full — four tracks cued"
			return m
		}
		m.selected[id] = true
	}
	m.status = ""
	return m
}

// currentID is the track id under the cursor, or -1 if the board is empty.
func (m Model) currentID() int {
	if m.cursor < 0 || m.cursor >= len(m.order) {
		return -1
	}
	return m.order[m.cursor]
}

// peelLabel toggles the focused tape's label: peel it off to reveal the song
// underneath, or slide it back over to hide the song again. Either way the
// transition animates.
func (m Model) peelLabel() Model {
	id := m.currentID()
	if id < 0 {
		return m
	}
	m.animStart[id] = m.frame
	if m.revealed[id] {
		delete(m.revealed, id)
		m.status = "Label back on — song hidden"
	} else {
		m.revealed[id] = true
		m.status = "Peeled — " + m.byID[id].Title
	}
	return m
}

// beginEdit drops into write mode for the focused tape's hand-label. The label
// lives on the cover, so writing slides the cover back over the song.
func (m Model) beginEdit() Model {
	id := m.currentID()
	if id < 0 {
		return m
	}
	if m.revealed[id] {
		delete(m.revealed, id)
		m.animStart[id] = m.frame
	}
	m.editing = true
	m.status = "Writing label — type, enter to save, esc to cancel"
	return m
}

func (m Model) updateEditing(msg tea.KeyPressMsg) Model {
	id := m.currentID()
	if id < 0 {
		m.editing = false
		return m
	}
	switch msg.Code {
	case tea.KeyEnter:
		m.editing = false
		m.status = "Label written ✎"
	case tea.KeyEsc:
		m.editing = false
		m.status = ""
	case tea.KeyBackspace, tea.KeyDelete:
		r := []rune(m.notes[id])
		if len(r) > 0 {
			m.notes[id] = string(r[:len(r)-1])
		}
	default:
		// Any printable key (incl. space) appends its text.
		if msg.Text != "" && len([]rune(m.notes[id])) < 28 {
			m.notes[id] += msg.Text
		}
	}
	return m
}

func (m Model) togglePlay() (tea.Model, tea.Cmd) {
	if !m.canPlay {
		m.status = "No audio player found — install mpv, ffplay, or vlc"
		return m, nil
	}
	if m.cursor < 0 || m.cursor >= len(m.order) {
		return m, nil
	}
	id := m.order[m.cursor]
	if m.playingID == id {
		m.player.Stop()
		m.playingID = -1
		return m, nil
	}
	track := m.byID[id]
	return m, m.playCmd(id, track.ItunesID)
}

// playCmd resolves+plays a preview off the main loop. The natural end-of-clip
// signal arrives later via the audio package's onDone callback → Sender.
func (m Model) playCmd(trackID int, itunesID int64) tea.Cmd {
	player := m.player
	send := m.send
	return func() tea.Msg {
		err := player.Play(context.Background(), itunesID, func(int64) {
			if send != nil {
				send.Send(playStoppedMsg{trackID: trackID})
			}
		})
		if err != nil {
			return playErrMsg{trackID: trackID, err: err}
		}
		return playStartedMsg{trackID: trackID}
	}
}

// ── submit / guess resolution ─────────────────────────────────────────────

func (m Model) submit() (tea.Model, tea.Cmd) {
	if len(m.selected) != 4 {
		m.status = fmt.Sprintf("Cue four tracks to submit (%d cued)", len(m.selected))
		return m, nil
	}
	picks := make([]int, 0, 4)
	for _, id := range m.order { // stable order for a tidy share grid
		if m.selected[id] {
			picks = append(picks, id)
		}
	}
	sig := signature(picks)
	if m.sigs[sig] {
		m.status = "Already tried that exact set"
		return m, nil
	}
	m.sigs[sig] = true

	// Count themes among the picks.
	counts := map[int]int{}
	pickedThemes := make([]int, len(picks))
	for i, id := range picks {
		ti := m.byID[id].ThemeIdx
		pickedThemes[i] = ti
		counts[ti]++
	}
	m.guesses = append(m.guesses, pickedThemes)

	maxCount, maxTheme := 0, -1
	for t, c := range counts {
		if c > maxCount {
			maxCount, maxTheme = c, t
		}
	}

	var res Model
	if maxCount == 4 {
		res = m.solveTheme(maxTheme)
	} else {
		m.mistakes++
		if maxCount == 3 {
			m.status = "So close — one away!"
		} else {
			m.status = "Not a group. Try again."
		}
		if m.mistakes >= maxMistakes {
			res = m.endGame(false)
		} else {
			res = m
		}
	}
	// Every resolved guess updates the day's saved record.
	res = res.persist()
	return res, nil
}

// persist writes the current session to the save store and refreshes the
// in-memory copy used by the picker's status column.
func (m Model) persist() Model {
	if m.puzzle.ID == "" {
		return m
	}
	rec := m.buildRecord()
	_ = save.Put(rec)
	if m.saves == nil {
		m.saves = map[string]save.Record{}
	}
	m.saves[rec.ID] = rec
	return m
}

func (m Model) buildRecord() save.Record {
	selected := make([]int, 0, len(m.selected))
	for id := range m.selected {
		selected = append(selected, id)
	}
	sort.Ints(selected)

	notes := make([]save.Note, 0, len(m.notes))
	for id, txt := range m.notes {
		notes = append(notes, save.Note{ID: id, Text: txt})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].ID < notes[j].ID })

	sigs := make([]string, 0, len(m.sigs))
	for s := range m.sigs {
		sigs = append(sigs, s)
	}
	sort.Strings(sigs)

	guesses := make([]save.Guess, len(m.guesses))
	for i, g := range m.guesses {
		correct := len(g) == 4
		for _, t := range g {
			if t != g[0] {
				correct = false
			}
		}
		// ids are intentionally empty (dedup runs off guessSignatures), matching
		// the web game's imported-record encoding.
		guesses[i] = save.Guess{Themes: append([]int(nil), g...), Correct: correct, IDs: []int{}}
	}

	return save.Record{
		V:               save.Version,
		ID:              m.puzzle.ID,
		Day:             m.puzzle.Day,
		Selected:        selected,
		SolvedThemes:    append([]int(nil), m.solved...),
		Notes:           notes,
		Mistakes:        m.mistakes,
		GuessHistory:    guesses,
		GameOver:        m.gameOver,
		TrackOrder:      append([]int(nil), m.fullOrder...),
		GuessSignatures: sigs,
	}
}

func (m Model) solveTheme(themeIdx int) Model {
	// Drop the four solved tracks from the board.
	next := m.order[:0:0]
	for _, id := range m.order {
		if m.byID[id].ThemeIdx != themeIdx {
			next = append(next, id)
		}
	}
	m.order = next
	m.selected = make(map[int]bool)
	m.solved = append(m.solved, themeIdx)
	if m.cursor >= len(m.order) {
		m.cursor = max(0, len(m.order)-1)
	}
	m.status = fmt.Sprintf("🎯 %s", m.puzzle.Themes[themeIdx])
	if len(m.solved) == 4 {
		return m.endGame(true)
	}
	return m
}

func (m Model) endGame(won bool) Model {
	m.gameOver = true
	m.won = won
	if m.canPlay {
		m.player.Stop()
	}
	m.playingID = -1
	if won {
		m.status = "Nailed it. 🎧"
	} else {
		m.status = "Out of tape. Here's the reveal."
	}
	return m
}

// eraseTape wipes the current day's saved record and deals it fresh, so a
// finished day can be replayed (and a mid-game can be restarted).
func (m Model) eraseTape() (tea.Model, tea.Cmd) {
	id := m.puzzle.ID
	delete(m.saves, id)
	_ = save.Delete(id)
	if m.canPlay {
		m.player.Stop()
	}
	m.confirmErase = false
	m.startGame(m.puzzle) // no save now → fresh deal
	m.status = "🧲 tape erased — fresh deal"
	return m, nil
}

func signature(ids []int) string {
	s := make([]int, len(ids))
	copy(s, ids)
	// tiny insertion sort — only ever four elements
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	var b strings.Builder
	for i, v := range s {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d", v)
	}
	return b.String()
}

// solvedSet reports which themes are revealed (solved, or all on game over).
func (m Model) solvedOrder() []int {
	if m.gameOver && !m.won {
		// Append the unsolved themes so the reveal shows the full board.
		seen := map[int]bool{}
		out := append([]int{}, m.solved...)
		for _, t := range m.solved {
			seen[t] = true
		}
		for t := 0; t < len(m.puzzle.Themes); t++ {
			if !seen[t] {
				out = append(out, t)
			}
		}
		return out
	}
	return m.solved
}
