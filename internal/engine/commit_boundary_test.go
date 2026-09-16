package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/journal"
	"github.com/leaf482/quant-firm-simulation/internal/risk"
)

const boundaryCSV = "timestamp,symbol,bid,ask\n2026-09-15T13:30:00Z,AAPL,100,102\n2026-09-15T13:30:01Z,AAPL,101,103\n2026-09-15T13:30:02Z,AAPL,102,104\n2026-09-15T13:30:03Z,AAPL,103,105\n"

func boundaryConfig() Config {
	return Config{Symbol: "AAPL", InitialCash: 10000000, Limits: risk.Limits{MaxOrderNotional: 5000000, MaxPosition: 10}}
}

func assertStopped(t *testing.T, result Summary, err error, logs string) {
	t.Helper()
	if err == nil {
		t.Fatal("injected failure reported success")
	}
	if result.QuotesProcessed != 3 || result.IntentsGenerated != 2 || result.RiskApprovals != 2 {
		t.Fatalf("processed beyond failing quote or stopped too early: %+v", result)
	}
	if result.FinalEquity != 0 || strings.Contains(logs, "simulation_complete") || strings.Contains(logs, "quote=4") || strings.Contains(logs, `order_id="order-3"`) {
		t.Fatalf("run reported completion/later work: %s", logs)
	}
}

// Only durable bytes enter these assertions; engine components are inaccessible
// after run returns. The first completed trade is a baseline, so every boundary
// can be recovered and checked, including failure before the second NEW record.
func assertRecoveredPrefix(t *testing.T, data []byte, records int) {
	t.Helper()
	state, err := journal.Recover(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if state.Records != records {
		t.Fatalf("records=%d want %d", state.Records, records)
	}
	orderCount := 1
	if records >= 5 {
		orderCount = 2
	}
	if len(state.Orders.Orders()) != orderCount {
		t.Fatal("wrong order count")
	}
	stamp := time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)
	for n := 1; n <= orderCount; n++ {
		status := domain.OrderFilled
		if n == 2 {
			status = domain.OrderNew
			if records >= 6 {
				status = domain.OrderSubmitted
			}
			if records >= 8 {
				status = domain.OrderFilled
			}
		}
		id := domain.OrderID(fmt.Sprintf("order-%d", n))
		got, err := state.Orders.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		want := domain.Order{OrderID: id, IntentID: domain.IntentID(fmt.Sprintf("price-movement-%d", n)), Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Status: status, CreatedAt: stamp.Add(time.Duration(n) * time.Second)}
		if got != want {
			t.Fatalf("order=%+v want %+v", got, want)
		}
	}
	cash, position := state.Portfolio.State()
	wantCash, wantPosition := domain.Money(8970000), domain.Quantity(1)
	if records >= 7 {
		wantCash, wantPosition = 7930000, 2
	}
	if cash != wantCash || position != wantPosition {
		t.Fatalf("cash/position=%s/%d want %s/%d", cash, position, wantCash, wantPosition)
	}
	q := domain.Quote{Symbol: "AAPL", Timestamp: stamp.Add(10 * time.Second), Bid: 9990000, Ask: 10000000}
	// Already remembered fills must retain the original price and simulated time.
	remembered := 1
	if records >= 7 {
		remembered = 2
	}
	for n := 1; n <= remembered; n++ {
		o, _ := state.Orders.Get(domain.OrderID(fmt.Sprintf("order-%d", n)))
		o.Status = domain.OrderSubmitted
		f, err := state.Broker.Execute(o, q)
		want := domain.Fill{FillID: domain.FillID(fmt.Sprintf("fill-%d", n)), OrderID: o.OrderID, Symbol: o.Symbol, Side: o.Side, Quantity: o.Quantity, Price: domain.Price(1020000 + n*10000), Timestamp: stamp.Add(time.Duration(n) * time.Second)}
		if err != nil || f != want {
			t.Fatalf("remembered fill=%+v %v want %+v", f, err, want)
		}
		if err := state.Portfolio.Apply(f); err != nil {
			t.Fatal(err)
		}
	}
	if afterCash, afterPosition := state.Portfolio.State(); afterCash != cash || afterPosition != position {
		t.Fatal("recovered fill idempotency failed")
	}
	// Verify both next IDs without consuming an uncommitted pending order first.
	o, err := state.Orders.Create(domain.OrderIntent{IntentID: "new-after-recovery", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Timestamp: q.Timestamp})
	if err != nil {
		t.Fatal(err)
	}
	if o.OrderID != domain.OrderID(fmt.Sprintf("order-%d", orderCount+1)) {
		t.Fatal("next OrderID collision")
	}
	o, err = state.Orders.Transition(o.OrderID, domain.OrderSubmitted)
	if err != nil {
		t.Fatal(err)
	}
	f, err := state.Broker.Execute(o, q)
	if err != nil || f.FillID != domain.FillID(fmt.Sprintf("fill-%d", remembered+1)) {
		t.Fatalf("next FillID=%s %v", f.FillID, err)
	}
	// An execution made only in discarded memory must not have been remembered.
	if records >= 5 && records < 7 {
		fresh, err := journal.Recover(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		o, err := fresh.Orders.Transition("order-2", domain.OrderSubmitted)
		if err != nil {
			t.Fatal(err)
		}
		f, err := fresh.Broker.Execute(o, q)
		if err != nil || f.FillID != "fill-2" || f.Price != q.Ask || !f.Timestamp.Equal(q.Timestamp) {
			t.Fatal("uncommitted execution leaked through recovery")
		}
	}
}

func TestActualCommitBoundaries(t *testing.T) {
	for _, tt := range []struct {
		boundary string
		records  int
	}{
		{"before_new", 4}, {"after_new_mutation", 4}, {"after_new_sync", 5},
		{"after_submitted_mutation", 5}, {"after_submitted_sync", 6},
		{"after_broker_fill", 6}, {"after_portfolio_mutation", 6},
		{"after_fill_sync", 7}, {"after_filled_mutation", 7}, {"after_filled_sync", 8},
	} {
		t.Run(tt.boundary, func(t *testing.T) {
			cfg := boundaryConfig()
			path := filepath.Join(t.TempDir(), "run.jsonl")
			w, err := journal.Create(path, cfg.Symbol, cfg.InitialCash)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Journal = w
			var logs bytes.Buffer
			hits := 0
			hooks := runHooks{at: func(point string) error {
				if point == tt.boundary {
					hits++
					if hits == 2 {
						return errors.New("injected interruption")
					}
				}
				return nil
			}}
			result, err := run(strings.NewReader(boundaryCSV), cfg, log.New(&logs, "", 0), hooks)
			if hits != 2 {
				t.Fatal("requested boundary not reached")
			}
			assertStopped(t, result, err, logs.String())
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertRecoveredPrefix(t, data, tt.records)
		})
	}
}

// A wire fixture encodes the same deterministic struct layout. The engine seam
// substitutes only the failed append; every preceding append uses the real
// journal Writer. Journal unit tests separately inject failures into Write/Sync.
func failureLine(t *testing.T, sequence uint64, event journal.Event) []byte {
	t.Helper()
	record := journal.Record{Version: 2, Sequence: sequence, Symbol: "AAPL", InitialCash: 10000000, Event: event}
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(struct {
		Record journal.Record `json:"record"`
		CRC32C uint32         `json:"crc32c"`
	}{record, crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli))})
	if err != nil {
		t.Fatal(err)
	}
	return append(line, '\n')
}

func TestEngineAppendFailureOutcomes(t *testing.T) {
	for _, sequence := range []uint64{5, 6, 7, 8} {
		for _, outcome := range []string{"partial", "sync_bytes_survive", "sync_bytes_absent"} {
			t.Run(fmt.Sprintf("record_%d_%s", sequence, outcome), func(t *testing.T) {
				cfg := boundaryConfig()
				path := filepath.Join(t.TempDir(), "run.jsonl")
				w, err := journal.Create(path, cfg.Symbol, cfg.InitialCash)
				if err != nil {
					t.Fatal(err)
				}
				cfg.Journal = w
				var logs bytes.Buffer
				calls := uint64(0)
				hooks := runHooks{append: func(event journal.Event) error {
					calls++
					if calls != sequence {
						return w.Append(event)
					}
					line := failureLine(t, sequence, event)
					// Close the real writer before installing the surviving disk fixture.
					if err := w.Close(); err != nil {
						return err
					}
					f, err := os.OpenFile(path, os.O_WRONLY, 0600)
					if err != nil {
						return err
					}
					defer f.Close()
					prefixSize, err := f.Seek(0, io.SeekEnd)
					if err != nil {
						return err
					}
					if outcome == "partial" {
						line = line[:len(line)/2]
					}
					if _, err := f.Write(line); err != nil {
						return err
					}
					if outcome == "sync_bytes_absent" {
						if err := f.Truncate(prefixSize); err != nil {
							return err
						}
					}
					if err := f.Sync(); err != nil {
						return err
					} // Stabilize the chosen crash outcome.
					if outcome == "partial" {
						return io.ErrShortWrite
					}
					return errors.New("injected Sync failure; survival determined by fixture")
				}}
				result, err := run(strings.NewReader(boundaryCSV), cfg, log.New(&logs, "", 0), hooks)
				assertStopped(t, result, err, logs.String())
				if calls != sequence {
					t.Fatal("attempted later append")
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if outcome == "partial" {
					if state, err := journal.Recover(bytes.NewReader(data)); err == nil || state != nil {
						t.Fatal("torn record must fail closed")
					}
					return
				}
				count := int(sequence) - 1
				if outcome == "sync_bytes_survive" {
					count++
				}
				assertRecoveredPrefix(t, data, count)
			})
		}
	}
}
