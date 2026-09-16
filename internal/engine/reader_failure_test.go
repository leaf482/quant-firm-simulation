package engine_test

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/engine"
	"github.com/leaf482/quant-firm-simulation/internal/journal"
)

// Return the exact prefix with no error, then fail on the next read instead of
// returning EOF. Keeping these separate also makes buffered read-ahead explicit.
type failingCSVReader struct {
	prefix    string
	err       error
	failures  int
	onFailure func()
}

func (r *failingCSVReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.prefix) != 0 {
		n := copy(p, r.prefix)
		r.prefix = r.prefix[n:]
		return n, nil
	}
	r.failures++
	r.onFailure()
	return 0, r.err
}

func TestReaderFailurePreservesCommittedTrade(t *testing.T) {
	for _, tt := range []struct{ name, tail string }{
		{"between_records", ""},
		{"middle_of_next_record", "2026-09-15T13:30:02Z,AAPL,102,"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config()
			path := filepath.Join(t.TempDir(), "reader-failure.jsonl")
			w, err := journal.Create(path, cfg.Symbol, cfg.InitialCash)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = w.Close() })
			cfg.Journal = w
			injected := errors.New("injected CSV input failure")
			var committedAtFailure []byte
			r := &failingCSVReader{prefix: csv("100,102", "101,103") + tt.tail, err: injected, onFailure: func() {
				// Confirm the error really occurs after a full trade is on disk,
				// not during header read-ahead before any trading has occurred.
				var err error
				committedAtFailure, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				s, err := journal.Recover(bytes.NewReader(committedAtFailure))
				if err != nil {
					t.Fatal(err)
				}
				if s.Records != 4 {
					t.Fatalf("records at injection=%d want 4", s.Records)
				}
			}}
			var logs bytes.Buffer
			got, err := engine.Run(r, cfg, log.New(&logs, "", 0))
			if !errors.Is(err, injected) || err.Error() != "engine replay: CSV row 4: injected CSV input failure" {
				t.Fatalf("Reader error was lost or misclassified: %v", err)
			}
			if r.failures != 1 || r.prefix != "" {
				t.Fatalf("injection count=%d remaining prefix=%q", r.failures, r.prefix)
			}
			want := engine.Summary{QuotesProcessed: 2, IntentsGenerated: 1, RiskApprovals: 1, OrdersCreated: 1, FillsApplied: 1}
			if got != want {
				t.Fatalf("summary=%+v want %+v", got, want)
			}
			for _, forbidden := range []string{"simulation_complete", "quote=3", `intent_id="price-movement-2"`, `order_id="order-2"`, `fill_id="fill-2"`} {
				if strings.Contains(logs.String(), forbidden) {
					t.Fatalf("unexpected later work/completion %q in %s", forbidden, logs.String())
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, committedAtFailure) {
				t.Fatal("journal changed after Reader failure")
			}
			// Only surviving bytes, not engine memory, supply recovered state.
			s, err := journal.Recover(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			stamp := time.Date(2026, 9, 15, 13, 30, 1, 0, time.UTC)
			wantOrder := domain.Order{OrderID: "order-1", IntentID: "price-movement-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Status: domain.OrderFilled, CreatedAt: stamp}
			orders := s.Orders.Orders()
			if s.Records != 4 || len(orders) != 1 || orders[0] != wantOrder {
				t.Fatalf("recovered records/orders=%d/%+v", s.Records, orders)
			}
			assertAccount := func() {
				t.Helper()
				if cash, position := s.Portfolio.State(); cash != 8970000 || position != 1 {
					t.Fatalf("cash/position=%d/%d want 8970000/1", cash, position)
				}
			}
			assertAccount()
			q := domain.Quote{Symbol: "AAPL", Timestamp: stamp.Add(time.Hour), Bid: 2000000, Ask: 2010000}
			old := wantOrder
			old.Status = domain.OrderSubmitted // Retry using a copy, not an OMS transition.
			wantFill := domain.Fill{FillID: "fill-1", OrderID: "order-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Price: 1030000, Timestamp: stamp}
			for retry := 0; retry < 2; retry++ {
				f, err := s.Broker.Execute(old, q)
				if err != nil || f != wantFill {
					t.Fatalf("remembered fill=%+v error=%v want %+v", f, err, wantFill)
				}
				if err := s.Portfolio.Apply(f); err != nil {
					t.Fatal(err)
				}
				assertAccount()
			}
			next, err := s.Orders.Create(domain.OrderIntent{IntentID: "after-recovery", Symbol: "AAPL", Side: domain.Sell, Quantity: 1, Timestamp: q.Timestamp})
			if err != nil || next.OrderID != "order-2" {
				t.Fatalf("next order=%+v error=%v", next, err)
			}
			next, err = s.Orders.Transition(next.OrderID, domain.OrderSubmitted)
			if err != nil {
				t.Fatal(err)
			}
			f, err := s.Broker.Execute(next, q)
			wantNext := domain.Fill{FillID: "fill-2", OrderID: "order-2", Symbol: "AAPL", Side: domain.Sell, Quantity: 1, Price: q.Bid, Timestamp: q.Timestamp}
			if err != nil || f != wantNext {
				t.Fatalf("next fill=%+v error=%v want %+v", f, err, wantNext)
			}
			assertAccount() // Probing IDs does not apply the new fill.
		})
	}
}
