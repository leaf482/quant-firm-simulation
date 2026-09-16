package engine_test

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"strings"
	"testing"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/engine"
	"github.com/leaf482/quant-firm-simulation/internal/risk"
)

func config() engine.Config {
	return engine.Config{Symbol: "AAPL", InitialCash: 10000000, Limits: risk.Limits{MaxOrderNotional: 5000000, MaxPosition: 10}}
}
func csv(prices ...string) string {
	var b strings.Builder
	b.WriteString("timestamp,symbol,bid,ask\n")
	for n, p := range prices {
		fmt.Fprintf(&b, "2026-09-15T13:30:%02dZ,AAPL,%s\n", n, p)
	}
	return b.String()
}

func TestEndToEnd(t *testing.T) {
	// First SELL is rejected. Then buy at 103, buy at 105, sell at 102;
	// final unchanged midpoint has a different bid, so the final mark must be 101.
	input := csv("100,102", "99,101", "101,103", "103,105", "102,104", "101,105")
	want := engine.Summary{QuotesProcessed: 6, IntentsGenerated: 4, RiskApprovals: 3, RiskRejections: 1, OrdersCreated: 3, FillsApplied: 3, FinalCash: 8940000, FinalPosition: 1, FinalEquity: 9950000, FinalPnL: -50000}
	var firstLog string
	for run := 0; run < 2; run++ {
		var logs bytes.Buffer
		got, err := engine.Run(strings.NewReader(input), config(), log.New(&logs, "", 0))
		if err != nil || got != want {
			t.Fatalf("summary=%+v, err=%v; want %+v", got, err, want)
		}
		if run == 0 {
			firstLog = logs.String()
		} else if logs.String() != firstLog {
			t.Fatal("run logs are not deterministic")
		}
		for _, event := range []string{"event=risk_rejection", `intent_id="price-movement-1"`, `order_id="order-1"`, `fill_id="fill-3"`, "event=simulation_complete"} {
			if !strings.Contains(logs.String(), event) {
				t.Fatalf("missing log %s", event)
			}
		}
		t.Logf("quotes=%d intents=%d approvals=%d rejections=%d orders=%d fills=%d cash=%s position=%d equity=%s pnl=%s", got.QuotesProcessed, got.IntentsGenerated, got.RiskApprovals, got.RiskRejections, got.OrdersCreated, got.FillsApplied, got.FinalCash, got.FinalPosition, got.FinalEquity, got.FinalPnL)
	}
}

func TestTradingRejectionsContinue(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*engine.Config)
	}{
		{"cash", func(c *engine.Config) { c.InitialCash = 0 }},
		{"notional", func(c *engine.Config) { c.Limits.MaxOrderNotional = 1 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config()
			tt.change(&cfg)
			got, err := engine.Run(strings.NewReader(csv("100,102", "101,103", "102,104")), cfg, nil)
			want := engine.Summary{QuotesProcessed: 3, IntentsGenerated: 2, RiskRejections: 2, FinalCash: cfg.InitialCash, FinalEquity: cfg.InitialCash}
			if err != nil || got != want {
				t.Fatalf("summary=%+v, %v", got, err)
			}
		})
	}
	cfg := config()
	cfg.Limits.MaxPosition = 1
	got, err := engine.Run(strings.NewReader(csv("100,102", "101,103", "102,104")), cfg, nil)
	want := engine.Summary{QuotesProcessed: 3, IntentsGenerated: 2, RiskApprovals: 1, RiskRejections: 1, OrdersCreated: 1, FillsApplied: 1, FinalCash: 8970000, FinalPosition: 1, FinalEquity: 9990000, FinalPnL: -10000}
	if err != nil || got != want {
		t.Fatalf("position rejection summary=%+v, %v", got, err)
	}
}

func TestFatalErrors(t *testing.T) {
	for _, tt := range []struct{ name, input, reason string }{
		{"empty", "", "header"},
		{"header only", csv(), "no valid quotes"},
		{"bad row", csv("100,102", "bad,103", "102,104"), "row 3"},
		{"first wrong symbol", strings.ReplaceAll(csv("100,102"), "AAPL", "MSFT"), "expected symbol"},
		{"backwards", "timestamp,symbol,bid,ask\n2026-09-15T13:30:01Z,AAPL,100,101\n2026-09-15T13:30:00Z,AAPL,101,102\n", "earlier"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			got, err := engine.Run(strings.NewReader(tt.input), config(), log.New(&logs, "", 0))
			if err == nil || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(logs.String(), "simulation_complete") || got.FinalEquity != 0 {
				t.Fatal("fatal run reported completion")
			}
		})
	}
}

func TestPortfolioFailureIsFatal(t *testing.T) {
	// Start at MaxInt64 cash, buy one unit, then sell for two units after
	// a higher midpoint. The sell passes risk but would overflow cash.
	cfg := config()
	cfg.InitialCash = math.MaxInt64
	input := csv("0.0001,0.0001", "0.0001,0.0003", "0.0004,0.0010", "0.0005,0.0005", "0.0006,0.0006")
	// Two buys cost 3+10 units. Sell 5 does not overflow; use one position cap
	// so the second buy is rejected, then sell 5 exceeds the initial cash.
	cfg.Limits.MaxPosition = 1
	got, err := engine.Run(strings.NewReader(input), cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "engine apply fill") || !strings.Contains(err.Error(), "cash overflows") {
		t.Fatalf("error=%v", err)
	}
	if got.QuotesProcessed != 4 || got.FillsApplied != 1 || got.RiskApprovals != 2 || got.RiskRejections != 1 {
		t.Fatalf("partial counters=%+v", got)
	}
}

func TestConfigurationAndSingleQuote(t *testing.T) {
	for _, change := range []func(*engine.Config){func(c *engine.Config) { c.Symbol = "" }, func(c *engine.Config) { c.InitialCash = -1 }, func(c *engine.Config) { c.Limits.MaxPosition = 0 }} {
		cfg := config()
		change(&cfg)
		if _, err := engine.Run(strings.NewReader(csv("1,2")), cfg, nil); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	got, err := engine.Run(strings.NewReader(csv("1,2")), config(), nil)
	if err != nil || got != (engine.Summary{QuotesProcessed: 1, FinalCash: domain.Money(10000000), FinalEquity: 10000000}) {
		t.Fatalf("single quote=%+v, %v", got, err)
	}
}
