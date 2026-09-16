package portfolio

import (
	"maps"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

func newPortfolio(t *testing.T, cash domain.Money) *Portfolio {
	t.Helper()
	p, err := New("AAPL", cash)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func fill(id domain.FillID, side domain.Side, quantity domain.Quantity, price domain.Price) domain.Fill {
	return domain.Fill{FillID: id, OrderID: domain.OrderID("order-" + string(id)), Symbol: "AAPL", Side: side, Quantity: quantity, Price: price, Timestamp: time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)}
}

func quote(bid, ask domain.Price) domain.Quote {
	return domain.Quote{Symbol: "AAPL", Timestamp: time.Date(2026, 9, 15, 13, 30, 1, 0, time.UTC), Bid: bid, Ask: ask}
}

// Copy the deduplication map too, so atomicity checks cover existing records.
func copyState(p *Portfolio) Portfolio {
	before := *p
	before.applied = maps.Clone(p.applied)
	return before
}
func assertUnchanged(t *testing.T, p *Portfolio, before Portfolio) {
	t.Helper()
	if p.symbol != before.symbol || p.initialCash != before.initialCash || p.cash != before.cash || p.position != before.position || !maps.Equal(p.applied, before.applied) {
		t.Fatal("operation mutated portfolio state")
	}
}

func TestInitialization(t *testing.T) {
	for _, cash := range []domain.Money{0, 10000000, math.MaxInt64} {
		p := newPortfolio(t, cash)
		got, err := p.Snapshot(quote(1, 2))
		want := Snapshot{Cash: cash, Equity: cash}
		if err != nil || got != want || p.initialCash != cash || p.symbol != "AAPL" || len(p.applied) != 0 {
			t.Fatalf("initial snapshot=%+v, %v", got, err)
		}
	}
	for _, tt := range []struct {
		symbol domain.Symbol
		cash   domain.Money
	}{{"", 1}, {" \t", 1}, {"AAPL", -1}} {
		if p, err := New(tt.symbol, tt.cash); err == nil || p != nil {
			t.Fatal("invalid initialization accepted")
		}
	}
}

func TestBuySellAndSnapshots(t *testing.T) {
	p := newPortfolio(t, 10000000)             // $1,000
	buy := fill("buy", domain.Buy, 2, 1000000) // 2 shares at $100
	if err := p.Apply(buy); err != nil {
		t.Fatal(err)
	}
	if p.cash != 8000000 || p.position != 2 {
		t.Fatalf("buy cash=%d position=%d", p.cash, p.position)
	}
	for _, tt := range []struct {
		name string
		bid  domain.Price
		want Snapshot
	}{
		{"profit", 1100000, Snapshot{8000000, 2, 2200000, 10200000, 200000}},
		{"loss", 900000, Snapshot{8000000, 2, 1800000, 9800000, -200000}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := copyState(p)
			for repeat := 0; repeat < 2; repeat++ {
				got, err := p.Snapshot(quote(tt.bid, tt.bid+100000))
				if err != nil || got != tt.want {
					t.Fatalf("snapshot=%+v, %v; want %+v", got, err, tt.want)
				}
			}
			assertUnchanged(t, p, before)
		})
	}
	sell := fill("sell", domain.Sell, 1, 1200000)
	if err := p.Apply(sell); err != nil {
		t.Fatal(err)
	}
	got, err := p.Snapshot(quote(1100000, 1200000))
	want := Snapshot{9200000, 1, 1100000, 10300000, 300000}
	if err != nil || got != want {
		t.Fatalf("sell snapshot=%+v, %v", got, err)
	}
	if err := p.Apply(fill("close", domain.Sell, 1, 1100000)); err != nil {
		t.Fatal(err)
	}
	got, err = p.Snapshot(quote(1, math.MaxInt64))
	if err != nil || got != (Snapshot{Cash: 10300000, Equity: 10300000, PnL: 300000}) {
		t.Fatalf("closed snapshot=%+v, %v", got, err)
	}
}

func TestDuplicateFills(t *testing.T) {
	p := newPortfolio(t, 1000000)
	buy := fill("buy", domain.Buy, 1, 1000000)
	if err := p.Apply(buy); err != nil {
		t.Fatal(err)
	}
	before := copyState(p)
	if err := p.Apply(buy); err != nil {
		t.Fatal(err)
	} // Cash is now zero; retry must still succeed.
	assertUnchanged(t, p, before)
	sell := fill("sell", domain.Sell, 1, 1100000)
	if err := p.Apply(sell); err != nil {
		t.Fatal(err)
	}
	before = copyState(p)
	for _, retry := range []domain.Fill{buy, sell} {
		retry.Timestamp = retry.Timestamp.In(time.FixedZone("offset", 3600))
		if err := p.Apply(retry); err != nil {
			t.Fatal(err)
		}
		assertUnchanged(t, p, before)
	}
}

func TestConflictingFillIDs(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*domain.Fill)
	}{
		{"order", func(f *domain.Fill) { f.OrderID = "different" }},
		{"symbol", func(f *domain.Fill) { f.Symbol = "MSFT" }},
		{"side", func(f *domain.Fill) { f.Side = domain.Sell }},
		{"quantity", func(f *domain.Fill) { f.Quantity++ }},
		{"price", func(f *domain.Fill) { f.Price++ }},
		{"timestamp", func(f *domain.Fill) { f.Timestamp = f.Timestamp.Add(time.Second) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newPortfolio(t, 10000000)
			f := fill("f", domain.Buy, 1, 1000000)
			if err := p.Apply(f); err != nil {
				t.Fatal(err)
			}
			before := copyState(p)
			conflict := f
			tt.edit(&conflict)
			if err := p.Apply(conflict); err == nil {
				t.Fatal("conflict accepted")
			}
			assertUnchanged(t, p, before)
			if err := p.Apply(f); err != nil {
				t.Fatal(err)
			}
			assertUnchanged(t, p, before)
		})
	}
}

func TestRejectedFillsAreAtomic(t *testing.T) {
	for _, tt := range []struct {
		name   string
		edit   func(*domain.Fill)
		reason string
	}{
		{"insufficient cash", func(f *domain.Fill) { f.Quantity = 11 }, "insufficient cash"},
		{"sell without holdings", func(f *domain.Fill) { f.Side = domain.Sell }, "insufficient holdings"},
		{"wrong symbol", func(f *domain.Fill) { f.Symbol = "MSFT" }, "symbol"},
		{"invalid ID", func(f *domain.Fill) { f.FillID = "" }, "ID"},
		{"invalid quantity", func(f *domain.Fill) { f.Quantity = 0 }, "quantity"},
		{"invalid price", func(f *domain.Fill) { f.Price = -1 }, "price"},
		{"invalid side", func(f *domain.Fill) { f.Side = "OTHER" }, "side"},
		{"invalid timestamp", func(f *domain.Fill) { f.Timestamp = time.Time{} }, "timestamp"},
		{"notional overflow", func(f *domain.Fill) { f.Price = math.MaxInt64; f.Quantity = 2 }, "overflows"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newPortfolio(t, 10000000)
			before := copyState(p)
			f := fill("retry", domain.Buy, 1, 1000000)
			tt.edit(&f)
			if err := p.Apply(f); err == nil || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("rejection=%v", err)
			}
			assertUnchanged(t, p, before)
			if err := p.Apply(fill("retry", domain.Buy, 1, 1000000)); err != nil {
				t.Fatal(err)
			}
			if p.cash != 9000000 || p.position != 1 || len(p.applied) != 1 {
				t.Fatal("failed fill was incorrectly marked applied")
			}
		})
	}
}

func TestAccountOverflowAndBoundaries(t *testing.T) {
	t.Run("cash overflow", func(t *testing.T) {
		p := newPortfolio(t, math.MaxInt64)
		if err := p.Apply(fill("buy", domain.Buy, 1, 1)); err != nil {
			t.Fatal(err)
		}
		before := copyState(p)
		if err := p.Apply(fill("sell", domain.Sell, 1, 2)); err == nil || !strings.Contains(err.Error(), "cash overflows") {
			t.Fatalf("error=%v", err)
		}
		assertUnchanged(t, p, before)
		if err := p.Apply(fill("sell", domain.Sell, 1, 1)); err != nil {
			t.Fatal(err)
		}
		if p.cash != math.MaxInt64 || p.position != 0 {
			t.Fatal("exact cash boundary failed")
		}
	})
	t.Run("position overflow", func(t *testing.T) {
		p := newPortfolio(t, math.MaxInt64)
		for _, f := range []domain.Fill{
			fill("buy-all", domain.Buy, math.MaxInt64, 1),
			fill("sell-one", domain.Sell, 1, 2),
			fill("buy-one", domain.Buy, 1, 1),
		} {
			if err := p.Apply(f); err != nil {
				t.Fatal(err)
			}
		}
		before := copyState(p)
		if err := p.Apply(fill("overflow", domain.Buy, 1, 1)); err == nil || !strings.Contains(err.Error(), "position overflows") {
			t.Fatalf("error=%v", err)
		}
		assertUnchanged(t, p, before)
	})
	t.Run("sell above nonzero holdings", func(t *testing.T) {
		p := newPortfolio(t, 100)
		if err := p.Apply(fill("buy", domain.Buy, 1, 100)); err != nil {
			t.Fatal(err)
		}
		before := copyState(p)
		if err := p.Apply(fill("sell", domain.Sell, 2, 1)); err == nil {
			t.Fatal("short position allowed")
		}
		assertUnchanged(t, p, before)
	})
}

func TestRejectedSnapshotsDoNotMutate(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cash     domain.Money
		quantity domain.Quantity
		q        domain.Quote
		reason   string
	}{
		{"wrong symbol", 100, 1, domain.Quote{Symbol: "MSFT", Timestamp: quote(1, 1).Timestamp, Bid: 1, Ask: 1}, "symbol"},
		{"invalid quote", 100, 1, quote(2, 1), "quote"},
		{"market value overflow", 2, 2, quote(math.MaxInt64, math.MaxInt64), "notional overflows"},
		{"equity overflow", math.MaxInt64, 1, quote(2, 2), "equity overflows"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newPortfolio(t, tt.cash)
			if err := p.Apply(fill("buy", domain.Buy, tt.quantity, 1)); err != nil {
				t.Fatal(err)
			}
			before := copyState(p)
			got, err := p.Snapshot(tt.q)
			if err == nil || !strings.Contains(err.Error(), tt.reason) || got != (Snapshot{}) {
				t.Fatalf("snapshot=%+v, %v", got, err)
			}
			assertUnchanged(t, p, before)
		})
	}
}

func TestZeroValuePortfolioRejects(t *testing.T) {
	var p Portfolio
	if err := p.Apply(fill("f", domain.Buy, 1, 1)); err == nil {
		t.Fatal("uninitialized portfolio accepted fill")
	}
	if _, err := p.Snapshot(quote(1, 1)); err == nil {
		t.Fatal("uninitialized portfolio accepted snapshot")
	}
}
