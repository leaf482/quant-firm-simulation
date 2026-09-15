# quant-firm-simulation

A Go learning project for trading-system engineering. Currently implements
Task 1 only: a CLI bootstrap with paper-only configuration validation.

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

See [the Phase 1 plan](outputs/phase-1-plan.md) for the implementation order.
Market data, strategy, risk, order management, paper execution, portfolio/PnL,
integration/observability, and journal/recovery are future tasks.
