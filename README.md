# quant-firm-simulation

A Go learning project for trading-system engineering. Currently implements
Tasks 1–5: a CLI bootstrap with paper-only configuration validation,
minimal domain contracts, CSV quote replay, a toy strategy, and a pre-trade risk checker.

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

## Toy strategy

`strategy.NewPriceMovement() *PriceMovement` creates a single-symbol strategy.
`(*PriceMovement).OnQuote(domain.Quote) (*domain.OrderIntent, error)` validates
the quote and compares its midpoint with the previous rounded midpoint.
The first quote produces no signal; a rise produces BUY, a fall produces SELL,
and an unchanged midpoint produces no signal. Every intent has quantity 1 and
copies the quote's symbol and timestamp, then passes domain validation.

Midpoints use `bid + (ask - bid) / 2`, which avoids overflow for validated prices.
Integer division rounds down to $0.0001; sub-unit midpoint changes may therefore
produce no signal. No floating-point arithmetic is used.

IDs are `price-movement-1`, `price-movement-2`, and so on, incrementing only for
generated intents. Replaying the same sequence through a fresh instance produces
the same IDs. IDs are unique only within a run; state persistence and retry
deduplication are deferred. Sequence exhaustion returns an error instead of wrapping.

The first valid quote selects the symbol; later symbol changes are rejected.
Errors leave strategy state unchanged. Quotes are processed in caller-supplied
order; timestamp ordering remains the replay reader's responsibility.
The strategy is educational, has no position awareness, and is not connected to
replay, the CLI, or execution components.

## Pre-trade risk

`risk.NewChecker(risk.Limits) (*risk.Checker, error)` requires positive
`MaxOrderNotional` and `MaxPosition`. Call
`Check(intent, quote, availableCash, currentPosition) error` for each decision.
Cash and total notional use `domain.Price`'s $0.0001 units; position is whole
shares of the supplied symbol. Account values may be zero but not negative.

The checker validates both domain inputs and requires matching symbols.
BUY uses ask times quantity and rejects costs above cash or maximum order
notional, or a resulting position above maximum position. Equality is allowed.
SELL uses bid times quantity and rejects quantities above holdings. The BUY
limits do not prevent a SELL from reducing an existing position above limits.
Both sides reject notional overflow before multiplication. Position comparison
uses remaining capacity instead of adding quantities, avoiding addition overflow.

`nil` means approved; a contextual error describes the first rejection.
Checks do not mutate state or reserve cash/shares, so repeated approvals do not
consume resources. The caller supplies the current quote and account snapshot;
freshness, fees, reservations, and execution integration are deferred.
No additional domain arithmetic API or money library was needed.

See [the Phase 1 plan](outputs/phase-1-plan.md) for the implementation order.
Order management, paper execution, portfolio/PnL,
integration/observability, and journal/recovery are future tasks.
