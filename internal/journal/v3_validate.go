package journal

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

// validate checks only facts contained in this record. Cross-record identity,
// lifecycle, risk replay, reservations and processing frontiers belong to the
// future v3 recovery state machine, not the codec.
func (r V3Record) validate() error {
	if r.Version != Version3 || r.Sequence == 0 {
		return fmt.Errorf("invalid version or sequence")
	}
	m := r.Run
	if blank(m.RunID) || blank(string(m.Symbol)) || m.InitialCash < 0 || m.Mode != "PAPER" ||
		m.MoneyScale != 10000 || m.QuantityUnit != "WHOLE_SHARE" || m.PositionPolicy != "LONG_ONLY" ||
		m.FillPolicy != "FULL_ONLY" || m.FeePolicy != "NONE" || m.QuoteDelay != 2 ||
		m.BuyReservationPolicy != "SUBMISSION_ASK_CAP" || m.RiskLimits.MaxOrderNotional <= 0 || m.RiskLimits.MaxPosition <= 0 {
		return fmt.Errorf("invalid run policy")
	}
	switch p := r.Event.Payload.(type) {
	case *V3Start:
		return nil
	case *V3Admission:
		if p.AdmissionOrdinal == 0 || p.OrderID != domain.OrderID(fmt.Sprintf("order-%d", p.AdmissionOrdinal)) ||
			p.AdmissionID != fmt.Sprintf("admission-%d", p.AdmissionOrdinal) || p.Status != domain.OrderNew {
			return fmt.Errorf("invalid admission identity or status")
		}
		if err := validateV3Origin(p.Intent, p.OriginQuote, m.Symbol); err != nil {
			return err
		}
		if p.OriginQuote.Sequence > math.MaxUint64-2 {
			return fmt.Errorf("eligibility sequence overflow")
		}
		t := p.Reservation
		if p.Intent.Side == domain.Buy {
			if t.Type != "BUY_CASH" || t.ReferenceAsk == nil || t.ReservedMoney == nil || t.ReservedQuantity != nil {
				return fmt.Errorf("invalid BUY reservation fields")
			}
			n, err := domain.Notional(*t.ReferenceAsk, p.Intent.Quantity)
			if err != nil {
				return err
			}
			if *t.ReferenceAsk != p.OriginQuote.Ask || *t.ReservedMoney != n {
				return fmt.Errorf("inconsistent BUY reservation")
			}
		} else {
			if t.Type != "SELL_QUANTITY" || t.ReservedQuantity == nil || t.ReferenceAsk != nil || t.ReservedMoney != nil {
				return fmt.Errorf("invalid SELL reservation fields")
			}
			if *t.ReservedQuantity != p.Intent.Quantity {
				return fmt.Errorf("inconsistent SELL reservation")
			}
		}
	case *V3Submission:
		if err := validateV3OrderRef(p.OrderID, p.AdmissionID); err != nil {
			return err
		}
		if p.QuoteSequence == 0 || p.QuoteSequence > math.MaxUint64-2 {
			return fmt.Errorf("invalid submission sequence")
		}
		return validateV3Timestamp(p.Timestamp)
	case *V3Settlement:
		if err := validateV3OrderRef(p.OrderID, p.AdmissionID); err != nil {
			return err
		}
		if _, err := v3Ordinal(string(p.FillID), "fill-"); err != nil {
			return err
		}
		if p.Symbol != m.Symbol || (p.Side != domain.Buy && p.Side != domain.Sell) || p.Quantity <= 0 {
			return fmt.Errorf("invalid fill terms")
		}
		if err := p.ExecutionQuote.validate(); err != nil {
			return err
		}
		price := p.ExecutionQuote.Ask
		if p.Side == domain.Sell {
			price = p.ExecutionQuote.Bid
		}
		if p.Price != price {
			return fmt.Errorf("fill price does not match execution quote")
		}
		_, err := domain.Notional(p.Price, p.Quantity)
		return err
	case *V3Finalization:
		if err := validateV3OrderRef(p.OrderID, p.AdmissionID); err != nil {
			return err
		}
		_, err := v3Ordinal(string(p.FillID), "fill-")
		return err
	case *V3Terminal:
		if err := validateV3OrderRef(p.OrderID, p.AdmissionID); err != nil {
			return err
		}
		if p.OutcomeID != "outcome/"+string(p.OrderID) {
			return fmt.Errorf("invalid outcome identity")
		}
		c := p.Context
		switch c.Type {
		case "QUOTE":
			if c.Quote == nil || c.CommandID != nil || c.AfterQuoteSequence != nil {
				return fmt.Errorf("invalid QUOTE context fields")
			}
			if p.Status != domain.OrderRejected || (p.Reason.Code != "SUBMISSION_DECLINED" && p.Reason.Code != "BUY_RESERVATION_EXCEEDED") {
				return fmt.Errorf("invalid QUOTE terminal reason/status")
			}
			return c.Quote.validate()
		case "CONTROL":
			if c.Quote != nil || c.CommandID == nil || c.AfterQuoteSequence == nil || blank(*c.CommandID) || *c.AfterQuoteSequence == 0 {
				return fmt.Errorf("invalid CONTROL context fields")
			}
			if !((p.Status == domain.OrderRejected && p.Reason.Code == "REJECT_REQUESTED") || (p.Status == domain.OrderCancelled && p.Reason.Code == "CANCEL_REQUESTED")) {
				return fmt.Errorf("invalid CONTROL terminal reason/status")
			}
		default:
			return fmt.Errorf("invalid terminal context type")
		}
	case *V3Denial:
		if p.DenialID != "denial/"+string(p.Intent.IntentID) {
			return fmt.Errorf("invalid denial identity")
		}
		if err := validateV3Origin(p.Intent, p.OriginQuote, m.Symbol); err != nil {
			return err
		}
		if p.Intent.Side == domain.Sell {
			if p.Reason.Code != "INSUFFICIENT_HOLDINGS" {
				return fmt.Errorf("invalid SELL denial reason")
			}
		} else if p.Reason.Code != "INSUFFICIENT_CASH" && p.Reason.Code != "MAX_ORDER_NOTIONAL" && p.Reason.Code != "MAX_POSITION" {
			return fmt.Errorf("invalid BUY denial reason")
		}
	default:
		return fmt.Errorf("invalid v3 payload")
	}
	return nil
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func validateV3Timestamp(s string) error {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.IsZero() || t.UTC().Format(time.RFC3339Nano) != s {
		return fmt.Errorf("timestamp must be nonzero canonical UTC RFC3339Nano")
	}
	return nil
}

func (q V3Quote) validate() error {
	if q.Sequence == 0 || q.Bid <= 0 || q.Ask < q.Bid {
		return fmt.Errorf("invalid quote")
	}
	return validateV3Timestamp(q.Timestamp)
}

func validateV3Origin(i V3Intent, q V3Quote, symbol domain.Symbol) error {
	if blank(string(i.IntentID)) || i.Symbol != symbol || i.Quantity <= 0 || (i.Side != domain.Buy && i.Side != domain.Sell) {
		return fmt.Errorf("invalid intent")
	}
	if err := q.validate(); err != nil {
		return err
	}
	if i.Timestamp != q.Timestamp {
		return fmt.Errorf("intent timestamp differs from origin quote")
	}
	price := q.Ask
	if i.Side == domain.Sell {
		price = q.Bid
	}
	_, err := domain.Notional(price, i.Quantity)
	return err
}

func v3Ordinal(id, prefix string) (uint64, error) {
	text, ok := strings.CutPrefix(id, prefix)
	n, err := strconv.ParseUint(text, 10, 64)
	if !ok || err != nil || n == 0 || strconv.FormatUint(n, 10) != text {
		return 0, fmt.Errorf("invalid identity %q", id)
	}
	return n, nil
}

func validateV3OrderRef(order domain.OrderID, admission string) error {
	n, err := v3Ordinal(string(order), "order-")
	if err != nil {
		return err
	}
	if admission != fmt.Sprintf("admission-%d", n) {
		return fmt.Errorf("order/admission identity mismatch")
	}
	return nil
}
