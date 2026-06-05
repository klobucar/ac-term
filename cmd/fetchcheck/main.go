// Command fetchcheck exercises the remote refresh + cache path. Dev-only.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/klobucar/ac-term/internal/remote"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("URL:", remote.URL())
	cat, updated, err := remote.Refresh(ctx)
	if err != nil {
		fmt.Println("Refresh error:", err)
	} else {
		fmt.Printf("Refresh ok: %d puzzles + %d backlog, updated=%v (day1=%s, lastDay=%d)\n",
			len(cat.Puzzles), len(cat.Backlog), updated, cat.Puzzles[0].ID, cat.Puzzles[len(cat.Puzzles)-1].Day)
		if len(cat.Backlog) > 0 {
			fmt.Printf("backlog ids: ")
			for _, b := range cat.Backlog {
				fmt.Printf("%s ", b.ID)
			}
			fmt.Println()
		}
	}

	cc, fetchedAt, ok := remote.Cached()
	fmt.Printf("Cached: ok=%v puzzles=%d backlog=%d fetchedAt=%s\n",
		ok, len(cc.Puzzles), len(cc.Backlog), fetchedAt.Format(time.RFC3339))
}
