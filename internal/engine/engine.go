// Package engine runs one synchronous, deterministic paper simulation.
package engine

import (
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/marketdata"
	"github.com/leaf482/quant-firm-simulation/internal/oms"
	"github.com/leaf482/quant-firm-simulation/internal/paper"
	"github.com/leaf482/quant-firm-simulation/internal/portfolio"
	"github.com/leaf482/quant-firm-simulation/internal/risk"
	"github.com/leaf482/quant-firm-simulation/internal/strategy"
)

type Config struct {
	Symbol      domain.Symbol
	InitialCash domain.Money
	Limits      risk.Limits
}

type Summary struct {
	QuotesProcessed  int
	IntentsGenerated int
	RiskApprovals    int
	RiskRejections   int
	OrdersCreated    int
	FillsApplied     int
	FinalCash        domain.Money
	FinalPosition    domain.Quantity
	FinalEquity      domain.Money
	FinalPnL         domain.Money
}

// Run owns fresh components but does not close input. A nil logger discards logs.
// On failure, counters reflect completed steps; final account fields are only
// populated after successful EOF and valuation. An approved intent is fully
// settled before reading another quote; there are no outstanding reservations.
func Run(input io.Reader, cfg Config, logger *log.Logger) (Summary, error) {
	var result Summary
	account, err := portfolio.New(cfg.Symbol, cfg.InitialCash)
	if err != nil {
		return result, fmt.Errorf("engine configuration: %w", err)
	}
	checker, err := risk.NewChecker(cfg.Limits)
	if err != nil {
		return result, fmt.Errorf("engine configuration: %w", err)
	}
	replay, err := marketdata.NewReplay(input)
	if err != nil {
		return result, fmt.Errorf("engine replay: %w", err)
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	logger.Printf("event=simulation_start mode=PAPER symbol=%q", cfg.Symbol)
	signals, orders, broker := strategy.NewPriceMovement(), oms.NewManager(), paper.NewBroker()
	var last domain.Quote
	for {
		q, err := replay.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("engine replay: %w", err)
		}
		if q.Symbol != cfg.Symbol {
			return result, fmt.Errorf("engine quote %d: expected symbol %q, got %q", result.QuotesProcessed+1, cfg.Symbol, q.Symbol)
		}
		result.QuotesProcessed++
		last = q
		intent, err := signals.OnQuote(q)
		if err != nil {
			return result, fmt.Errorf("engine strategy quote %d: %w", result.QuotesProcessed, err)
		}
		if intent == nil {
			continue
		}
		result.IntentsGenerated++
		cash, position := account.State()
		if err := checker.Check(*intent, q, cash, position); err != nil {
			var rejection *risk.Rejection
			if !errors.As(err, &rejection) {
				return result, fmt.Errorf("engine risk intent %q: %w", intent.IntentID, err)
			}
			result.RiskRejections++
			logger.Printf("event=risk_rejection quote=%d intent_id=%q reason=%q", result.QuotesProcessed, intent.IntentID, err)
			continue
		}
		result.RiskApprovals++
		order, err := orders.Create(*intent)
		if err != nil {
			return result, fmt.Errorf("engine create intent %q: %w", intent.IntentID, err)
		}
		result.OrdersCreated++
		logger.Printf("event=order_created quote=%d intent_id=%q order_id=%q", result.QuotesProcessed, intent.IntentID, order.OrderID)
		order, err = orders.Transition(order.OrderID, domain.OrderSubmitted)
		if err != nil {
			return result, fmt.Errorf("engine submit intent %q: %w", intent.IntentID, err)
		}
		fill, err := broker.Execute(order, q)
		if err != nil {
			return result, fmt.Errorf("engine execute order %q: %w", order.OrderID, err)
		}
		logger.Printf("event=fill_created quote=%d intent_id=%q order_id=%q fill_id=%q price=%s quantity=%d", result.QuotesProcessed, intent.IntentID, fill.OrderID, fill.FillID, fill.Price, fill.Quantity)
		if err := account.Apply(fill); err != nil {
			return result, fmt.Errorf("engine apply fill %q: %w", fill.FillID, err)
		}
		result.FillsApplied++
		if _, err := orders.Transition(order.OrderID, domain.OrderFilled); err != nil {
			return result, fmt.Errorf("engine filled order %q: %w", order.OrderID, err)
		}
	}
	if result.QuotesProcessed == 0 {
		return result, fmt.Errorf("engine: no valid quotes processed")
	}
	snapshot, err := account.Snapshot(last)
	if err != nil {
		return result, fmt.Errorf("engine final snapshot: %w", err)
	}
	result.FinalCash, result.FinalPosition, result.FinalEquity, result.FinalPnL = snapshot.Cash, snapshot.Position, snapshot.Equity, snapshot.PnL
	logger.Printf("event=simulation_complete mode=PAPER quotes=%d intents=%d approvals=%d rejections=%d orders=%d fills=%d cash=%s position=%d equity=%s pnl=%s", result.QuotesProcessed, result.IntentsGenerated, result.RiskApprovals, result.RiskRejections, result.OrdersCreated, result.FillsApplied, result.FinalCash, result.FinalPosition, result.FinalEquity, result.FinalPnL)
	return result, nil
}
