package journal

import (
	"bytes"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFillPriceCorruption(t *testing.T) {
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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Recover(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(data, []byte(`"Price":1000000`), []byte(`"Price":9000000`), 1)
	if bytes.Equal(changed, data) {
		t.Fatal("price digit was not changed")
	}
	for _, line := range bytes.Split(bytes.TrimSpace(changed), []byte("\n")) {
		if !json.Valid(line) {
			t.Fatal("mutation must retain valid JSON")
		}
	}
	state, err := Recover(bytes.NewReader(changed))
	if state != nil || err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("corruption accepted: %v", err)
	}
}

func TestChecksumCoversRecord(t *testing.T) {
	original := records()[0]
	encoded, err := encodeRecord(original)
	if err != nil {
		t.Fatal(err)
	}
	var e envelope
	if err := json.Unmarshal(encoded, &e); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(original)
	if e.CRC32C != crc32.Checksum(canonical, crc32.MakeTable(crc32.Castagnoli)) {
		t.Fatal("wrong checksum algorithm or scope")
	}
	for _, tt := range []struct{ name, from, to string }{
		{"version", `"version":2`, `"version":3`},
		{"sequence", `"sequence":1`, `"sequence":2`},
		{"symbol", `"symbol":"AAPL"`, `"symbol":"MSFT"`},
		{"initial cash", `"initial_cash":10000000`, `"initial_cash":20000000`},
		{"event type", `"type":"order_created"`, `"type":"fill_applied"`},
		{"payload", `"Quantity":1`, `"Quantity":2`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := strings.Replace(string(encoded), tt.from, tt.to, 1) + "\n"
			if state, err := Recover(strings.NewReader(input)); err == nil || state != nil {
				t.Fatal("modified record accepted")
			}
		})
	}
	// Whitespace/member ordering does not change canonical struct serialization.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, encoded, "", "  "); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRecord(pretty.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestStrictStructureBeforeChecksum(t *testing.T) {
	r := records()[0]
	r.InitialCash = 0
	valid, err := encodeRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	state, err := Recover(strings.NewReader(string(valid) + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	cash, position := state.Portfolio.State()
	if cash != 0 || position != 0 {
		t.Fatal("explicit zero lost")
	}
	// Missing/null cash decode to the same zero, and equal duplicate keys decode
	// to the same value: all retain a valid CRC under the old permissive decoder.
	for _, tt := range []struct{ name, from, to string }{
		{"missing cash", `"initial_cash":0,`, ``},
		{"null cash", `"initial_cash":0`, `"initial_cash":null`},
		{"duplicate cash", `"initial_cash":0`, `"initial_cash":0,"initial_cash":0`},
		{"duplicate sequence", `"sequence":1`, `"sequence":1,"sequence":1`},
		{"escaped duplicate", `"sequence":1`, `"sequence":1,"seque\u006ece":1`},
		{"duplicate version", `"version":2`, `"version":2,"version":2`},
		{"duplicate payload field", `"Quantity":1`, `"Quantity":1,"Quantity":1`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := strings.Replace(string(valid), tt.from, tt.to, 1)
			var permissive envelope
			if err := json.Unmarshal([]byte(input), &permissive); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(permissive.Record)
			if crc32.Checksum(payload, castagnoli) != permissive.CRC32C {
				t.Fatal("fixture must have an otherwise valid checksum")
			}
			s, err := Recover(strings.NewReader(input + "\n"))
			if s != nil || err == nil || !strings.Contains(err.Error(), "structure:") {
				t.Fatalf("structure accepted: %v", err)
			}
		})
	}
	for _, record := range records() {
		line, _ := encodeRecord(record)
		// Assert presence/null rejection for every required field at every level.
		var paths [][]string
		paths = append(paths, []string{}, []string{"record"}, []string{"record", "event"})
		payload := "order"
		if record.Event.Type == OrderChanged {
			payload = "change"
		}
		if record.Event.Type == FillApplied {
			payload = "fill"
		}
		paths = append(paths, []string{"record", "event", payload})
		for _, path := range paths {
			var root map[string]json.RawMessage
			json.Unmarshal(line, &root)
			obj := root
			for _, key := range path {
				var next map[string]json.RawMessage
				json.Unmarshal(obj[key], &next)
				obj = next
			}
			for key := range obj {
				for _, null := range []bool{false, true} {
					// Maps are used only to construct malformed test inputs, never CRC input.
					var mutate func([]byte, int) []byte
					mutate = func(raw []byte, depth int) []byte {
						var fields map[string]json.RawMessage
						json.Unmarshal(raw, &fields)
						if depth == len(path) {
							if null {
								fields[key] = json.RawMessage("null")
							} else {
								delete(fields, key)
							}
						} else {
							fields[path[depth]] = mutate(fields[path[depth]], depth+1)
						}
						out, _ := json.Marshal(fields)
						return out
					}
					input := append(mutate(line, 0), '\n')
					if s, err := Recover(bytes.NewReader(input)); err == nil || s != nil {
						t.Fatalf("accepted missing/null %v.%s", path, key)
					}
				}
			}
		}
	}
}

func TestDuplicateFillPriceAndEnvelope(t *testing.T) {
	input := encode(t, records())
	for _, tt := range []struct {
		name string
		data string
	}{
		{"fill price", strings.Replace(input, `"Price":1000000`, `"Price":1000000,"Price":1000000`, 1)},
		{"envelope record", strings.Replace(input, `{"record":`, `{"record":{},"record":`, 1)},
		{"checksum", strings.Replace(input, `"crc32c":`, `"crc32c":0,"crc32c":`, 1)},
		{"malformed", `{"record":` + "\n"},
		{"legacy v1", `{"version":1,"sequence":1}` + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if state, err := Recover(strings.NewReader(tt.data)); err == nil || state != nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}
