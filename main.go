// Command ac-term is a terminal build of Audio Connections — the daily
// group-four-songs-by-ear puzzle from connections.audio — rendered with
// Bubbletea and a cassette-deck color scheme.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/klobucar/ac-term/internal/audio"
	"github.com/klobucar/ac-term/internal/data"
	"github.com/klobucar/ac-term/internal/remote"
	"github.com/klobucar/ac-term/internal/save"
	"github.com/klobucar/ac-term/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// progSender adapts *tea.Program to ui.Sender. The program isn't constructed
// until after the model, so the pointer is filled in just before Run.
type progSender struct{ p *tea.Program }

func (s *progSender) Send(msg tea.Msg) {
	if s.p != nil {
		s.p.Send(msg)
	}
}

// version is stamped at release time by GoReleaser (-X main.version=...).
var version = "dev"

func main() {
	// Verb subcommands handle their own args (and stdin), so dispatch them
	// before flag parsing — same pattern as `go build` / `go test`.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help":
			usage(os.Stdout)
			return
		case "version":
			fmt.Printf("ac-term %s\n", version)
			return
		case "export":
			doExport()
			return
		case "import":
			doImport(os.Args[2:])
			return
		}
	}

	f := parseFlags(os.Args[1:]) // handles -h/--help, -v/--version, hidden -a/-b
	if f.version {
		fmt.Printf("ac-term %s\n", version)
		return
	}

	now := time.Now()
	unlocked := f.all || envSet("AC_UNLOCK_ALL")
	withBacklog := f.backlog || envSet("AC_BACKLOG")

	// Start from the freshest catalogue we already have on disk; fall back to
	// the embedded snapshot. The network refresh happens in the background so
	// the UI is up instantly and works fully offline.
	cat, _, cached := remote.Cached()
	if !cached {
		cat = embedCatalogue()
	}
	puzzles := buildPlayable(cat, now, unlocked, withBacklog)

	if len(puzzles) == 0 {
		// No cache and an empty embed (a bare `go build` without `make data`):
		// fetch the feed synchronously so there's something to play.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if fresh, _, err := remote.Refresh(ctx); err == nil {
			cat, cached = fresh, true
			puzzles = buildPlayable(cat, now, unlocked, withBacklog)
		}
		cancel()
	}
	if len(puzzles) == 0 {
		fmt.Fprintln(os.Stderr, "ac-term: no puzzles available — a network connection is needed on first run")
		os.Exit(1)
	}

	var player *audio.Player
	if audio.Available() {
		p, err := audio.NewPlayer()
		if err == nil {
			player = p
			defer player.Close()
		}
	}

	// Sweep orphaned previews (ids no longer in any puzzle) against what we know
	// at startup; refresh() sweeps again if the feed brings changes.
	if player != nil {
		go player.Prune(itunesIDSet(cat))
	}

	sender := &progSender{}
	model := ui.New(puzzles, player, sender)
	prog := tea.NewProgram(model) // alt-screen is set on the View in v2
	sender.p = prog

	go refresh(prog, now, cached, unlocked, withBacklog, player)

	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ac-term: %v\n", err)
		os.Exit(1)
	}
}

// embedCatalogue is the offline floor: the baked scheduled + backlog snapshots.
func embedCatalogue() remote.Catalogue {
	return remote.Catalogue{Puzzles: data.All(), Backlog: data.Backlog()}
}

// itunesIDSet is every iTunes id referenced by the catalogue (scheduled +
// backlog) — the allow-list for preview-cache pruning.
func itunesIDSet(cat remote.Catalogue) map[int64]bool {
	s := map[int64]bool{}
	for _, p := range cat.Puzzles {
		for _, t := range p.Tracks {
			s[t.ItunesID] = true
		}
	}
	for _, p := range cat.Backlog {
		for _, t := range p.Tracks {
			s[t.ItunesID] = true
		}
	}
	return s
}

// buildPlayable assembles the picker list: released (or all) scheduled days,
// with the unscheduled backlog appended when requested.
func buildPlayable(cat remote.Catalogue, now time.Time, unlocked, withBacklog bool) []data.Puzzle {
	out := releasedOr(cat.Puzzles, now, unlocked)
	if withBacklog {
		out = append(out, cat.Backlog...)
	}
	return out
}

// doExport prints a transfer string of finished days to stdout.
func doExport() {
	all := save.LoadAll()
	recs := make([]save.Record, 0, len(all))
	for _, r := range all {
		recs = append(recs, r)
	}
	b64 := save.EncodeBackup(recs, time.Now().UTC().Format(time.RFC3339))
	fmt.Println(b64)
}

// doImport reads a transfer string (from args or stdin) and merges its days
// into saved progress. `--replace` wipes existing progress first.
func doImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	replace := fs.Bool("replace", false, "wipe existing progress before importing")
	_ = fs.Parse(args)

	str := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if str == "" { // no inline string — read it from stdin
		raw, _ := io.ReadAll(os.Stdin)
		str = strings.TrimSpace(string(raw))
	}

	days, err := save.DecodeBackup(str)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ac-term: import failed: %v\n", err)
		os.Exit(1)
	}

	// Map day number -> puzzle so we can resolve the save id + track count.
	cat, _, ok := remote.Cached()
	if !ok {
		cat = embedCatalogue()
	}
	byDay := map[int]data.Puzzle{}
	for _, p := range cat.Puzzles {
		byDay[p.Day] = p
	}

	if *replace {
		_ = save.Clear()
	}
	imported, skipped := 0, 0
	for _, d := range days {
		p, ok := byDay[d.Day]
		if !ok {
			skipped++ // a day this build doesn't know about
			continue
		}
		if err := save.Put(save.Materialize(d, p.ID, len(p.Tracks))); err != nil {
			fmt.Fprintf(os.Stderr, "ac-term: failed to write day %d: %v\n", d.Day, err)
			os.Exit(1)
		}
		imported++
	}
	msg := fmt.Sprintf("imported %d day(s)", imported)
	if skipped > 0 {
		msg += fmt.Sprintf(", skipped %d unknown", skipped)
	}
	if *replace {
		msg = "replaced progress — " + msg
	}
	fmt.Println(msg)
}

// cliFlags are the top-level boolean flags.
type cliFlags struct{ version, all, backlog bool }

// parseFlags parses the non-verb flags. --all/-a and --backlog/-b are
// deliberately HIDDEN: registered so they work, but omitted from usage() (they
// reveal unreleased/unscheduled answers). AC_UNLOCK_ALL / AC_BACKLOG mirror them.
func parseFlags(args []string) cliFlags {
	var f cliFlags
	fs := flag.NewFlagSet("ac-term", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { usage(os.Stderr) }
	fs.BoolVar(&f.version, "version", false, "print version and exit")
	fs.BoolVar(&f.version, "v", false, "print version and exit")
	fs.BoolVar(&f.all, "all", false, "")
	fs.BoolVar(&f.all, "a", false, "")
	fs.BoolVar(&f.backlog, "backlog", false, "")
	fs.BoolVar(&f.backlog, "b", false, "")
	_ = fs.Parse(args) // ExitOnError handles bad flags / -h
	return f
}

// usage prints the public help. Hidden flags are intentionally absent.
func usage(w io.Writer) {
	fmt.Fprint(w, `ac-term — terminal Audio Connections

Usage:
  ac-term                play
  ac-term version        print version
  ac-term export         print a backup string (interchangeable with connections.audio)
  ac-term import [str]   import a backup (reads stdin if omitted; --replace wipes first)

Env:
  AC_PUZZLES_URL         override the puzzle catalogue source
`)
}

// envSet reports whether an env var is set to a non-empty, non-"0" value.
func envSet(name string) bool {
	v := os.Getenv(name)
	return v != "" && v != "0"
}

// releasedOr returns the full catalogue when unlocked, else only released days.
func releasedOr(ps []data.Puzzle, now time.Time, unlocked bool) []data.Puzzle {
	if unlocked {
		return ps
	}
	return data.OnlyReleased(ps, now)
}

// refresh pulls the latest catalogue and, if anything changed (or we started
// from the embed), hands the playable set to the running program.
func refresh(prog *tea.Program, now time.Time, hadCache, unlocked, withBacklog bool, player *audio.Player) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	fresh, updated, err := remote.Refresh(ctx)
	if err != nil {
		return // offline or source down — the cache/embed already loaded
	}
	if updated || !hadCache {
		prog.Send(ui.CatalogueMsg{Puzzles: buildPlayable(fresh, now, unlocked, withBacklog)})
		// Re-sweep orphans against the freshest catalogue.
		if player != nil {
			player.Prune(itunesIDSet(fresh))
		}
	}
}
