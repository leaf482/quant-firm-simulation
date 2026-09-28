package journal

import "github.com/leaf482/quant-firm-simulation/internal/domain"

const (
	Version3       = 3
	RunStarted     = "run_started"
	OrderAdmitted  = "order_admitted"
	OrderSubmitted = "order_submitted"
	OrderFilled    = "order_filled"
	OrderTerminal  = "order_terminal"
	IntentDenied   = "intent_denied"
)

// V3Record and its nested structs declare canonical CRC field order. These are
// codec values, not recovered trading state. Timestamps retain canonical UTC text.
type V3Record struct {
	Version  int     `json:"version"`
	Sequence uint64  `json:"sequence"`
	Run      V3Run   `json:"run"`
	Event    V3Event `json:"event"`
}

type V3Run struct {
	RunID                string        `json:"run_id"`
	Symbol               domain.Symbol `json:"symbol"`
	InitialCash          domain.Money  `json:"initial_cash"`
	Mode                 string        `json:"mode"`
	MoneyScale           int64         `json:"money_scale"`
	QuantityUnit         string        `json:"quantity_unit"`
	PositionPolicy       string        `json:"position_policy"`
	FillPolicy           string        `json:"fill_policy"`
	FeePolicy            string        `json:"fee_policy"`
	QuoteDelay           uint64        `json:"quote_delay"`
	BuyReservationPolicy string        `json:"buy_reservation_policy"`
	RiskLimits           V3Limits      `json:"risk_limits"`
}

type V3Limits struct {
	MaxOrderNotional domain.Money    `json:"max_order_notional"`
	MaxPosition      domain.Quantity `json:"max_position"`
}

type V3Event struct {
	Type    string    `json:"type"`
	Payload V3Payload `json:"payload"`
}

// V3Payload is restricted to the seven contract payload types. FillApplied is
// shared as an event name with v2, but its v3 payload is V3Settlement.
type V3Payload interface{ v3Payload() }

type V3Start struct{}

func (V3Start) v3Payload() {}

type V3Intent struct {
	IntentID  domain.IntentID `json:"intent_id"`
	Symbol    domain.Symbol   `json:"symbol"`
	Side      domain.Side     `json:"side"`
	Quantity  domain.Quantity `json:"quantity"`
	Timestamp string          `json:"timestamp"`
}

type V3Quote struct {
	Sequence  uint64       `json:"sequence"`
	Timestamp string       `json:"timestamp"`
	Bid       domain.Price `json:"bid"`
	Ask       domain.Price `json:"ask"`
}

// Pointers preserve absence for the side-inapplicable fields of this tagged union.
type V3Reservation struct {
	Type             string           `json:"type"`
	ReferenceAsk     *domain.Price    `json:"reference_ask,omitempty"`
	ReservedMoney    *domain.Money    `json:"reserved_money,omitempty"`
	ReservedQuantity *domain.Quantity `json:"reserved_quantity,omitempty"`
}

type V3Admission struct {
	AdmissionID      string             `json:"admission_id"`
	AdmissionOrdinal uint64             `json:"admission_ordinal"`
	OrderID          domain.OrderID     `json:"order_id"`
	Intent           V3Intent           `json:"intent"`
	OriginQuote      V3Quote            `json:"origin_quote"`
	Status           domain.OrderStatus `json:"status"`
	Reservation      V3Reservation      `json:"reservation"`
}

func (V3Admission) v3Payload() {}

type V3Submission struct {
	OrderID       domain.OrderID `json:"order_id"`
	AdmissionID   string         `json:"admission_id"`
	QuoteSequence uint64         `json:"quote_sequence"`
	Timestamp     string         `json:"timestamp"`
}

func (V3Submission) v3Payload() {}

// FillID is the settlement identity; cost/proceeds and the Fill timestamp are
// derived from Price * Quantity and ExecutionQuote.Timestamp, respectively.
type V3Settlement struct {
	FillID         domain.FillID   `json:"fill_id"`
	OrderID        domain.OrderID  `json:"order_id"`
	AdmissionID    string          `json:"admission_id"`
	Symbol         domain.Symbol   `json:"symbol"`
	Side           domain.Side     `json:"side"`
	Quantity       domain.Quantity `json:"quantity"`
	Price          domain.Price    `json:"price"`
	ExecutionQuote V3Quote         `json:"execution_quote"`
}

func (V3Settlement) v3Payload() {}

type V3Finalization struct {
	OrderID     domain.OrderID `json:"order_id"`
	AdmissionID string         `json:"admission_id"`
	FillID      domain.FillID  `json:"fill_id"`
}

func (V3Finalization) v3Payload() {}

type V3Reason struct {
	Code string `json:"code"`
}

type V3Context struct {
	Type               string   `json:"type"`
	Quote              *V3Quote `json:"quote,omitempty"`
	CommandID          *string  `json:"command_id,omitempty"`
	AfterQuoteSequence *uint64  `json:"after_quote_sequence,omitempty"`
}

type V3Terminal struct {
	OutcomeID   string             `json:"outcome_id"`
	OrderID     domain.OrderID     `json:"order_id"`
	AdmissionID string             `json:"admission_id"`
	Status      domain.OrderStatus `json:"status"`
	Reason      V3Reason           `json:"reason"`
	Context     V3Context          `json:"context"`
}

func (V3Terminal) v3Payload() {}

type V3Denial struct {
	DenialID    string   `json:"denial_id"`
	Intent      V3Intent `json:"intent"`
	OriginQuote V3Quote  `json:"origin_quote"`
	Reason      V3Reason `json:"reason"`
}

func (V3Denial) v3Payload() {}
