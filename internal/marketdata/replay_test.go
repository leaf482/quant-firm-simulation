package marketdata_test

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/marketdata"
)

const header = "timestamp,symbol,bid,ask\n"

func TestReplayFixture(t *testing.T) {
	file, err := os.Open("../../testdata/quotes.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r, err := marketdata.NewReplay(file)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)
	prices := [][2]domain.Price{{2291000, 2291200}, {2291100, 2291300}, {2290900, 2291100}, {2291200, 2291400}, {2291300, 2291500}, {2291000, 2291200}}
	for i, prices := range prices {
		q, err := r.Next()
		if err != nil {
			t.Fatalf("quote %d: %v", i, err)
		}
		if q.Symbol != "AAPL" || !q.Timestamp.Equal(base.Add(time.Duration(i)*100*time.Millisecond)) || q.Bid != prices[0] || q.Ask != prices[1] {
			t.Fatalf("quote %d: unexpected value %+v", i, q)
		}
		if err := q.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		q, err := r.Next()
		if err != io.EOF || q != (domain.Quote{}) {
			t.Fatalf("Next after completion = %+v, %v", q, err)
		}
	}
}

func TestHeader(t *testing.T) {
	for _, input := range []string{"", "time,symbol,bid,ask\n", "timestamp,symbol,ask,bid\n", "timestamp,symbol,bid\n", "timestamp,symbol,bid,ask,extra\n", "timestamp, symbol,bid,ask\n"} {
		t.Run(input, func(t *testing.T) {
			if _, err := marketdata.NewReplay(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "row 1") {
				t.Fatalf("NewReplay error = %v", err)
			}
		})
	}
	r, err := marketdata.NewReplay(strings.NewReader(header))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("header-only EOF = %v", err)
	}
}

func TestInvalidRowsStopReplay(t *testing.T) {
	for _, tt := range []struct{ name, row, reason string }{
		{"timestamp", "bad,AAPL,100,101", "timestamp"},
		{"zero timestamp", "0001-01-01T00:00:00Z,AAPL,100,101", "timestamp"},
		{"bid", "2026-09-15T13:30:00Z,AAPL,no,101", "bid"},
		{"ask precision", "2026-09-15T13:30:00Z,AAPL,100,101.00001", "ask"},
		{"negative bid", "2026-09-15T13:30:00Z,AAPL,-1,101", "bid"},
		{"zero bid", "2026-09-15T13:30:00Z,AAPL,0,101", "positive"},
		{"crossed", "2026-09-15T13:30:00Z,AAPL,102,101", "exceed"},
		{"symbol", "2026-09-15T13:30:00Z,,100,101", "symbol"},
		{"blank symbol", "2026-09-15T13:30:00Z, ,100,101", "symbol"},
		{"missing field", "2026-09-15T13:30:00Z,AAPL,100", "fields"},
		{"extra field", "2026-09-15T13:30:00Z,AAPL,100,101,extra", "fields"},
		{"broken CSV", "2026-09-15T13:30:00Z,AAPL,100,10\"1", "quote"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := marketdata.NewReplay(strings.NewReader(header + tt.row + "\n2026-09-15T13:30:01Z,AAPL,100,101\n"))
			if err != nil {
				t.Fatal(err)
			}
			q, err := r.Next()
			if err == nil || !strings.Contains(err.Error(), "row 2") || !strings.Contains(err.Error(), tt.reason) || q != (domain.Quote{}) {
				t.Fatalf("Next = %+v, %v", q, err)
			}
			if _, nextErr := r.Next(); nextErr != err {
				t.Fatalf("error must remain terminal: %v", nextErr)
			}
		})
	}
}

func TestTimestampOrdering(t *testing.T) {
	for _, tt := range []struct {
		name, second string
		wantErr      bool
	}{
		{"backwards", "2026-09-15T13:30:00.050Z", true},
		{"equal", "2026-09-15T13:30:00.100Z", false},
		{"nanoseconds", "2026-09-15T13:30:00.100000001Z", false},
		{"same instant offset", "2026-09-15T09:30:00.100-04:00", false},
		{"earlier instant offset", "2026-09-15T14:30:00.050+01:00", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := marketdata.NewReplay(strings.NewReader(header + "2026-09-15T13:30:00.100Z,AAPL,100,101\n" + tt.second + ",AAPL,102,103\n"))
			if err != nil {
				t.Fatal(err)
			}
			first, err := r.Next()
			if err != nil || first.Bid != 1000000 {
				t.Fatalf("first = %+v, %v", first, err)
			}
			second, err := r.Next()
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "row 3") || !strings.Contains(err.Error(), "earlier") {
					t.Fatalf("ordering error = %v", err)
				}
			} else if err != nil || second.Bid != 1020000 {
				t.Fatalf("second = %+v, %v", second, err)
			}
		})
	}
}
