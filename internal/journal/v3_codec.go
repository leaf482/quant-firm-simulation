package journal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"reflect"
	"strings"
	"unicode/utf8"
)

type v3Envelope struct {
	Record V3Record `json:"record"`
	CRC32C uint32   `json:"crc32c"`
}

// EncodeV3Record emits one newline-terminated canonical JSONL record. Validation
// is record-local; this does not authorize an append or validate trading history.
func EncodeV3Record(record V3Record) ([]byte, error) {
	canonical, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	line, err := json.Marshal(v3Envelope{record, crc32.Checksum(canonical, castagnoli)})
	if err != nil {
		return nil, err
	}
	// Use the same strict path for caller-built and wire records, including nil
	// payloads, mismatched event types and side-inapplicable union fields.
	if _, err := DecodeV3Record(line); err != nil {
		return nil, err
	}
	return append(line, '\n'), nil
}

// DecodeV3Record validates a single JSON object and its canonical CRC32C. JSONL
// framing, consecutive sequences and immutable run metadata are ReadVersioned's
// responsibility. This function never applies or deduplicates trading state.
func DecodeV3Record(line []byte) (V3Record, error) {
	var result V3Record
	if !utf8.Valid(line) {
		return result, fmt.Errorf("v3: invalid UTF-8")
	}
	e, err := object(line, []string{"record", "crc32c"}, nil)
	if err != nil {
		return result, fmt.Errorf("v3 structure: %w", err)
	}
	r, err := object(field(e, "record"), []string{"version", "sequence", "run", "event"}, nil)
	if err != nil {
		return result, fmt.Errorf("v3 structure: %w", err)
	}
	for _, item := range []struct {
		raw    []byte
		target any
	}{
		{field(r, "version"), &result.Version}, {field(r, "sequence"), &result.Sequence}, {field(r, "run"), &result.Run},
	} {
		if err := decodeV3Value(item.raw, item.target); err != nil {
			return V3Record{}, err
		}
	}
	if result.Version != Version3 {
		return V3Record{}, fmt.Errorf("unsupported journal version %d", result.Version)
	}
	ev, err := object(field(r, "event"), []string{"type", "payload"}, nil)
	if err != nil {
		return V3Record{}, fmt.Errorf("v3 structure: %w", err)
	}
	if err := decodeV3Value(field(ev, "type"), &result.Event.Type); err != nil {
		return V3Record{}, err
	}
	var payload V3Payload
	switch result.Event.Type {
	case RunStarted:
		payload = &V3Start{}
	case OrderAdmitted:
		payload = &V3Admission{}
	case OrderSubmitted:
		payload = &V3Submission{}
	case FillApplied:
		payload = &V3Settlement{}
	case OrderFilled:
		payload = &V3Finalization{}
	case OrderTerminal:
		payload = &V3Terminal{}
	case IntentDenied:
		payload = &V3Denial{}
	default:
		return V3Record{}, fmt.Errorf("v3: unknown event type %q", result.Event.Type)
	}
	if err := decodeV3Value(field(ev, "payload"), payload); err != nil {
		return V3Record{}, err
	}
	result.Event.Payload = payload
	var checksum uint32
	if err := decodeV3Value(field(e, "crc32c"), &checksum); err != nil {
		return V3Record{}, err
	}
	canonical, err := json.Marshal(result)
	if err != nil {
		return V3Record{}, err
	}
	if crc32.Checksum(canonical, castagnoli) != checksum {
		return V3Record{}, fmt.Errorf("v3: CRC32C checksum mismatch")
	}
	if err := result.validate(); err != nil {
		return V3Record{}, fmt.Errorf("v3: %w", err)
	}
	return result, nil
}

// The concrete codec structs are also the structural schema. This walk checks
// every nested object before typed decoding can erase missing/null values or
// accept duplicate keys and case aliases. Only pointer fields tagged omitempty
// are optional; tagged-union validation subsequently checks their exact variant.
// Payload dispatch supplies a concrete struct before this walk; canonical
// serialization uses those declared struct fields, never unordered maps.
func decodeV3Value(raw []byte, target any) error {
	if err := v3Shape(raw, reflect.TypeOf(target).Elem()); err != nil {
		return fmt.Errorf("v3 structure: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("v3 value: %w", err)
	}
	return nil
}

func v3Shape(raw []byte, typ reflect.Type) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("null value")
	}
	if typ.Kind() == reflect.Pointer {
		return v3Shape(raw, typ.Elem())
	}
	if typ.Kind() != reflect.Struct {
		// Typed decoding checks primitive kinds and ranges.
		return nil
	}
	var required, optional []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name, options, _ := strings.Cut(f.Tag.Get("json"), ",")
		if options == "omitempty" {
			optional = append(optional, name)
		} else {
			required = append(required, name)
		}
	}
	fields, err := object(raw, required, optional)
	if err != nil {
		return err
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if value := field(fields, name); value != nil {
			if err := v3Shape(value, f.Type); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return nil
}
