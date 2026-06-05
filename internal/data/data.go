// Package data owns the puzzle catalogue. The embedded `puzzle.json` is the raw
// published feed (the offline floor); the same decode path is used for the live
// network fetch and the on-disk cache, so embed, cache, and feed all share one
// shape. Refresh the embed with `make data` (curls the feed).
package data

import (
	"embed"
	"encoding/json"
	"fmt"
	"time"
)

// The feed dir is embedded as a directory (with a committed .gitkeep) so the
// build never requires puzzle.json to exist — a bare `go build` with no
// `make data` compiles with an empty embed and relies on the runtime fetch.
// `make data` drops puzzle.json in here for the offline floor.
//
//go:embed all:feed
var feedFS embed.FS

// feedBytes returns the embedded feed, or nil when puzzle.json wasn't present
// at build time.
func feedBytes() []byte {
	b, err := feedFS.ReadFile("feed/puzzle.json")
	if err != nil {
		return nil
	}
	return b
}

// Track is one song in a puzzle. ID is a stable 0..15 index within the puzzle
// (themeIdx*4 + position); ItunesID drives the preview lookup.
type Track struct {
	ID       int    `json:"id"`
	ItunesID int64  `json:"itunesId"`
	ThemeIdx int    `json:"themeIdx"`
	Artist   string `json:"artist"`
	Title    string `json:"title"`
	Note     string `json:"note,omitempty"`
}

// Puzzle is a single day: four themes of four tracks each. Day 0 / empty date
// marks an unscheduled backlog puzzle.
type Puzzle struct {
	ID         string   `json:"id"`
	Day        int      `json:"day"`
	Date       string   `json:"date"`
	ReleaseAt  string   `json:"releaseAt"`
	Author     string   `json:"author"`
	Constraint string   `json:"constraint,omitempty"`
	Themes     []string `json:"themes"`
	Tracks     []Track  `json:"tracks"`
}

// Released reports whether the puzzle's release date has arrived as of now.
func (p Puzzle) Released(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, p.ReleaseAt)
	if err != nil {
		return true
	}
	return !now.Before(t)
}

// PrettyDate renders the puzzle date like "May 10, 2026".
func (p Puzzle) PrettyDate() string {
	t, err := time.Parse("2006-01-02", p.Date)
	if err != nil {
		return p.Date
	}
	return t.Format("Jan 2, 2006")
}

// ── published feed shape (api/v0/puzzle.json) ───────────────────────────────
// Wrapped object with themes nested over their tracks and iTunes ids in place.
// Decode flattens it into []Puzzle (themes-as-strings, positional 0..15 ids).

type feedFile struct {
	Puzzles []feedPuzzle `json:"puzzles"`
	Backlog []feedPuzzle `json:"backlog"`
}

type feedPuzzle struct {
	ID         string      `json:"id"`
	Day        int         `json:"day"`
	Date       string      `json:"date"`
	ReleaseAt  string      `json:"releaseAt"`
	Author     string      `json:"author"`
	Constraint string      `json:"constraint"`
	Themes     []feedTheme `json:"themes"`
}

type feedTheme struct {
	Theme  string      `json:"theme"`
	Tracks []feedTrack `json:"tracks"`
}

type feedTrack struct {
	ID     int64  `json:"id"` // iTunes id
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Note   string `json:"note"`
}

func flatten(fp feedPuzzle) Puzzle {
	p := Puzzle{
		ID:         fp.ID,
		Day:        fp.Day,
		Date:       fp.Date,
		ReleaseAt:  fp.ReleaseAt,
		Author:     fp.Author,
		Constraint: fp.Constraint,
	}
	for ti, th := range fp.Themes {
		p.Themes = append(p.Themes, th.Theme)
		for pos, tr := range th.Tracks {
			p.Tracks = append(p.Tracks, Track{
				ID:       ti*4 + pos,
				ItunesID: tr.ID,
				ThemeIdx: ti,
				Artist:   tr.Artist,
				Title:    tr.Title,
				Note:     tr.Note,
			})
		}
	}
	return p
}

// Decode parses published-feed bytes into scheduled puzzles and the unscheduled
// backlog. Shared by the embed, the on-disk cache, and the live fetch. Empty
// input (no embed) decodes to no puzzles — not an error.
func Decode(raw []byte) (puzzles, backlog []Puzzle, err error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	var f feedFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, nil, fmt.Errorf("decode feed: %w", err)
	}
	for _, fp := range f.Puzzles {
		puzzles = append(puzzles, flatten(fp))
	}
	for _, fp := range f.Backlog {
		backlog = append(backlog, flatten(fp))
	}
	return puzzles, backlog, nil
}

// All returns every scheduled puzzle from the embedded feed, in day order.
// Empty when the binary was built without `make data` (runtime fetch fills in).
func All() []Puzzle {
	ps, _, err := Decode(feedBytes())
	if err != nil {
		panic(fmt.Sprintf("ac-term: corrupt embedded puzzle.json: %v", err))
	}
	return ps
}

// Backlog returns the embedded unscheduled puzzles (day 0, no date). Offline
// floor; online the same set comes from the feed's `backlog` key.
func Backlog() []Puzzle {
	_, bl, err := Decode(feedBytes())
	if err != nil {
		panic(fmt.Sprintf("ac-term: corrupt embedded puzzle.json: %v", err))
	}
	return bl
}

// OnlyReleased filters a catalogue down to puzzles whose release date arrived.
func OnlyReleased(ps []Puzzle, now time.Time) []Puzzle {
	out := make([]Puzzle, 0, len(ps))
	for _, p := range ps {
		if p.Released(now) {
			out = append(out, p)
		}
	}
	return out
}

// Released returns only the embedded puzzles whose release date has arrived.
func Released(now time.Time) []Puzzle {
	return OnlyReleased(All(), now)
}
