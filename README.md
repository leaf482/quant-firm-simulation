# quant-firm-simulation

A Go learning project for trading-system engineering. Currently implements
Tasks 1–10: a synchronous PAPER simulation with optional durable JSONL trading
journals and read-only recovery inspection. Strategy/CSV resume is not supported.

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
go test -race ./...
```

## Run

```sh
go run ./cmd/paper
go run ./cmd/paper -csv testdata/quotes.csv -symbol AAPL -initial-cash 1000 -max-order-notional 500 -max-position 10
```

Expected output:

```text
PAPER summary: quotes=6 intents=5 approvals=5 rejections=0 orders=5 fills=5 cash=770.7700 position=1 equity=999.8700 pnl=-0.1300
```

The second command spells out the development defaults. Monetary flags are dollar
strings with at most four decimals; quantity is an integer. `-mode` accepts only
`PAPER`. Invalid configuration, files, replay data, or system errors exit nonzero.
Event logs go to stderr; the successful final summary goes to stdout. Logs omit
wall-clock prefixes and include quote sequence and intent/order/fill IDs.

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

The CLI opens the CSV; the engine consumes it to EOF through this reader.

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
The strategy is educational and has no position awareness. The engine supplies
quotes and routes generated intents through risk before creating orders.

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
freshness, fees, and reservations are deferred. `*risk.Rejection` identifies an
expected cash/holdings/limit denial; other errors are fatal to the engine.
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
There is no persistence, reservation handling, or risk recheck inside OMS.

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
The engine applies the returned fill and requests the OMS transition to FILLED.

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
Optional journal and recovery behavior is documented below.

## Engine

`engine.Run(io.Reader, engine.Config, *log.Logger) (engine.Summary, error)` owns
fresh concrete components per run. Config contains Symbol, InitialCash, and risk
Limits, plus an optional fresh `*journal.Writer`. A nil logger discards logs.
The caller closes the input reader and journal writer.

For each quote: strategy -> current portfolio State -> risk -> OMS Create ->
SUBMITTED -> paper execution using the same quote -> portfolio Apply -> FILLED.
No signal skips execution. Expected risk denials are logged and counted without
creating orders. Every other error stops the run immediately. Each order settles
before the next quote; there are no outstanding orders or reservations.

The summary counts quotes, intents, approvals, rejections, orders, and applied
fills, plus final cash, position, equity, and PnL. The final valid quote supplies
the bid mark, even if it produced no signal or a rejected intent. Empty input or
a header without quotes is an error. Failed runs return partial counters only;
final account fields must not be interpreted as a completed valuation.

The end-to-end test rejects an initial SELL, then buys at 103 and 105, sells at
102, and marks the remaining share at 101. From 1000 initial cash it asserts:
6 quotes, 4 intents, 3 approvals, 1 rejection, 3 orders/fills, cash 894, position 1,
equity 995, PnL -5. Two independent runs must produce identical results and logs.
Fatal errors do not roll back earlier committed records. Trading-state recovery
is available with a journal, but simulation resume is not supported.

## Durable journal and recovery

New run (the journal path must not exist; its parent directory must exist):

```sh
go run ./cmd/paper -csv testdata/quotes.csv -journal run.jsonl
```

Read-only recovery inspection, without CSV input or external account settings:

```sh
go run ./cmd/paper -recover run.jsonl
```

For the default fixture this prints `records=20 orders=5 cash=770.7700 position=1`
and the five FILLED orders. `-recover` cannot be combined with `-journal` or run
configuration flags (except `-mode PAPER`). It never writes or resumes the file.
Without `-journal`, runs retain the existing in-memory behavior.

`journal.Create(path, symbol, initialCash)` uses exclusive creation; an existing
file is never overwritten or reopened for append. `Writer.Append(Event)` writes
one JSON line and calls `File.Sync()` before success. `Writer.Close()` closes the
file. `journal.Read(io.Reader)` validates records; `journal.Recover(io.Reader)`
returns fresh OMS, portfolio, and paper broker components, or no state on error.

Each line is a schema v2 envelope with exactly two fields: `record` and `crc32c`.
`record` contains `version: 2`, a consecutive `sequence` starting at 1, `symbol`,
`initial_cash` (integer $0.0001 units), and `event`, containing one of:

1. `order_created`: full NEW order
2. `order_state_changed`: order ID and target status
3. `fill_applied`: full fill

`crc32c` is an unsigned numeric CRC32C/Castagnoli checksum of `json.Marshal(record)`.
The fixed Record/Event/domain structs determine field order. All recovery data,
including version, sequence, account metadata, event type and payload, is covered;
the checksum field and trailing newline are excluded. Reading first validates
structure, decodes the typed record, then marshals that same struct layout and
verifies its checksum before any replay. Whitespace and JSON member order do not
affect this canonical checksum. CRC detects accidental corruption, not malicious
modification by someone who can recompute it. Old v1 journals are rejected;
automatic migration is not implemented.

The journal-local JSON validator requires every envelope, record and event payload
field, rejects explicit null, duplicate keys (including escaped spellings of the
same key), unknown fields and case variants. Explicit numeric `initial_cash: 0`
remains valid. Structure is checked independently of CRC; a recomputed checksum
cannot make missing/null/duplicate fields acceptable. Any failure returns no
recovered state.

Account metadata is repeated so recovery requires only the journal. Metadata
must agree across all records. Empty journals (including runs with no trades)
have no account metadata to recover and are rejected explicitly. No quote marks,
strategy state, CSV cursor, or run summary is persisted. Recovered equity/PnL
requires a caller-supplied valid quote; CLI inspection reports cash/holdings only.

### Exact commit order

For each approved intent, the engine validates/applies each operation to its
private, tentative in-memory state, then writes and syncs its record before any
dependent processing, committed counter, or corresponding event log:

1. Create NEW order -> append/sync `order_created`.
2. Transition SUBMITTED -> append/sync `order_state_changed`.
3. Execute and apply fill -> append/sync `fill_applied`.
4. Transition FILLED -> append/sync `order_state_changed`.

Existing component validation happens before journaling, so failed operations
are not recorded as valid history. A write/sync failure immediately halts the
run, discards its private components, and poisons the writer. It is never reported
as committed. A failed Sync has an uncertain disk outcome: a complete record may
still survive and be recovered; callers must not retry on that writer. Sync
success guarantees the requested file flush, subject to OS/storage guarantees.
This targets process termination, not filesystem namespace loss on power failure.

Recovery reuses OMS creation/transitions, portfolio application, and broker fill
restoration. It rejects sequence gaps/duplicates, unknown schema fields/types,
invalid domain values, missing orders, conflicting IDs, and impossible lifecycle
histories (including FILLED without an applied fill). A complete prefix may end
at NEW, SUBMITTED, or after a fill but before FILLED; that exact state is preserved.
Malformed JSON and an unterminated final line fail recovery; no tail repair occurs.

OMS ID sequences are rebuilt through ordered Create calls; `Broker.Restore`
rebuilds fill sequences and execution deduplication. `Manager.Get` and `Orders`
return copies for inspection. A process-termination test persists two orders/fills,
exits without closing the journal, then recovers cash 997, position 0, equity 997,
PnL -3 and verifies next IDs `order-3` / `fill-3`.

### Commit-boundary regression tests

Private engine hooks interrupt actual component operations; the public Run API
and CLI cannot enable them. Tests first commit a BUY at 103 from cash 1000, leaving
order-1 FILLED, cash 897, one share and remembered fill-1 (four records). A second
BUY at 104 is interrupted at each boundary. Recovery uses only disk bytes:

- Before NEW or after NEW mutation: second order absent; four records.
- After NEW sync or SUBMITTED mutation: second order NEW; five records.
- After SUBMITTED sync, broker fill creation, or portfolio mutation: second order
  SUBMITTED, second fill absent; six records, cash 897, one share.
- After fill sync or FILLED mutation: second order SUBMITTED, fill-2 remembered;
  seven records, cash 793, two shares.
- After FILLED sync: second order FILLED; eight records, cash 793, two shares.

Cash/holdings remain 897/1 until record seven. Next OrderID is order-2 at four
records, otherwise order-3; next FillID is fill-2 before seven records, otherwise
fill-3. Tests check exact orders/fills, retry idempotency, no later quote/order,
and no successful completion report at every boundary.

At each of the four appends in the second trade, tests model a partial write
(torn tail, no recovery state), or a complete write followed by Sync failure
with either the full record surviving or none of its bytes surviving. Only the
surviving prefix determines recovered state. The internal append hook installs
these explicit disk fixtures; separate Writer tests inject Write/Sync failures
and verify writer poisoning and acknowledgement behavior. These are deterministic
fault simulations, not a claim to reproduce physical power-loss behavior.

No database, snapshots, compaction, strategy recovery, automated resume, concurrent
writers, or distributed coordination is implemented. Runtime journals are local
artifacts and should not be committed.
