package main

import (
	"fmt"
	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/engine"
	"github.com/leaf482/quant-firm-simulation/internal/risk"
	"strings"
)

const paperMode = "PAPER"

type config struct {
	Mode             string
	CSV              string
	Symbol           string
	InitialCash      string
	MaxOrderNotional string
	MaxPosition      int64
}

func defaultConfig() config {
	return config{Mode: paperMode, CSV: "testdata/quotes.csv", Symbol: "AAPL", InitialCash: "1000", MaxOrderNotional: "500", MaxPosition: 10}
}
func (c config) engineConfig() (engine.Config, error) {
	if c.Mode != paperMode {
		return engine.Config{}, fmt.Errorf("invalid mode %q: only PAPER is supported", c.Mode)
	}
	if strings.TrimSpace(c.CSV) == "" {
		return engine.Config{}, fmt.Errorf("CSV path must not be blank")
	}
	if strings.TrimSpace(c.Symbol) == "" {
		return engine.Config{}, fmt.Errorf("symbol must not be blank")
	}
	// Monetary flags share Price's scale and strict nonnegative decimal grammar.
	cash, err := domain.ParsePrice(c.InitialCash)
	if err != nil {
		return engine.Config{}, fmt.Errorf("initial cash: %w", err)
	}
	notional, err := domain.ParsePrice(c.MaxOrderNotional)
	if err != nil {
		return engine.Config{}, fmt.Errorf("max order notional: %w", err)
	}
	limits := risk.Limits{MaxOrderNotional: domain.Money(notional), MaxPosition: domain.Quantity(c.MaxPosition)}
	if _, err := risk.NewChecker(limits); err != nil {
		return engine.Config{}, err
	}
	return engine.Config{Symbol: domain.Symbol(c.Symbol), InitialCash: domain.Money(cash), Limits: limits}, nil
}
