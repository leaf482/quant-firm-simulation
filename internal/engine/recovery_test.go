package engine_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/engine"
	"github.com/leaf482/quant-firm-simulation/internal/journal"
)

// The helper process deliberately exits without closing its journal handle.
func TestJournalProcess(t *testing.T) {
	path := os.Getenv("PAPER_JOURNAL_TEST_PATH")
	if path == "" {
		return
	}
	cfg := config()
	w, err := journal.Create(path, cfg.Symbol, cfg.InitialCash)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Journal = w
	summary, err := engine.Run(strings.NewReader(csv("100,102", "101,103", "100,102")), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestProcessRestartRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.jsonl")
	cmd := exec.Command(os.Args[0], "-test.run=^TestJournalProcess$")
	cmd.Env = append(os.Environ(), "PAPER_JOURNAL_TEST_PATH="+path)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var before engine.Summary
	if err := json.Unmarshal(output, &before); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := journal.Recover(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 9, 15, 13, 30, 2, 0, time.UTC)
	q := domain.Quote{Symbol: "AAPL", Timestamp: stamp, Bid: 1000000, Ask: 1020000}
	snapshot, err := state.Portfolio.Snapshot(q)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Cash != before.FinalCash || snapshot.Position != before.FinalPosition || snapshot.Equity != before.FinalEquity || snapshot.PnL != before.FinalPnL {
		t.Fatalf("restart mismatch: %+v vs %+v", snapshot, before)
	}
	if snapshot.Cash != 9970000 || snapshot.Position != 0 || state.Records != 8 {
		t.Fatalf("unexpected recovered state %+v", snapshot)
	}
	orders := state.Orders.Orders()
	if len(orders) != 2 {
		t.Fatal("orders missing")
	}
	for _, o := range orders {
		if o.Status != domain.OrderFilled {
			t.Fatal("order status not restored")
		}
	}
	order, err := state.Orders.Create(domain.OrderIntent{IntentID: "after-recovery", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Timestamp: stamp})
	if err != nil {
		t.Fatal(err)
	}
	if order.OrderID != "order-3" {
		t.Fatalf("next ID=%s", order.OrderID)
	}
	order, err = state.Orders.Transition(order.OrderID, domain.OrderSubmitted)
	if err != nil {
		t.Fatal(err)
	}
	f, err := state.Broker.Execute(order, q)
	if err != nil || f.FillID != "fill-3" {
		t.Fatalf("next fill=%+v %v", f, err)
	}
	// Restored broker deduplication also survives restart.
	old := orders[0]
	old.Status = domain.OrderSubmitted
	f, err = state.Broker.Execute(old, q)
	if err != nil || f.FillID != "fill-1" || f.Price != 1030000 {
		t.Fatalf("old fill=%+v %v", f, err)
	}
}

func TestJournalFailureDoesNotReportSuccess(t *testing.T) {
	cfg := config()
	w, err := journal.Create(filepath.Join(t.TempDir(), "run.jsonl"), cfg.Symbol, cfg.InitialCash)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Journal = w
	got, err := engine.Run(strings.NewReader(csv("1,2", "2,3")), cfg, nil)
	if err == nil || got.OrdersCreated != 0 || got.FillsApplied != 0 || got.FinalEquity != 0 {
		t.Fatalf("failed journal reported success: %+v %v", got, err)
	}
}
