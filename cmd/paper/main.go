package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/leaf482/quant-firm-simulation/internal/engine"
	"io"
	"log"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, output, diagnostics io.Writer) error {
	cfg := defaultConfig()
	flags := flag.NewFlagSet("paper", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&cfg.Mode, "mode", cfg.Mode, "execution mode (PAPER only)")
	flags.StringVar(&cfg.CSV, "csv", cfg.CSV, "quote CSV input path")
	flags.StringVar(&cfg.Symbol, "symbol", cfg.Symbol, "single instrument symbol")
	flags.StringVar(&cfg.InitialCash, "initial-cash", cfg.InitialCash, "starting cash in dollars (up to 4 decimals)")
	flags.StringVar(&cfg.MaxOrderNotional, "max-order-notional", cfg.MaxOrderNotional, "maximum BUY value in dollars")
	flags.Int64Var(&cfg.MaxPosition, "max-position", cfg.MaxPosition, "maximum position in whole shares")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	engineCfg, err := cfg.engineConfig()
	if err != nil {
		return err
	}
	file, err := os.Open(cfg.CSV)
	if err != nil {
		return fmt.Errorf("open CSV: %w", err)
	}
	defer file.Close()
	summary, err := engine.Run(file, engineCfg, log.New(diagnostics, "", 0))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "PAPER summary: quotes=%d intents=%d approvals=%d rejections=%d orders=%d fills=%d cash=%s position=%d equity=%s pnl=%s\n", summary.QuotesProcessed, summary.IntentsGenerated, summary.RiskApprovals, summary.RiskRejections, summary.OrdersCreated, summary.FillsApplied, summary.FinalCash, summary.FinalPosition, summary.FinalEquity, summary.FinalPnL)
	return err
}
