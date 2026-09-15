// Package marketdata reads historical quotes without wall-clock timing.
package marketdata

import (
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

// Replay reads quotes in CSV record order. Use NewReplay to construct it.
// Row numbers count CSV records, including the header as row 1; blank lines
// are ignored by encoding/csv. The caller owns and closes the underlying reader.
type Replay struct {
	reader   *csv.Reader
	row      int
	previous time.Time
	terminal error
}

// NewReplay reads and validates the exact timestamp,symbol,bid,ask header.
func NewReplay(r io.Reader) (*Replay, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = 4
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("CSV row 1: read header: %w", err)
	}
	if !slices.Equal(header, []string{"timestamp", "symbol", "bid", "ask"}) {
		return nil, fmt.Errorf("CSV row 1: expected header timestamp,symbol,bid,ask; got %q", header)
	}
	return &Replay{reader: reader, row: 1}, nil
}

// Next immediately returns the next validated quote. Timestamps must not move
// backwards; equal timestamps are allowed and retain file order. On EOF or an
// invalid row, replay stops and subsequent calls return the same error.
func (r *Replay) Next() (domain.Quote, error) {
	if r.terminal != nil {
		return domain.Quote{}, r.terminal
	}
	r.row++
	quote, err := r.readQuote()
	if err != nil {
		if err == io.EOF {
			r.terminal = io.EOF
		} else {
			r.terminal = fmt.Errorf("CSV row %d: %w", r.row, err)
		}
		return domain.Quote{}, r.terminal
	}
	r.previous = quote.Timestamp
	return quote, nil
}

func (r *Replay) readQuote() (domain.Quote, error) {
	record, err := r.reader.Read()
	if err != nil {
		return domain.Quote{}, err
	}
	stamp, err := time.Parse(time.RFC3339Nano, record[0])
	if err != nil {
		return domain.Quote{}, fmt.Errorf("timestamp: %w", err)
	}
	bid, err := domain.ParsePrice(record[2])
	if err != nil {
		return domain.Quote{}, fmt.Errorf("bid: %w", err)
	}
	ask, err := domain.ParsePrice(record[3])
	if err != nil {
		return domain.Quote{}, fmt.Errorf("ask: %w", err)
	}
	quote := domain.Quote{Symbol: domain.Symbol(record[1]), Timestamp: stamp, Bid: bid, Ask: ask}
	if err := quote.Validate(); err != nil {
		return domain.Quote{}, err
	}
	if stamp.Before(r.previous) {
		return domain.Quote{}, fmt.Errorf("timestamp %s is earlier than previous timestamp %s", stamp.Format(time.RFC3339Nano), r.previous.Format(time.RFC3339Nano))
	}
	return quote, nil
}
