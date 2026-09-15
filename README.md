# quant-firm-simulation

A Go learning project for trading-system engineering. Currently implements
Tasks 1–3: a CLI bootstrap with paper-only configuration validation,
minimal domain contracts, and a deterministic CSV quote replay reader.

## Requirements

- Go 1.25.4 or later
- No third-party dependencies

Run these commands from the repository root.

## Build

```sh
go build -o bin/ ./cmd/paper
```

The executable is `bin/paper.exe` on Windows or `bin/paper` on other platforms.

## Test

```sh
go test ./...
go vet ./...
```

## Run

```sh
go run ./cmd/paper
```

Expected output:

```text
Starting quant-firm-simulation in PAPER mode
```

The bootstrap validates configuration, prints this message, and exits successfully.
It does not yet run a trading loop.

Configuration currently contains only `Mode`. The optional `-mode` flag defaults
to `PAPER`; only the exact value `PAPER` is accepted. Empty values and any other
mode produce an error and a nonzero exit status. There is no live execution mode.

## Domain contracts

`internal/domain` provides `Symbol`, `IntentID`, `Price`, `Quantity`, `Side`
(`Buy` and `Sell`), `Quote`, and `OrderIntent`.

`Price` is an `int64` count of $0.0001 units: `ParsePrice("100.25")` returns
1,002,500 units, and `String()` formats it as `100.2500`. Parsing accepts zero
and up to four fractional digits; it rejects signs, whitespace, malformed
values, excess precision, and overflow without rounding. This scale preserves
cents and sub-cent quote precision; exchange tick-size rules are not enforced.

Call `Validate()` on quantities, quotes, and intents before use. Quotes require
positive ordered bid/ask prices; quantities require positive whole shares.
Quotes and intents require nonblank symbols and nonzero timestamps. Intents
also require a nonblank ID and a BUY or SELL side. Timestamp validation does not
check freshness against the wall clock, so historical replay is possible.
These exported value types permit direct construction; validation is explicit.
Intent IDs represent stable strategy decisions, but deduplication is deferred.

## CSV replay

`marketdata.NewReplay(r io.Reader) (*Replay, error)` validates the exact header
`timestamp,symbol,bid,ask`. `(*Replay).Next() (domain.Quote, error)` reads one
validated quote immediately, in file order. The caller opens and closes files;
`testdata/quotes.csv` provides six sample quotes.

Timestamps use RFC3339/RFC3339Nano parsing and represent simulated market time.
Earlier timestamps are rejected; equal timestamps retain file order. There are
no timers or sleeps. Prices use `domain.ParsePrice`, and each quote is validated.
EOF returns `io.EOF`. Invalid input stops replay with a row-level error; subsequent
calls return the same error. Row numbers count CSV records with the header as
row 1; blank lines are ignored following `encoding/csv` behavior.

The CLI remains the startup-only bootstrap; replay is not wired into it yet.

See [the Phase 1 plan](outputs/phase-1-plan.md) for the implementation order.
Strategy, risk, order management, paper execution, portfolio/PnL,
integration/observability, and journal/recovery are future tasks.
