// Package save persists per-day game progress to the cache dir, adopting the
// audio-connections web game's localStorage record shape verbatim so saves are
// interchangeable. The web stores one PersistedGameState per day under
// `audio-connections:day:<id>`; we keep the same records in a single
// progress.json (a map of id -> record), which is cheaper to scan for the
// picker's status column.
//
// Compatibility note: track ids here are the positional 0..15 index
// (themeIdx*4+pos), exactly the web game's LoadedTrack.id, so selected /
// trackOrder / guess ids line up across both.
package save

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Version mirrors the web game's PersistedGameState __v.
const Version = 1

// MaxMistakes is the loss threshold (matches the web game's MAX_MISTAKES).
const MaxMistakes = 4

// Guess is one submitted guess, shaped like the web game's Guess.
type Guess struct {
	Themes  []int `json:"themes"`
	Correct bool  `json:"correct"`
	IDs     []int `json:"ids"`
}

// Note is a [trackID, text] pair. It (de)serializes as a 2-element JSON array
// to match the web game's `Array<[number, string]>` notes encoding.
type Note struct {
	ID   int
	Text string
}

func (n Note) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]any{n.ID, n.Text})
}

func (n *Note) UnmarshalJSON(b []byte) error {
	var a []json.RawMessage
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if len(a) != 2 {
		return fmt.Errorf("note pair must have 2 elements, got %d", len(a))
	}
	if err := json.Unmarshal(a[0], &n.ID); err != nil {
		return err
	}
	return json.Unmarshal(a[1], &n.Text)
}

// Record is one day's persisted state — field-for-field the web game's
// PersistedGameState.
type Record struct {
	V               int      `json:"__v"`
	ID              string   `json:"id"`
	Day             int      `json:"day"`
	Selected        []int    `json:"selected"`
	SolvedThemes    []int    `json:"solvedThemes"`
	Notes           []Note   `json:"notes"`
	Mistakes        int      `json:"mistakes"`
	GuessHistory    []Guess  `json:"guessHistory"`
	GameOver        bool     `json:"gameOver"`
	TrackOrder      []int    `json:"trackOrder"`
	GuessSignatures []string `json:"guessSignatures"`
}

// Won reports a winning terminal record (loss is mistakes == MaxMistakes).
func (r Record) Won() bool { return r.Mistakes < MaxMistakes }

// Status is the derived per-day picker status, matching the web game's
// DayStatus values.
type Status string

const (
	StatusDone         Status = "done"
	StatusDoneMistakes Status = "doneMistakes"
	StatusFailed       Status = "failed"
	StatusInProgress   Status = "inProgress"
	StatusToday        Status = "today"
	StatusUnplayed     Status = "unplayed"
)

// DeriveStatus mirrors the web game's deriveStatus (minus the locked case,
// which the released filter already handles here).
func DeriveStatus(isToday bool, groupsSolved, mistakes int) Status {
	if mistakes >= MaxMistakes {
		return StatusFailed
	}
	if groupsSolved == 4 {
		if mistakes == 0 {
			return StatusDone
		}
		return StatusDoneMistakes
	}
	if groupsSolved > 0 || mistakes > 0 {
		return StatusInProgress
	}
	if isToday {
		return StatusToday
	}
	return StatusUnplayed
}

// Status derives this record's status (records are only created once a day has
// been touched, so this never returns today/unplayed).
func (r Record) Status() Status {
	return DeriveStatus(false, len(r.SolvedThemes), r.Mistakes)
}

// ── on-disk store ───────────────────────────────────────────────────────────

type file struct {
	V    int               `json:"__v"`
	Days map[string]Record `json:"days"`
}

func progressPath() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ac-term", "progress.json"), nil
}

// LoadAll reads every saved record, keyed by puzzle id. Returns an empty (non-
// nil) map when there's no save file yet or it can't be read.
func LoadAll() map[string]Record {
	out := map[string]Record{}
	p, err := progressPath()
	if err != nil {
		return out
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return out
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return out
	}
	for id, rec := range f.Days {
		out[id] = rec
	}
	return out
}

// Put writes (or replaces) one day's record, leaving the others intact.
func Put(rec Record) error {
	rec.V = Version
	all := LoadAll()
	all[rec.ID] = rec
	return writeAll(all)
}

// Delete removes one day's record. A no-op if it wasn't saved.
func Delete(id string) error {
	all := LoadAll()
	if _, ok := all[id]; !ok {
		return nil
	}
	delete(all, id)
	return writeAll(all)
}

func writeAll(all map[string]Record) error {
	p, err := progressPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(file{V: Version, Days: all}, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
