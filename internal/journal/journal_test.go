package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

func events() []Event {
	stamp := time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)
	o := domain.Order{OrderID: "order-1", IntentID: "intent-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Status: domain.OrderNew, CreatedAt: stamp}
	f := domain.Fill{FillID: "fill-1", OrderID: o.OrderID, Symbol: o.Symbol, Side: o.Side, Quantity: o.Quantity, Price: 1000000, Timestamp: stamp}
	return []Event{{Type: OrderCreated, Order: &o}, {Type: OrderChanged, Change: &Change{OrderID: o.OrderID, Status: domain.OrderSubmitted}}, {Type: FillApplied, Fill: &f}, {Type: OrderChanged, Change: &Change{OrderID: o.OrderID, Status: domain.OrderFilled}}}
}
func records() []Record {
	var out []Record
	for n, e := range events() {
		out = append(out, Record{Version: 1, Sequence: uint64(n + 1), Symbol: "AAPL", InitialCash: 10000000, Event: e})
	}
	return out
}
func encode(t *testing.T, r []Record) string {
	t.Helper()
	var b bytes.Buffer
	for _, record := range r {
		if err := json.NewEncoder(&b).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

func TestAppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.jsonl")
	w, err := Create(path, "AAPL", 10000000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events() {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(path, "AAPL", 0); err == nil {
		t.Fatal("overwrote existing journal")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(bytes.NewReader(data))
	if err != nil || len(got) != 4 {
		t.Fatalf("read=%v %v", got, err)
	}
	for n, r := range got {
		if r.Sequence != uint64(n+1) {
			t.Fatal("bad sequence")
		}
	}
	if err := w.Append(events()[0]); err == nil {
		t.Fatal("closed writer accepted append")
	}
}

func TestRecoveryAndPrefixes(t *testing.T) {
	for count := 1; count <= 4; count++ {
		state, err := Recover(strings.NewReader(encode(t, records()[:count])))
		if err != nil {
			t.Fatal(err)
		}
		o, err := state.Orders.Get("order-1")
		if err != nil {
			t.Fatal(err)
		}
		want := domain.OrderNew
		if count >= 2 {
			want = domain.OrderSubmitted
		}
		if count == 4 {
			want = domain.OrderFilled
		}
		if o.Status != want {
			t.Fatalf("prefix %d status=%s", count, o.Status)
		}
		cash, position := state.Portfolio.State()
		if count < 3 {
			if cash != 10000000 || position != 0 {
				t.Fatal("unrecorded fill restored")
			}
		} else if cash != 9000000 || position != 1 {
			t.Fatal("recorded fill missing")
		}
	}
	// A matching duplicate fill, even after FILLED, does not debit twice.
	r := records()
	duplicate := r[2]
	duplicate.Sequence = 5
	r = append(r, duplicate)
	state, err := Recover(strings.NewReader(encode(t, r)))
	if err != nil {
		t.Fatal(err)
	}
	cash, position := state.Portfolio.State()
	if cash != 9000000 || position != 1 {
		t.Fatal("duplicate applied twice")
	}
}

func TestCorruptHistories(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func([]Record) []Record
	}{
		{"gap", func(r []Record) []Record { r[1].Sequence = 3; return r }},
		{"out of order", func(r []Record) []Record { r[2].Sequence = 2; return r }},
		{"duplicate sequence", func(r []Record) []Record { r[1].Sequence = 1; return r }},
		{"version", func(r []Record) []Record { r[0].Version = 2; return r }},
		{"config mismatch", func(r []Record) []Record { r[1].InitialCash++; return r }},
		{"filled before submitted", func(r []Record) []Record { r[1].Event.Change.Status = domain.OrderFilled; return r }},
		{"unknown fill order", func(r []Record) []Record { r[2].Event.Fill.OrderID = "order-9"; return r }},
		{"invalid domain", func(r []Record) []Record { r[2].Event.Fill.Quantity = 0; return r }},
		{"wrong fill quantity", func(r []Record) []Record { r[2].Event.Fill.Quantity = 2; return r }},
		{"unexpected fill ID", func(r []Record) []Record { r[2].Event.Fill.FillID = "fill-2"; return r }},
		{"unexpected order ID", func(r []Record) []Record { r[0].Event.Order.OrderID = "order-2"; return r }},
		{"fill without submission", func(r []Record) []Record { r[1].Event.Change.Status = domain.OrderNew; return r }},
		{"cancel after applied fill", func(r []Record) []Record { r[3].Event.Change.Status = domain.OrderCancelled; return r }},
		{"conflicting duplicate fill", func(r []Record) []Record {
			f := *r[2].Event.Fill
			f.Price++
			r = append(r, Record{Version: 1, Sequence: 5, Symbol: "AAPL", InitialCash: 10000000, Event: Event{Type: FillApplied, Fill: &f}})
			return r
		}},
		{"insufficient cash", func(r []Record) []Record {
			for n := range r {
				r[n].InitialCash = 1
			}
			return r
		}},
		{"duplicate creation", func(r []Record) []Record { r[1].Event = r[0].Event; return r }},
		{"unknown status", func(r []Record) []Record { r[1].Event.Change.Status = "OTHER"; return r }},
		{"wrong symbol", func(r []Record) []Record { r[0].Event.Order.Symbol = "MSFT"; return r }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state, err := Recover(strings.NewReader(encode(t, tt.edit(records()))))
			if err == nil || state != nil {
				t.Fatal("corrupt history returned recovered state")
			}
		})
	}
	for _, input := range []string{"", "{broken}\n", strings.TrimSuffix(encode(t, records()), "\n"), "null\n", encode(t, records()) + "\n"} {
		if state, err := Recover(strings.NewReader(input)); err == nil || state != nil {
			t.Fatalf("accepted malformed input %q", input)
		}
	}
}

type failingFile struct {
	writes, syncs     int
	short             bool
	writeErr, syncErr error
	data              bytes.Buffer
}

func (f *failingFile) Write(p []byte) (int, error) {
	f.writes++
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.short {
		return len(p) - 1, nil
	}
	return f.data.Write(p)
}
func (f *failingFile) Sync() error  { f.syncs++; return f.syncErr }
func (f *failingFile) Close() error { return nil }

func TestDurabilityFailures(t *testing.T) {
	for _, tt := range []struct {
		name     string
		file     failingFile
		wantSync int
	}{
		{"write", failingFile{writeErr: errors.New("disk failure")}, 0},
		{"short write", failingFile{short: true}, 0},
		{"sync", failingFile{syncErr: errors.New("sync failure")}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := &Writer{file: &tt.file, symbol: "AAPL", initialCash: 10000000}
			if err := w.Append(events()[0]); err == nil {
				t.Fatal("failed append reported success")
			}
			if w.sequence != 0 || tt.file.syncs != tt.wantSync {
				t.Fatal("failed append committed sequence")
			}
			if err := w.Append(events()[0]); err == nil || tt.file.writes != 1 {
				t.Fatal("poisoned writer retried")
			}
		})
	}
	f := &failingFile{}
	w := &Writer{file: f, symbol: "AAPL", initialCash: 10000000}
	if err := w.Append(events()[0]); err != nil || f.syncs != 1 || w.sequence != 1 {
		t.Fatalf("successful append not synced: %v", err)
	}
	if _, err := Read(io.LimitReader(&f.data, 1)); err == nil {
		t.Fatal("torn record accepted")
	}
}
