// Package journal stores three trading event types as synced JSON Lines.
package journal

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

const (
	OrderCreated = "order_created"
	OrderChanged = "order_state_changed"
	FillApplied  = "fill_applied"
)

type Change struct {
	OrderID domain.OrderID     `json:"order_id"`
	Status  domain.OrderStatus `json:"status"`
}

type Event struct {
	Type   string        `json:"type"`
	Order  *domain.Order `json:"order,omitempty"`
	Change *Change       `json:"change,omitempty"`
	Fill   *domain.Fill  `json:"fill,omitempty"`
}

// Account configuration is repeated so recovery needs no external configuration.
// No quotes or strategy state are recorded. Monetary JSON values are integer units.
type Record struct {
	Version     int           `json:"version"`
	Sequence    uint64        `json:"sequence"`
	Symbol      domain.Symbol `json:"symbol"`
	InitialCash domain.Money  `json:"initial_cash"`
	Event       Event         `json:"event"`
}

func (e Event) validate() error {
	switch e.Type {
	case OrderCreated:
		if e.Order == nil || e.Change != nil || e.Fill != nil {
			return fmt.Errorf("invalid order_created payload")
		}
		if err := e.Order.Validate(); err != nil {
			return err
		}
		if e.Order.Status != domain.OrderNew {
			return fmt.Errorf("created order must be NEW")
		}
	case OrderChanged:
		if e.Change == nil || e.Order != nil || e.Fill != nil {
			return fmt.Errorf("invalid order_state_changed payload")
		}
		if strings.TrimSpace(string(e.Change.OrderID)) == "" {
			return fmt.Errorf("empty change order ID")
		}
		return e.Change.Status.Validate()
	case FillApplied:
		if e.Fill == nil || e.Order != nil || e.Change != nil {
			return fmt.Errorf("invalid fill_applied payload")
		}
		return e.Fill.Validate()
	default:
		return fmt.Errorf("unknown event type %q", e.Type)
	}
	return nil
}

// Private durability boundary permits deterministic write/sync failure tests.
type durableFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type Writer struct {
	file        durableFile
	symbol      domain.Symbol
	initialCash domain.Money
	sequence    uint64
	failed      error
	closed      bool
}

// Create exclusively creates a new file. Existing journals are never overwritten
// or reopened for appending. The caller owns Close. Parent directories must exist.
func Create(path string, symbol domain.Symbol, initialCash domain.Money) (*Writer, error) {
	if strings.TrimSpace(string(symbol)) == "" || initialCash < 0 {
		return nil, fmt.Errorf("invalid journal account configuration")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	return &Writer{file: f, symbol: symbol, initialCash: initialCash}, nil
}

// CheckConfig prevents writing events under different account metadata.
func (w *Writer) CheckConfig(symbol domain.Symbol, cash domain.Money) error {
	if w.closed || w.failed != nil || w.file == nil {
		return fmt.Errorf("journal is unavailable")
	}
	if w.sequence != 0 {
		return fmt.Errorf("new run requires an unused journal writer")
	}
	if symbol != w.symbol || cash != w.initialCash {
		return fmt.Errorf("journal account configuration mismatch")
	}
	return nil
}

// Append writes one complete line and Syncs it before reporting success. Any
// write/sync failure poisons this writer; a caller must halt, never retry it.
// A failed Sync has an uncertain disk outcome: recovery validates whatever full
// records survived. No API can promise failed writes are absent from the file.
func (w *Writer) Append(event Event) error {
	if w.closed || w.file == nil {
		return fmt.Errorf("journal is closed")
	}
	if w.failed != nil {
		return fmt.Errorf("journal failed: %w", w.failed)
	}
	if w.sequence == math.MaxUint64 {
		return fmt.Errorf("journal sequence exhausted")
	}
	if err := event.validate(); err != nil {
		return err
	}
	record := Record{Version: schemaVersion, Sequence: w.sequence + 1, Symbol: w.symbol, InitialCash: w.initialCash, Event: event}
	line, err := encodeRecord(record)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	n, err := w.file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = w.file.Sync()
	}
	if err != nil {
		w.failed = err
		return fmt.Errorf("journal append: %w", err)
	}
	w.sequence++
	return nil
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

// Read validates framing, schema and consecutive sequences. Even a final valid
// JSON object without a newline is rejected as a potentially torn record.
func Read(input io.Reader) ([]Record, error) {
	r := bufio.NewReader(input)
	var records []Record
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF && len(line) == 0 {
			return records, nil
		}
		row := len(records) + 1
		if err != nil {
			return nil, fmt.Errorf("journal row %d: incomplete record: %w", row, err)
		}
		record, err := decodeRecord(line)
		if err != nil {
			return nil, fmt.Errorf("journal row %d: %w", row, err)
		}
		if record.Version != schemaVersion || record.Sequence != uint64(row) {
			return nil, fmt.Errorf("journal row %d: invalid version or sequence", row)
		}
		if strings.TrimSpace(string(record.Symbol)) == "" || record.InitialCash < 0 {
			return nil, fmt.Errorf("journal row %d: invalid account configuration", row)
		}
		if err := record.Event.validate(); err != nil {
			return nil, fmt.Errorf("journal row %d: %w", row, err)
		}
		records = append(records, record)
	}
}
