// Package remote keeps the puzzle catalogue fresh by fetching a published
// puzzle.json from the connections.audio site and caching it on disk. The build
// resolves day numbers/dates, so the TUI just consumes the array — no
// TypeScript or schedule logic lives here.
//
// Layering: the binary always ships an embedded snapshot (offline floor). On
// launch it loads the disk cache instantly if present, then refreshes in the
// background; a 304 means nothing changed. Override the source with
// AC_PUZZLES_URL (handy for testing against a local file server).
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/klobucar/ac-term/internal/data"
)

const maxCatalogueBytes = 8 << 20 // 8 MiB sanity cap on the download

// Catalogue is the decoded feed: scheduled puzzles plus the unscheduled backlog.
type Catalogue struct {
	Puzzles []data.Puzzle
	Backlog []data.Puzzle
}

// decode parses published-feed bytes via data.Decode (the one shared decoder
// for embed, cache, and network).
func decode(raw []byte) (Catalogue, error) {
	puzzles, backlog, err := data.Decode(raw)
	if err != nil {
		return Catalogue{}, err
	}
	if len(puzzles) == 0 {
		return Catalogue{}, fmt.Errorf("catalogue has no puzzles")
	}
	return Catalogue{Puzzles: puzzles, Backlog: backlog}, nil
}

// DefaultURL is the published catalogue served by the live site.
const DefaultURL = "https://connections.audio/api/v0/puzzle.json"

// URL is the catalogue source, overridable via AC_PUZZLES_URL.
func URL() string {
	if v := os.Getenv("AC_PUZZLES_URL"); v != "" {
		return v
	}
	return DefaultURL
}

type meta struct {
	URL       string    `json:"url"`
	ETag      string    `json:"etag"`
	FetchedAt time.Time `json:"fetchedAt"`
}

func cacheDir() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ac-term"), nil
}

func dataPath() (string, error) {
	d, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "puzzle.json"), nil
}

func metaPath() (string, error) {
	d, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "meta.json"), nil
}

// validate guards against caching or using a garbage payload.
func validate(cat Catalogue) error {
	if len(cat.Puzzles) == 0 {
		return fmt.Errorf("empty catalogue")
	}
	for _, p := range append(append([]data.Puzzle{}, cat.Puzzles...), cat.Backlog...) {
		if len(p.Themes) != 4 || len(p.Tracks) != 16 {
			return fmt.Errorf("puzzle %q malformed: %d themes, %d tracks", p.ID, len(p.Themes), len(p.Tracks))
		}
	}
	return nil
}

// Cached returns the disk-cached catalogue and when it was fetched. ok is false
// if there's no usable cache.
func Cached() (cat Catalogue, fetchedAt time.Time, ok bool) {
	dp, err := dataPath()
	if err != nil {
		return Catalogue{}, time.Time{}, false
	}
	raw, err := os.ReadFile(dp)
	if err != nil {
		return Catalogue{}, time.Time{}, false
	}
	cat, err = decode(raw)
	if err != nil || validate(cat) != nil {
		return Catalogue{}, time.Time{}, false
	}
	m := readMeta()
	return cat, m.FetchedAt, true
}

func readMeta() meta {
	var m meta
	if mp, err := metaPath(); err == nil {
		if raw, err := os.ReadFile(mp); err == nil {
			_ = json.Unmarshal(raw, &m)
		}
	}
	return m
}

func writeCache(raw []byte, m meta) error {
	dir, err := cacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dp, _ := dataPath()
	if err := os.WriteFile(dp, raw, 0o644); err != nil {
		return err
	}
	mp, _ := metaPath()
	mraw, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(mp, mraw, 0o644)
}

// Refresh does a conditional GET against URL(). On 200 it validates, caches, and
// returns the new catalogue with updated=true. On 304 (cache still current) it
// returns the cached catalogue with updated=false. Any network/parse failure is
// returned as an error so callers can fall back to cache or the embed.
func Refresh(ctx context.Context) (cat Catalogue, updated bool, err error) {
	url := URL()
	prev := readMeta()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Catalogue{}, false, err
	}
	// Only send If-None-Match when the cached etag belongs to this same URL.
	if prev.ETag != "" && prev.URL == url {
		req.Header.Set("If-None-Match", prev.ETag)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Catalogue{}, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		cached, _, ok := Cached()
		if !ok {
			return Catalogue{}, false, fmt.Errorf("304 but no cache on disk")
		}
		return cached, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Catalogue{}, false, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogueBytes+1))
	if err != nil {
		return Catalogue{}, false, err
	}
	if int64(len(raw)) > maxCatalogueBytes {
		return Catalogue{}, false, fmt.Errorf("catalogue exceeds %d bytes", maxCatalogueBytes)
	}
	cat, err = decode(raw)
	if err != nil {
		return Catalogue{}, false, err
	}
	if err := validate(cat); err != nil {
		return Catalogue{}, false, err
	}
	_ = writeCache(raw, meta{URL: url, ETag: resp.Header.Get("ETag"), FetchedAt: time.Now()})
	return cat, true, nil
}
