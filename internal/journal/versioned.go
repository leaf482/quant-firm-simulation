package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// DecodedJournal contains exactly one version's records, never migrated data.
// Successful decoding is NOT successful recovery: lifecycle, identity, resource
// application and CONTROL-frontier validation are deliberately outside this API.
type DecodedJournal struct {
	Version int
	V2      []Record
	V3      []V3Record
}

// ReadVersioned dispatches strict v2/v3 codecs and validates stream framing,
// sequence, v3 run-start placement and immutable v3 run metadata. Existing Read,
// Writer and Recover remain v2-only. No v3 execution/recovery is wired here.
// On any failure, including an empty journal, no partial result is returned.
func ReadVersioned(input io.Reader) (*DecodedJournal, error) {
	r := bufio.NewReader(input)
	result := &DecodedJournal{}
	var sequence uint64
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF && len(line) == 0 {
			if sequence == 0 {
				return nil, fmt.Errorf("journal is empty")
			}
			return result, nil
		}
		if err != nil {
			return nil, fmt.Errorf("journal after sequence %d: incomplete record: %w", sequence, err)
		}
		version, err := recordVersion(line)
		if err != nil {
			return nil, err
		}
		if result.Version != 0 && result.Version != version {
			return nil, fmt.Errorf("mixed journal versions %d and %d", result.Version, version)
		}
		if sequence == ^uint64(0) {
			return nil, fmt.Errorf("journal sequence overflow")
		}
		sequence++
		switch version {
		case schemaVersion:
			record, err := decodeRecord(line)
			if err != nil {
				return nil, fmt.Errorf("journal sequence %d: %w", sequence, err)
			}
			if record.Sequence != sequence {
				return nil, fmt.Errorf("invalid journal sequence")
			}
			// Match the legacy Read contract; do not reinterpret v2 as v3.
			if strings.TrimSpace(string(record.Symbol)) == "" || record.InitialCash < 0 {
				return nil, fmt.Errorf("invalid account configuration")
			}
			if err := record.Event.validate(); err != nil {
				return nil, err
			}
			result.V2 = append(result.V2, record)
		case Version3:
			record, err := DecodeV3Record(line)
			if err != nil {
				return nil, fmt.Errorf("journal sequence %d: %w", sequence, err)
			}
			if record.Sequence != sequence {
				return nil, fmt.Errorf("invalid journal sequence")
			}
			if (sequence == 1) != (record.Event.Type == RunStarted) {
				return nil, fmt.Errorf("run_started must occur exactly once at sequence 1")
			}
			if sequence > 1 && record.Run != result.V3[0].Run {
				return nil, fmt.Errorf("v3 run metadata changed")
			}
			result.V3 = append(result.V3, record)
		default:
			return nil, fmt.Errorf("unsupported journal version %d", version)
		}
		result.Version = version
	}
}

// Inspect only the explicit version before selecting a codec. All other fields
// still undergo the selected codec's full structural and integrity checks.
func recordVersion(line []byte) (int, error) {
	e, err := object(line, []string{"record", "crc32c"}, nil)
	if err != nil {
		return 0, err
	}
	r, err := object(field(e, "record"), []string{"version"}, []string{"sequence", "run", "event", "symbol", "initial_cash"})
	if err != nil {
		return 0, err
	}
	var version int
	if err := json.Unmarshal(field(r, "version"), &version); err != nil {
		return 0, err
	}
	return version, nil
}
