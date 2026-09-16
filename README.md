# quant-firm-simulation

A Go learning project for trading-system engineering. Currently implements
Tasks 1–8: a CLI bootstrap with paper-only configuration validation,
domain contracts, CSV replay, a toy strategy, risk checks, an OMS, a paper broker,
and an in-memory portfolio. Components are not integrated yet.

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
Cash and `MaxOrderNotional` use `domain.Money` in $0.0001 units; position is whole
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
The risk checker uses the shared overflow-checked `domain.Notional` helper.

## Order management

`oms.NewManager() *Manager` creates a single-threaded in-memory manager.
`Create(domain.OrderIntent) (domain.Order, error)` assumes prior risk approval,
validates the intent and new order, and creates `order-1`, `order-2`, etc. in NEW
state. Orders copy the intent's ID, symbol, side, quantity, and timestamp.
The new domain types are `OrderID`, `OrderStatus`, and `Order`; `Order.Validate()`
and `OrderStatus.Validate()` reject invalid values.

`Transition(domain.OrderID, domain.OrderStatus) (domain.Order, error)` allows:

- NEW -> SUBMITTED or REJECTED
- SUBMITTED -> CANCELLED, REJECTED, or FILLED
- Any state -> itself (idempotent success)

Unknown order IDs, unsupported statuses, and other transitions return errors.
SUBMITTED is a recorded lifecycle state only; no broker submission takes place.

Repeated creation with the same IntentID and payload returns the existing order
in its current state. A different symbol, side, quantity, or timestamp for that
ID is rejected without mutation; timestamps are compared as instants.
Returned orders are copies. IDs are local to the manager run; exhaustion fails
without wrapping. Invalid requests do not consume IDs or change managed orders.
There is no persistence, reservation handling, risk recheck, or component wiring.

## Paper broker

`paper.NewBroker() *Broker` creates a single-threaded in-memory broker.
`Execute(domain.Order, domain.Quote) (domain.Fill, error)` requires a valid
SUBMITTED order and valid matching-symbol quote. BUY fills the entire quantity
at ask; SELL fills at bid. Fill time is the quote timestamp. There are no fees,
slippage, partial fills, timers, or next-quote scheduling.

`domain.FillID`, `domain.Fill`, and `Fill.Validate()` describe and validate the
execution. IDs are `fill-1`, `fill-2`, etc., unique within one broker run.
Matching retries return the original fill, even if the supplied quote changed.
Reuse of an OrderID with different IntentID, symbol, side, quantity, or creation
instant is rejected. Duplicate calls do not advance IDs. All calls, including
retries, require valid inputs and SUBMITTED status; FILLED orders are rejected.

The broker returns copies and never calls the OMS or changes account state.
The future caller is responsible for applying the returned fill and requesting
the OMS transition to FILLED. Components are not wired together yet.

## Portfolio and money

`domain.Money` is a distinct `int64` type for monetary balances, values, and PnL,
using the same $0.0001 scale as `Price`. It can represent negative PnL; portfolio
cash is constrained to nonnegative values. `Money.String()` formats four decimal
places. `domain.Notional(price Price, quantity Quantity) (Money, error)` safely
multiplies a positive price by a nonnegative whole-share quantity. Zero quantity
supports an empty position; overflow is rejected before multiplication.

`portfolio.New(symbol domain.Symbol, initialCash domain.Money) (*Portfolio, error)`
requires a nonblank symbol and nonnegative cash. `Apply(domain.Fill) error` validates
the fill and symbol, then updates cash and holdings atomically:

- BUY: subtract price times quantity from cash and add shares; reject insufficient cash.
- SELL: add proceeds to cash and subtract shares; reject insufficient holdings.

All arithmetic is checked before state changes. Each FillID is applied at most
once. Matching retries succeed without mutation; different order ID, symbol,
side, quantity, price, or timestamp for an existing FillID is rejected. Timestamps
are compared as instants. Failed fills are not recorded as applied.

`Snapshot(domain.Quote) (Snapshot, error)` validates the quote and matching symbol,
then returns `Cash`, `Position`, `MarketValue`, `Equity`, and `PnL` without mutation:

```text
MarketValue = quote.Bid * Position
Equity      = Cash + MarketValue
PnL         = Equity - initialCash
```

Overflow returns an error. Quote time is simulated; snapshots do not use the wall
clock or store a mark. There is no cost basis, realized/unrealized split, fees,
persistence, or calls to risk, OMS, or broker. Callers select the current quote.

See [the Phase 1 plan](outputs/phase-1-plan.md) for the implementation order.
Integration/observability and journal/recovery are future tasks.
