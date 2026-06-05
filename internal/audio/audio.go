// Package audio handles song-preview playback for the TUI. It resolves an
// iTunes track id to its 30s .m4a (AAC) preview, caches the bytes on disk, and
// plays them through whatever CLI player the OS has — afplay on macOS, or
// ffplay/mpv/vlc/sox on Linux & friends. Only one clip plays at a time;
// starting a new one stops whatever's running.
package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// backend is a CLI audio player and how to invoke it for a one-shot, headless,
// quiet playback of a single file that exits on its own at end-of-clip.
type backend struct {
	name string
	args func(path string) []string
}

// backends are tried in order; the first one on PATH wins. All must decode AAC
// (.m4a previews) and run in the foreground so Wait()/Kill() control the clip.
var backends = []backend{
	{"afplay", func(p string) []string { return []string{p} }},                                               // macOS
	{"ffplay", func(p string) []string { return []string{"-nodisp", "-autoexit", "-loglevel", "quiet", p} }}, // ffmpeg
	{"mpv", func(p string) []string { return []string{"--no-video", "--no-terminal", "--really-quiet", p} }},
	{"cvlc", func(p string) []string { return []string{"--play-and-exit", "--intf", "dummy", "--quiet", p} }}, // VLC
	{"play", func(p string) []string { return []string{"-q", p} }},                                            // sox (needs libsox-fmt-all)
}

// detectBackend returns the first available CLI player.
func detectBackend() (backend, bool) {
	for _, b := range backends {
		if _, err := exec.LookPath(b.name); err == nil {
			return b, true
		}
	}
	return backend{}, false
}

// defaultPreviewCacheMB caps the persistent preview cache; override with
// AC_PREVIEW_CACHE_MB (0 = unlimited).
const defaultPreviewCacheMB = 200

// Player owns the single active player process and a persistent, size-capped
// disk cache of previews (LRU-evicted by file mtime).
type Player struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	cacheDir string
	client   *http.Client
	backend  backend
	maxBytes int64 // preview cache cap; <= 0 means unlimited

	evictMu sync.Mutex // serializes cap enforcement

	urlMu sync.Mutex
	urls  map[int64]string // itunesId -> previewUrl (in-memory memo)
}

// previewsDir is the persistent preview cache location.
func previewsDir() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ac-term", "previews"), nil
}

// NewPlayer creates a player backed by a persistent preview cache and a detected
// backend. Previews survive across runs and are trimmed to the size cap.
func NewPlayer() (*Player, error) {
	dir, err := previewsDir()
	if err != nil {
		dir, err = os.MkdirTemp("", "ac-term-audio-") // fall back to ephemeral
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	maxBytes := int64(defaultPreviewCacheMB) << 20
	if v := os.Getenv("AC_PREVIEW_CACHE_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxBytes = int64(n) << 20
		}
	}

	b, _ := detectBackend() // may be empty; Play guards on it
	return &Player{
		cacheDir: dir,
		client:   &http.Client{Timeout: 20 * time.Second},
		backend:  b,
		maxBytes: maxBytes,
		urls:     make(map[int64]string),
	}, nil
}

// Available reports whether any supported CLI player is on PATH.
func Available() bool {
	_, ok := detectBackend()
	return ok
}

// BackendName returns the detected player's command name (e.g. "ffplay"), or ""
// if none — handy for surfacing "install mpv/ffplay" hints.
func BackendName() string {
	b, _ := detectBackend()
	return b.name
}

// previewURL resolves (and memoizes) the iTunes preview URL for a track.
func (p *Player) previewURL(ctx context.Context, itunesID int64) (string, error) {
	p.urlMu.Lock()
	if u, ok := p.urls[itunesID]; ok {
		p.urlMu.Unlock()
		return u, nil
	}
	p.urlMu.Unlock()

	url := fmt.Sprintf("https://itunes.apple.com/lookup?id=%d", itunesID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("itunes lookup: status %d", resp.StatusCode)
	}
	var body struct {
		Results []struct {
			PreviewURL string `json:"previewUrl"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if len(body.Results) == 0 || body.Results[0].PreviewURL == "" {
		return "", fmt.Errorf("no preview for itunes id %d", itunesID)
	}
	u := body.Results[0].PreviewURL
	p.urlMu.Lock()
	p.urls[itunesID] = u
	p.urlMu.Unlock()
	return u, nil
}

// cachePath is where a track's preview bytes live on disk.
func (p *Player) cachePath(itunesID int64) string {
	return filepath.Join(p.cacheDir, fmt.Sprintf("%d.m4a", itunesID))
}

// Prefetch resolves and downloads a track's preview into the cache without
// playing it. Safe to call concurrently; a no-op if already cached (a hit just
// bumps the file's mtime so the LRU keeps it).
func (p *Player) Prefetch(ctx context.Context, itunesID int64) error {
	path := p.cachePath(itunesID)
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
		return nil
	}
	u, err := p.previewURL(ctx, itunesID)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("preview download: status %d", resp.StatusCode)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	p.enforceCap(path)
	return nil
}

// enforceCap trims the preview cache to maxBytes, evicting least-recently-used
// files (oldest mtime first). keep is never evicted — it's the file we just
// wrote (or are about to play). A no-op when unlimited.
func (p *Player) enforceCap(keep string) {
	if p.maxBytes <= 0 {
		return
	}
	p.evictMu.Lock()
	defer p.evictMu.Unlock()

	entries, err := os.ReadDir(p.cacheDir)
	if err != nil {
		return
	}
	type ent struct {
		path string
		size int64
		mod  time.Time
	}
	var files []ent
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".m4a") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, ent{filepath.Join(p.cacheDir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= p.maxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= p.maxBytes {
			break
		}
		if f.path == keep {
			continue
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// Play stops any current clip and starts the given track. It blocks only long
// enough to resolve+download (cached after the first time), then returns once
// the player is running. When the clip finishes on its own, onDone is invoked
// with the same itunesID — the caller uses this to clear its "now playing" state.
func (p *Player) Play(ctx context.Context, itunesID int64, onDone func(int64)) error {
	if p.backend.name == "" {
		return fmt.Errorf("no audio player found (install ffplay, mpv, or vlc)")
	}
	if err := p.Prefetch(ctx, itunesID); err != nil {
		return err
	}
	p.Stop()

	path := p.cachePath(itunesID)
	cmd := exec.Command(p.backend.name, p.backend.args(path)...)
	if err := cmd.Start(); err != nil {
		return err
	}
	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()

	go func() {
		_ = cmd.Wait()
		p.mu.Lock()
		// Only fire onDone if this is still the active command (i.e. it ended
		// naturally rather than being killed by a newer Play/Stop).
		stillCurrent := p.cmd == cmd
		if stillCurrent {
			p.cmd = nil
		}
		p.mu.Unlock()
		if stillCurrent && onDone != nil {
			onDone(itunesID)
		}
	}()
	return nil
}

// Prune removes cached previews whose itunes id is not in `valid` — orphans
// left behind when a puzzle's track is swapped upstream. It's a no-op when
// `valid` is empty (treated as "catalogue unknown", to avoid nuking the cache).
func (p *Player) Prune(valid map[int64]bool) {
	if len(valid) == 0 {
		return
	}
	p.evictMu.Lock()
	defer p.evictMu.Unlock()

	entries, err := os.ReadDir(p.cacheDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".m4a") {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSuffix(name, ".m4a"), 10, 64)
		if err != nil {
			continue
		}
		if !valid[id] {
			_ = os.Remove(filepath.Join(p.cacheDir, name))
		}
	}
}

// Stop kills the active clip, if any.
func (p *Player) Stop() {
	p.mu.Lock()
	cmd := p.cmd
	p.cmd = nil
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// Close stops playback. The preview cache is persistent (size-capped), so it's
// intentionally left on disk for the next run.
func (p *Player) Close() {
	p.Stop()
}
