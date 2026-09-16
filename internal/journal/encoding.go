package journal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"slices"
)

const schemaVersion = 2

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// CRC32C covers json.Marshal(Record): all recovery fields, in struct field order.
// The envelope checksum and newline are excluded. This detects accidental
// corruption, not deliberate modification by someone who can recompute the CRC.
type envelope struct {
	Record Record `json:"record"`
	CRC32C uint32 `json:"crc32c"`
}

func encodeRecord(record Record) ([]byte, error) {
	payload, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{Record: record, CRC32C: crc32.Checksum(payload, castagnoli)})
}

func decodeRecord(line []byte) (Record, error) {
	if err := validateStructure(line); err != nil {
		return Record{}, fmt.Errorf("structure: %w", err)
	}
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return Record{}, err
	}
	if e.Record.Version != schemaVersion {
		return Record{}, fmt.Errorf("unsupported journal version %d; expected %d", e.Record.Version, schemaVersion)
	}
	payload, err := json.Marshal(e.Record)
	if err != nil {
		return Record{}, err
	}
	if crc32.Checksum(payload, castagnoli) != e.CRC32C {
		return Record{}, fmt.Errorf("CRC32C checksum mismatch")
	}
	return e.Record, nil
}

// Validate wire fields before typed decoding can erase missing/null values or
// accept duplicate keys and case-insensitive aliases. No maps enter encoding.
type member struct {
	name  string
	value json.RawMessage
}

func object(raw []byte, required, optional []string) ([]member, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, fmt.Errorf("expected object")
	}
	var fields []member
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("expected field name")
		}
		for _, f := range fields {
			if f.name == name {
				return nil, fmt.Errorf("duplicate field %q", name)
			}
		}
		if !slices.Contains(required, name) && !slices.Contains(optional, name) {
			return nil, fmt.Errorf("unknown field %q", name)
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("null field %q", name)
		}
		fields = append(fields, member{name: name, value: value})
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	for _, name := range required {
		if field(fields, name) == nil {
			return nil, fmt.Errorf("missing field %q", name)
		}
	}
	return fields, nil
}

func field(fields []member, name string) json.RawMessage {
	for _, f := range fields {
		if f.name == name {
			return f.value
		}
	}
	return nil
}

func validateStructure(line []byte) error {
	e, err := object(line, []string{"record", "crc32c"}, nil)
	if err != nil {
		return err
	}
	r, err := object(field(e, "record"), []string{"version", "sequence", "symbol", "initial_cash", "event"}, nil)
	if err != nil {
		return err
	}
	event, err := object(field(r, "event"), []string{"type"}, []string{"order", "change", "fill"})
	if err != nil {
		return err
	}
	var kind string
	if err := json.Unmarshal(field(event, "type"), &kind); err != nil {
		return err
	}
	if len(event) != 2 {
		return fmt.Errorf("event requires exactly one payload")
	}
	var payload string
	var required []string
	switch kind {
	case OrderCreated:
		payload = "order"
		required = []string{"OrderID", "IntentID", "Symbol", "Side", "Quantity", "Status", "CreatedAt"}
	case OrderChanged:
		payload = "change"
		required = []string{"order_id", "status"}
	case FillApplied:
		payload = "fill"
		required = []string{"FillID", "OrderID", "Symbol", "Side", "Quantity", "Price", "Timestamp"}
	default:
		return fmt.Errorf("unknown event type %q", kind)
	}
	_, err = object(field(event, payload), required, nil)
	return err
}
