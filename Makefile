# ac-term — build + maintenance tasks.

# The published puzzle feed (the live site). Override: make data FEED=...
FEED  ?= https://connections.audio/api/v0/puzzle.json
EMBED := internal/data/feed/puzzle.json

.PHONY: build run data vet tidy clean

# build/run depend on `data`, so a `make` build always embeds the live feed.
# A bare `go build` (no make) compiles with an empty embed and fetches at runtime.
build: data
	go build -o ac-term .

run: data
	go run .

# data: download the feed into the (gitignored) embed file.
data:
	@curl -fsSL $(FEED) -o $(EMBED) && \
		echo "updated $(EMBED) ($$(wc -c < $(EMBED) | tr -d ' ') bytes)"

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f ac-term
