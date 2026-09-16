package engine

import (
	"bytes"
	"errors"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/journal"
)

func sellJournal(t *testing.T, cfg *Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sell.jsonl")
	w, err := journal.Create(path, cfg.Symbol, cfg.InitialCash)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Journal = w
	t.Cleanup(func() { _ = w.Close() })
	return path
}

func recoverSellJournal(t *testing.T, path string, w *journal.Writer) *journal.State {
	t.Helper()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := journal.Recover(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Exercise retry behavior only after asserting the recovered prefix. Changing
// the retry quote distinguishes a remembered fill from a newly executed fill.
func assertSellRecovery(t *testing.T, s *journal.State, records int, buyPrice domain.Price, sellIntent domain.IntentID, sellSecond int, cash domain.Money, position domain.Quantity, durable bool) {
	t.Helper()
	if s.Records != records || len(s.Orders.Orders()) != 2 {
		t.Fatalf("records/orders=%d/%d, want %d/2", s.Records, len(s.Orders.Orders()), records)
	}
	stamp := time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)
	buy := domain.Order{OrderID: "order-1", IntentID: "price-movement-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Status: domain.OrderFilled, CreatedAt: stamp.Add(time.Second)}
	sell := domain.Order{OrderID: "order-2", IntentID: sellIntent, Symbol: "AAPL", Side: domain.Sell, Quantity: 1, Status: domain.OrderSubmitted, CreatedAt: stamp.Add(time.Duration(sellSecond) * time.Second)}
	if records == 8 {
		sell.Status = domain.OrderFilled
	}
	for _, want := range []domain.Order{buy, sell} {
		got, err := s.Orders.Get(want.OrderID)
		if err != nil || got != want {
			t.Fatalf("order=%+v error=%v want %+v", got, err, want)
		}
	}
	assertAccount := func(wantCash domain.Money, wantPosition domain.Quantity) {
		t.Helper()
		if gotCash, gotPosition := s.Portfolio.State(); gotCash != wantCash || gotPosition != wantPosition {
			t.Fatalf("account=%d/%d want %d/%d", gotCash, gotPosition, wantCash, wantPosition)
		}
	}
	assertAccount(cash, position)
	q := domain.Quote{Symbol: "AAPL", Timestamp: stamp.Add(10 * time.Second), Bid: 1, Ask: 2}
	buy.Status = domain.OrderSubmitted // Broker retries require SUBMITTED copies.
	f, err := s.Broker.Execute(buy, q)
	wantBuy := domain.Fill{FillID: "fill-1", OrderID: "order-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Price: buyPrice, Timestamp: buy.CreatedAt}
	if err != nil || f != wantBuy {
		t.Fatalf("remembered BUY=%+v error=%v want %+v", f, err, wantBuy)
	}
	if err := s.Portfolio.Apply(f); err != nil {
		t.Fatal(err)
	}
	assertAccount(cash, position)

	// Check the next OrderID. Below, the pending SELL must either allocate
	// the unused fill-2 or return the restored fill-2 without advancing IDs.
	probe, err := s.Orders.Create(domain.OrderIntent{IntentID: "probe", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Timestamp: q.Timestamp})
	if err != nil || probe.OrderID != "order-3" {
		t.Fatalf("next order=%+v error=%v", probe, err)
	}
	probe, err = s.Orders.Transition(probe.OrderID, domain.OrderSubmitted)
	if err != nil {
		t.Fatal(err)
	}
	// On the non-durable path, execute the pending SELL first: it must use
	// the new quote, proving that its original fill was not restored.
	sell.Status = domain.OrderSubmitted
	f, err = s.Broker.Execute(sell, q)
	wantSell := domain.Fill{FillID: "fill-2", OrderID: "order-2", Symbol: "AAPL", Side: domain.Sell, Quantity: 1, Price: q.Bid, Timestamp: q.Timestamp}
	if durable {
		wantSell.Price = 1000000
		wantSell.Timestamp = sell.CreatedAt
	}
	if err != nil || f != wantSell {
		t.Fatalf("SELL retry=%+v error=%v want %+v", f, err, wantSell)
	}
	for retry := 0; retry < 2; retry++ {
		if err := s.Portfolio.Apply(f); err != nil {
			t.Fatal(err)
		}
		wantCash := cash
		if !durable {
			wantCash++ // Newly executed SELL at one unit, applied exactly once.
		}
		assertAccount(wantCash, 0)
	}
	// SELL consumed fill-2 only if it was absent; otherwise it was a retry.
	if next, err := s.Broker.Execute(probe, q); err != nil || next.FillID != "fill-3" {
		t.Fatalf("next fill after SELL=%+v error=%v", next, err)
	}
}

func TestSellLiquidationCommitBoundaries(t *testing.T) {
	input := strings.Replace(boundaryCSV, "13:30:02Z,AAPL,102,104", "13:30:02Z,AAPL,100,102", 1)
	for _, tt := range []struct {
		point   string
		records int
	}{
		{"after_broker_fill", 6}, {"after_portfolio_mutation", 6},
		{"after_fill_sync", 7}, {"after_filled_sync", 8},
	} {
		t.Run(tt.point, func(t *testing.T) {
			cfg := boundaryConfig()
			path := sellJournal(t, &cfg)
			var logs bytes.Buffer
			hits := 0
			stop := errors.New("SELL interruption")
			result, err := run(strings.NewReader(input), cfg, log.New(&logs, "", 0), runHooks{at: func(point string) error {
				if point == tt.point {
					hits++
					if hits == 2 {
						return stop
					}
				}
				return nil
			}})
			if hits != 2 || !errors.Is(err, stop) {
				t.Fatalf("boundary hits=%d error=%v", hits, err)
			}
			assertStopped(t, result, err, logs.String())
			durable := tt.records >= 7
			wantFills := 1
			cash, position := domain.Money(8970000), domain.Quantity(1)
			if durable {
				wantFills, cash, position = 2, 9970000, 0
			}
			if result.OrdersCreated != 2 || result.FillsApplied != wantFills || result.RiskRejections != 0 {
				t.Fatalf("partial summary=%+v", result)
			}
			s := recoverSellJournal(t, path, cfg.Journal)
			assertSellRecovery(t, s, tt.records, 1030000, "price-movement-2", 2, cash, position, durable)
		})
	}
}

func TestJournaledPortfolioOverflowRecovery(t *testing.T) {
	cfg := boundaryConfig()
	cfg.InitialCash = math.MaxInt64
	cfg.Limits.MaxPosition = 1
	path := sellJournal(t, &cfg)
	input := "timestamp,symbol,bid,ask\n2026-09-15T13:30:00Z,AAPL,0.0001,0.0001\n2026-09-15T13:30:01Z,AAPL,0.0001,0.0003\n2026-09-15T13:30:02Z,AAPL,0.0004,0.0010\n2026-09-15T13:30:03Z,AAPL,0.0005,0.0005\n2026-09-15T13:30:04Z,AAPL,0.0006,0.0006\n"
	var logs bytes.Buffer
	// Public production path, with no fault hooks: SELL proceeds overflow cash.
	result, err := Run(strings.NewReader(input), cfg, log.New(&logs, "", 0))
	if err == nil || !strings.Contains(err.Error(), "engine apply fill") || !strings.Contains(err.Error(), "cash overflows") {
		t.Fatalf("expected real accounting failure, got %v", err)
	}
	want := Summary{QuotesProcessed: 4, IntentsGenerated: 3, RiskApprovals: 2, RiskRejections: 1, OrdersCreated: 2, FillsApplied: 1}
	if result != want || strings.Contains(logs.String(), "simulation_complete") || strings.Contains(logs.String(), "quote=5") || strings.Contains(logs.String(), `fill_id="fill-2"`) {
		t.Fatalf("unexpected continuation/completion: %+v logs=%s", result, logs.String())
	}
	s := recoverSellJournal(t, path, cfg.Journal)
	assertSellRecovery(t, s, 6, 3, "price-movement-3", 3, math.MaxInt64-3, 1, false)
}
