package journal

import (
	"bytes"
	"encoding/json"
	"hash/crc32"
	"io"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

// Golden records copied from the contract, including its independent CRC values.
// The fixture contains alternatives, not one consecutive journal history.
func v3Examples(t *testing.T) [][]byte {
	t.Helper()
	data, err := os.ReadFile("testdata/v3-records.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 8 {
		t.Fatalf("got %d examples", len(lines))
	}
	for i := range lines {
		lines[i] = bytes.TrimSpace(lines[i])
	}
	return lines
}

func decodeExample(t *testing.T, index int) V3Record {
	t.Helper()
	r, err := DecodeV3Record(v3Examples(t)[index])
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func uncheckedV3(t *testing.T, r V3Record) []byte {
	t.Helper()
	canonical, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(v3Envelope{r, crc32.Checksum(canonical, castagnoli)})
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func streamV3(lines ...[]byte) []byte { return append(bytes.Join(lines, []byte("\n")), '\n') }

func TestV3ContractExamples(t *testing.T) {
	lines := v3Examples(t)
	wantTypes := []string{RunStarted, OrderAdmitted, OrderSubmitted, FillApplied, OrderFilled, OrderTerminal, OrderTerminal, IntentDenied}
	for i, line := range lines {
		r, err := DecodeV3Record(line)
		if err != nil {
			t.Fatalf("example %d: %v", i, err)
		}
		if r.Version != 3 || r.Event.Type != wantTypes[i] {
			t.Fatalf("unexpected record %+v", r)
		}
		encoded, err := EncodeV3Record(r)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded, append(append([]byte{}, line...), '\n')) {
			t.Fatalf("canonical encoding differs for example %d", i)
		}
	}
	for _, history := range [][][]byte{lines[:5], {lines[0], lines[1], lines[2], lines[5]}, {lines[0], lines[1], lines[2], lines[6]}, {lines[0], lines[7]}, {lines[0]}} {
		got, err := ReadVersioned(bytes.NewReader(streamV3(history...)))
		if err != nil || got.Version != 3 || len(got.V3) != len(history) || got.V2 != nil {
			t.Fatalf("dispatch=%+v err=%v", got, err)
		}
	}
}

func TestV3OtherPayloadVariants(t *testing.T) {
	admission := decodeExample(t, 1)
	p := admission.Event.Payload.(*V3Admission)
	p.Intent.Side = domain.Sell
	q := p.Intent.Quantity
	p.Reservation = V3Reservation{Type: "SELL_QUANTITY", ReservedQuantity: &q}
	settlement := decodeExample(t, 3)
	s := settlement.Event.Payload.(*V3Settlement)
	s.Side = domain.Sell
	s.Price = s.ExecutionQuote.Bid
	newRejected := decodeExample(t, 5)
	n := newRejected.Event.Payload.(*V3Terminal)
	n.Reason.Code = "SUBMISSION_DECLINED"
	n.Context.Quote = &p.OriginQuote
	controlRejected := decodeExample(t, 6)
	c := controlRejected.Event.Payload.(*V3Terminal)
	c.Status = domain.OrderRejected
	c.Reason.Code = "REJECT_REQUESTED"
	denial := decodeExample(t, 7)
	d := denial.Event.Payload.(*V3Denial)
	d.Intent.Side = domain.Sell
	d.Reason.Code = "INSUFFICIENT_HOLDINGS"
	for _, r := range []V3Record{admission, settlement, newRejected, controlRejected, denial} {
		line, err := EncodeV3Record(r)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeV3Record(line)
		if err != nil || !reflect.DeepEqual(got, r) {
			t.Fatalf("roundtrip=%+v err=%v", got, err)
		}
	}
}

// Walk every object, including empty run-start payloads, to exercise the entire
// nested schema rather than just envelope fields. Maps are test mutations only.
func objectPaths(raw []byte, path []string) [][]string {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	paths := [][]string{append([]string{}, path...)}
	for key, value := range fields {
		paths = append(paths, objectPaths(value, append(append([]string{}, path...), key))...)
	}
	return paths
}

func editObject(t *testing.T, raw []byte, path []string, edit func(map[string]json.RawMessage)) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(path) == 0 {
		edit(fields)
	} else {
		fields[path[0]] = editObject(t, fields[path[0]], path[1:], edit)
	}
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func rawAt(t *testing.T, raw []byte, path []string) []byte {
	t.Helper()
	for _, key := range path {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		raw = fields[key]
	}
	return raw
}

// Compute an otherwise valid typed CRC while preserving malformed raw fields.
// This proves rejection is not merely a stale-checksum side effect.
func resignMalformed(t *testing.T, raw []byte, original V3Record) []byte {
	t.Helper()
	var e struct {
		Record json.RawMessage `json:"record"`
		CRC32C uint32          `json:"crc32c"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	r := V3Record{Event: V3Event{Payload: reflect.New(reflect.TypeOf(original.Event.Payload).Elem()).Interface().(V3Payload)}}
	if err := json.Unmarshal(e.Record, &r); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return editObject(t, raw, nil, func(fields map[string]json.RawMessage) {
		fields["crc32c"], err = json.Marshal(crc32.Checksum(canonical, castagnoli))
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestV3StrictFieldsAtEveryLevel(t *testing.T) {
	for _, line := range v3Examples(t) {
		original, err := DecodeV3Record(line)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range objectPaths(line, nil) {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(rawAt(t, line, path), &obj); err != nil {
				t.Fatal(err)
			}
			for key := range obj {
				for _, mode := range []string{"missing", "null", "case"} {
					bad := editObject(t, line, path, func(fields map[string]json.RawMessage) {
						switch mode {
						case "missing":
							delete(fields, key)
						case "null":
							fields[key] = json.RawMessage("null")
						case "case":
							fields[strings.ToUpper(key)] = fields[key]
							delete(fields, key)
						}
					})
					// Keep envelope intact for re-signing; envelope errors are tested as-is.
					if len(path) > 0 {
						bad = resignMalformed(t, bad, original)
					}
					if _, err := DecodeV3Record(bad); err == nil {
						t.Fatalf("accepted %s %v.%s", mode, path, key)
					}
				}
				for _, escaped := range []bool{false, true} {
					name, _ := json.Marshal(key)
					if escaped {
						name = []byte(`"\u` + fmtHex(key[0]) + key[1:] + `"`)
					}
					// Insert a duplicate into the exact original object, preserving CRC.
					target := rawAt(t, line, path)
					dup := append([]byte{'{'}, name...)
					dup = append(dup, ':')
					dup = append(dup, obj[key]...)
					dup = append(dup, ',')
					dup = append(dup, target[1:]...)
					bad := bytes.Replace(line, target, dup, 1)
					if _, err := DecodeV3Record(bad); err == nil || !strings.Contains(err.Error(), "duplicate") {
						t.Fatalf("accepted duplicate %v.%s escaped=%v: %v", path, key, escaped, err)
					}
				}
			}
			bad := editObject(t, line, path, func(fields map[string]json.RawMessage) { fields["unexpected"] = json.RawMessage("0") })
			bad = resignMalformed(t, bad, original)
			if _, err := DecodeV3Record(bad); err == nil {
				t.Fatalf("accepted unknown field at %v", path)
			}
		}
	}
}

func fmtHex(b byte) string {
	const digits = "0123456789abcdef"
	return "00" + string([]byte{digits[b>>4], digits[b&15]})
}

func TestV3CanonicalCRCAndCorruption(t *testing.T) {
	for _, line := range v3Examples(t) {
		// Decode/re-encode through test maps to reorder every object; original CRC
		// must still work. Values in these fixtures are exactly representable.
		var obj any
		if err := json.Unmarshal(line, &obj); err != nil {
			t.Fatal(err)
		}
		reordered, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeV3Record(reordered); err != nil {
			t.Fatal(err)
		}
	}
	line := v3Examples(t)[1]
	for _, tt := range []struct{ from, to string }{
		{`"run_id":"fixture-1"`, `"run_id":"fixture-2"`},
		{`"sequence":2`, `"sequence":3`},
		{`"quantity":1`, `"quantity":2`},
		{`"max_position":10`, `"max_position":11`},
		{`"crc32c":2073893609`, `"crc32c":2073893608`},
	} {
		bad := bytes.Replace(line, []byte(tt.from), []byte(tt.to), 1)
		if bytes.Equal(bad, line) {
			t.Fatal("mutation missed target")
		}
		if _, err := DecodeV3Record(bad); err == nil || !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("corruption=%v", err)
		}
	}
	r := decodeExample(t, 0)
	r.Run.RunID = "fixture <&> \u2028"
	encoded, err := EncodeV3Record(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`\u003c\u0026\u003e`)) || !bytes.Contains(encoded, []byte(`\u2028`)) {
		t.Fatal("noncanonical Go escaping")
	}
	if got, err := DecodeV3Record(encoded); err != nil || got.Run != r.Run {
		t.Fatal("escaped roundtrip failed")
	}
}

func TestV3ValueValidation(t *testing.T) {
	for _, tt := range []struct {
		name  string
		index int
		edit  func(*V3Record)
	}{
		{"zero sequence", 0, func(r *V3Record) { r.Sequence = 0 }},
		{"blank run", 0, func(r *V3Record) { r.Run.RunID = " " }},
		{"negative cash", 0, func(r *V3Record) { r.Run.InitialCash = -1 }},
		{"live mode", 0, func(r *V3Record) { r.Run.Mode = "LIVE" }},
		{"wrong scale", 0, func(r *V3Record) { r.Run.MoneyScale = 100 }},
		{"wrong delay", 0, func(r *V3Record) { r.Run.QuoteDelay = 1 }},
		{"wrong fees", 0, func(r *V3Record) { r.Run.FeePolicy = "FEES" }},
		{"invalid limits", 0, func(r *V3Record) { r.Run.RiskLimits.MaxPosition = 0 }},
		{"inconsistent reservation", 1, func(r *V3Record) { *r.Event.Payload.(*V3Admission).Reservation.ReservedMoney = 1 }},
		{"inapplicable field", 1, func(r *V3Record) {
			x := domain.Quantity(1)
			r.Event.Payload.(*V3Admission).Reservation.ReservedQuantity = &x
		}},
		{"wrong type", 1, func(r *V3Record) { r.Event.Payload.(*V3Admission).Reservation.Type = "SELL_QUANTITY" }},
		{"order ordinal", 1, func(r *V3Record) { r.Event.Payload.(*V3Admission).OrderID = "order-01" }},
		{"wrong status", 1, func(r *V3Record) { r.Event.Payload.(*V3Admission).Status = domain.OrderSubmitted }},
		{"wrong symbol", 1, func(r *V3Record) { r.Event.Payload.(*V3Admission).Intent.Symbol = "OTHER" }},
		{"zero quantity", 1, func(r *V3Record) { r.Event.Payload.(*V3Admission).Intent.Quantity = 0 }},
		{"invalid quote", 1, func(r *V3Record) { r.Event.Payload.(*V3Admission).OriginQuote.Bid = 2000000 }},
		{"intent time mismatch", 1, func(r *V3Record) { r.Event.Payload.(*V3Admission).Intent.Timestamp = "2026-09-16T14:00:00Z" }},
		{"noncanonical timestamp", 2, func(r *V3Record) { r.Event.Payload.(*V3Submission).Timestamp = "2026-09-16T13:00:00+00:00" }},
		{"trailing zero timestamp", 2, func(r *V3Record) { r.Event.Payload.(*V3Submission).Timestamp = "2026-09-16T13:00:00.100Z" }},
		{"zero time", 2, func(r *V3Record) { r.Event.Payload.(*V3Submission).Timestamp = "0001-01-01T00:00:00Z" }},
		{"eligibility overflow", 2, func(r *V3Record) { r.Event.Payload.(*V3Submission).QuoteSequence = math.MaxUint64 }},
		{"wrong fill price", 3, func(r *V3Record) { r.Event.Payload.(*V3Settlement).Price++ }},
		{"notional overflow", 3, func(r *V3Record) { r.Event.Payload.(*V3Settlement).Quantity = math.MaxInt64 }},
		{"invalid fill ID", 4, func(r *V3Record) { r.Event.Payload.(*V3Finalization).FillID = "fill-0" }},
		{"invalid terminal reason", 5, func(r *V3Record) { r.Event.Payload.(*V3Terminal).Reason.Code = "UNKNOWN" }},
		{"quote cancelled", 5, func(r *V3Record) { r.Event.Payload.(*V3Terminal).Status = domain.OrderCancelled }},
		{"control zero sequence", 6, func(r *V3Record) { *r.Event.Payload.(*V3Terminal).Context.AfterQuoteSequence = 0 }},
		{"control extra quote", 6, func(r *V3Record) { r.Event.Payload.(*V3Terminal).Context.Quote = &V3Quote{} }},
		{"blank command", 6, func(r *V3Record) { *r.Event.Payload.(*V3Terminal).Context.CommandID = " " }},
		{"denial identity", 7, func(r *V3Record) { r.Event.Payload.(*V3Denial).DenialID = "wrong" }},
		{"denial wrong reason", 7, func(r *V3Record) { r.Event.Payload.(*V3Denial).Reason.Code = "INSUFFICIENT_HOLDINGS" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := decodeExample(t, tt.index)
			tt.edit(&r)
			if _, err := DecodeV3Record(uncheckedV3(t, r)); err == nil {
				t.Fatal("invalid value accepted with valid CRC")
			}
			if _, err := EncodeV3Record(r); err == nil {
				t.Fatal("encoded invalid record")
			}
		})
	}
}

func TestV3MalformedPrimitiveValues(t *testing.T) {
	line := v3Examples(t)[0]
	for _, value := range []string{`1.0`, `1e0`, `"1"`, `true`, `[]`, `{}`, `18446744073709551616`, `-1`} {
		bad := bytes.Replace(line, []byte(`"sequence":1`), []byte(`"sequence":`+value), 1)
		if _, err := DecodeV3Record(bad); err == nil || strings.Contains(err.Error(), "checksum") {
			t.Fatalf("primitive %s: %v", value, err)
		}
	}
	for _, input := range [][]byte{[]byte("null"), []byte("[]"), []byte("{}"), append(append([]byte{}, line...), []byte(" {}")...), []byte(`{"record":`)} {
		if _, err := DecodeV3Record(input); err == nil {
			t.Fatal("accepted malformed JSON")
		}
	}
}

func TestV3EncoderRejectsInvalidPayloads(t *testing.T) {
	for _, payload := range []V3Payload{nil, (*V3Start)(nil), &V3Submission{}} {
		r := decodeExample(t, 0)
		r.Event.Payload = payload
		if encoded, err := EncodeV3Record(r); err == nil || encoded != nil {
			t.Fatal("encoded invalid payload")
		}
	}
	r := decodeExample(t, 0)
	r.Event.Type = "future_event"
	if _, err := DecodeV3Record(uncheckedV3(t, r)); err == nil {
		t.Fatal("accepted unknown event type")
	}
	if _, err := EncodeV3Record(r); err == nil {
		t.Fatal("encoded unknown event type")
	}
}

func TestV3IntegerPrecisionAndChecksumRange(t *testing.T) {
	r := decodeExample(t, 0)
	r.Run.InitialCash = math.MaxInt64
	r.Run.RiskLimits.MaxOrderNotional = math.MaxInt64
	r.Run.RiskLimits.MaxPosition = math.MaxInt64
	line, err := EncodeV3Record(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeV3Record(line)
	if err != nil || got.Run != r.Run {
		t.Fatalf("int64 precision lost: %+v %v", got, err)
	}
	r = decodeExample(t, 2)
	r.Event.Payload.(*V3Submission).QuoteSequence = math.MaxUint64 - 2
	line, err = EncodeV3Record(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err = DecodeV3Record(line)
	if err != nil || got.Event.Payload.(*V3Submission).QuoteSequence != math.MaxUint64-2 {
		t.Fatal("uint64 precision lost")
	}
	for _, value := range []string{"4294967296", "-1", "1.0", "1e0", `"1"`} {
		bad := editObject(t, line, nil, func(fields map[string]json.RawMessage) { fields["crc32c"] = json.RawMessage(value) })
		if _, err := DecodeV3Record(bad); err == nil || strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("invalid checksum type/range: %s %v", value, err)
		}
	}
}

func TestVersionedStreamFailures(t *testing.T) {
	lines := v3Examples(t)
	changedSequence := decodeExample(t, 1)
	changedSequence.Sequence = 3
	changedRun := decodeExample(t, 1)
	changedRun.Run.RunID = "other"
	repeatedStart := decodeExample(t, 0)
	repeatedStart.Sequence = 2
	unknown := decodeExample(t, 0)
	unknown.Version = 4
	v2 := encode(t, records())
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"empty", nil}, {"blank", []byte("\n")},
		{"unterminated", lines[0]},
		{"torn tail", append(streamV3(lines[0]), []byte(`{"record":`)...)},
		{"gap", streamV3(lines[0], uncheckedV3(t, changedSequence))},
		{"duplicate sequence", streamV3(lines[0], lines[0])},
		{"run change", streamV3(lines[0], uncheckedV3(t, changedRun))},
		{"repeated start", streamV3(lines[0], uncheckedV3(t, repeatedStart))},
		{"no start", streamV3(lines[1])},
		{"future version", streamV3(uncheckedV3(t, unknown))},
		{"v2 then v3", append([]byte(v2), streamV3(lines[0])...)},
		{"v3 then v2", append(streamV3(lines[0]), []byte(v2)...)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := ReadVersioned(bytes.NewReader(tt.data)); err == nil || got != nil {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
	if got, err := ReadVersioned(io.MultiReader(bytes.NewReader(streamV3(lines[0])), errorReader{})); err == nil || got != nil {
		t.Fatal("reader failure returned partial journal")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestVersionedV2Compatibility(t *testing.T) {
	data := encode(t, records())
	legacy, err := Read(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadVersioned(strings.NewReader(data))
	if err != nil || got.Version != 2 || got.V3 != nil || !reflect.DeepEqual(got.V2, legacy) {
		t.Fatalf("v2 dispatch=%+v err=%v", got, err)
	}
	state, err := Recover(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cash, pos := state.Portfolio.State(); cash != 9000000 || pos != 1 {
		t.Fatal("v2 recovery changed")
	}
	for _, bad := range []string{
		strings.Replace(data, `"version":2`, `"version":1`, 1),
		strings.Replace(data, `"sequence":1`, `"sequence":1,"seque\u006ece":1`, 1),
		strings.Replace(data, `"initial_cash":10000000`, `"initial_cash":10000001`, 1),
		strings.TrimSuffix(data, "\n"),
	} {
		if _, err := Read(strings.NewReader(bad)); err == nil {
			t.Fatal("legacy invalid fixture")
		}
		if result, err := ReadVersioned(strings.NewReader(bad)); err == nil || result != nil {
			t.Fatal("dispatch accepted invalid v2")
		}
	}
	// Existing APIs do not silently gain v3 recovery or writing behavior.
	v3 := streamV3(v3Examples(t)[0])
	if _, err := Read(bytes.NewReader(v3)); err == nil {
		t.Fatal("legacy reader accepted v3")
	}
	if result, err := Recover(bytes.NewReader(v3)); err == nil || result != nil {
		t.Fatal("legacy recovery accepted v3")
	}
}
