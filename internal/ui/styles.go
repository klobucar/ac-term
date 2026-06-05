package ui

import "github.com/charmbracelet/lipgloss"

// Palette lifted from the web game's CSS custom properties — the four group
// colors are the "oxide gold / pine LED / azure tape / chrome violet" tape-deck
// set, on a near-black deck background.
var (
	colGold   = lipgloss.Color("#e8b94d") // theme-0
	colGreen  = lipgloss.Color("#9bc25e") // theme-1
	colBlue   = lipgloss.Color("#7fa8e3") // theme-2
	colViolet = lipgloss.Color("#b489ce") // theme-3

	colBG      = lipgloss.Color("#0a0b0e")
	colInk     = lipgloss.Color("#f4ecd6") // warm off-white text
	colMuted   = lipgloss.Color("#8a8d99")
	colDim     = lipgloss.Color("#5a5d68")
	colAmber   = lipgloss.Color("#ffaa55") // VU / now-playing accent
	colHot     = lipgloss.Color("#ff7a64") // mistakes / loss
	colCueWire = lipgloss.Color("#4a9eff")
)

// themeColors indexes the four group colors by themeIdx.
var themeColors = []lipgloss.Color{colGold, colGreen, colBlue, colViolet}

func themeColor(i int) lipgloss.Color {
	if i < 0 || i >= len(themeColors) {
		return colMuted
	}
	return themeColors[i]
}

// tileW is the content+padding width of a cassette tile (border adds 2 more).
const tileW = 21

var (
	bylineStyle = lipgloss.NewStyle().Foreground(colMuted)

	deckBadge = lipgloss.NewStyle().
			Foreground(colBG).Background(colAmber).Bold(true).
			Padding(0, 1)

	// Tile interior text styles. Borders/backgrounds are built per-frame in
	// renderTile so each tape can carry its own animated hue.
	tileArtist = lipgloss.NewStyle().Foreground(colMuted)
	tileNote   = lipgloss.NewStyle().Foreground(colDim).Italic(true)

	playingFlag = lipgloss.NewStyle().Foreground(colAmber).Bold(true)

	// Solved-group banner — full color block with black text.
	solvedBanner = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#16130a")).Bold(true).
			Padding(0, 2).Width(tileW*4 + 8)

	helpStyle = lipgloss.NewStyle().Foreground(colDim)
	keyStyle  = lipgloss.NewStyle().Foreground(colAmber).Bold(true)

	mistakeOn  = lipgloss.NewStyle().Foreground(colHot)
	mistakeOff = lipgloss.NewStyle().Foreground(colDim)

	statusStyle = lipgloss.NewStyle().Foreground(colAmber).Italic(true)
	winStyle    = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	loseStyle   = lipgloss.NewStyle().Foreground(colHot).Bold(true)

	pickerSel = lipgloss.NewStyle().Foreground(colBG).Background(colGold).Bold(true).Padding(0, 1)
	pickerRow = lipgloss.NewStyle().Foreground(colInk).Padding(0, 1)
	pickerDim = lipgloss.NewStyle().Foreground(colDim).Padding(0, 1)
)
