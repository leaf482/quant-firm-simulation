package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/leaf482/quant-firm-simulation/internal/engine"
	"github.com/leaf482/quant-firm-simulation/internal/journal"
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
	var journalPath, recoverPath string
	flags := flag.NewFlagSet("paper", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&cfg.Mode, "mode", cfg.Mode, "execution mode (PAPER only)")
	flags.StringVar(&cfg.CSV, "csv", cfg.CSV, "quote CSV input path")
	flags.StringVar(&cfg.Symbol, "symbol", cfg.Symbol, "single instrument symbol")
	flags.StringVar(&cfg.InitialCash, "initial-cash", cfg.InitialCash, "starting cash in dollars (up to 4 decimals)")
	flags.StringVar(&cfg.MaxOrderNotional, "max-order-notional", cfg.MaxOrderNotional, "maximum BUY value in dollars")
	flags.Int64Var(&cfg.MaxPosition, "max-position", cfg.MaxPosition, "maximum position in whole shares")
	flags.StringVar(&journalPath, "journal", "", "create a new durable JSONL journal (must not exist)")
	flags.StringVar(&recoverPath, "recover", "", "inspect a journal read-only; no simulation resume")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if recoverPath != "" {
		if cfg.Mode != paperMode {
			return fmt.Errorf("only PAPER is supported")
		}
		if journalPath != "" {
			return fmt.Errorf("-recover and -journal are mutually exclusive")
		}
		var conflicting string
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "mode" && f.Name != "recover" {
				conflicting = f.Name
			}
		})
		if conflicting != "" {
			return fmt.Errorf("-recover uses journal configuration; cannot specify -%s", conflicting)
		}
		f, err := os.Open(recoverPath)
		if err != nil {
			return err
		}
		defer f.Close()
		state, err := journal.Recover(f)
		if err != nil {
			return err
		}
		cash, position := state.Portfolio.State()
		if _, err = fmt.Fprintf(output, "PAPER recovery: records=%d orders=%d cash=%s position=%d\n", state.Records, len(state.Orders.Orders()), cash, position); err != nil {
			return err
		}
		for _, order := range state.Orders.Orders() {
			if _, err = fmt.Fprintf(output, "order_id=%s intent_id=%s status=%s\n", order.OrderID, order.IntentID, order.Status); err != nil {
				return err
			}
		}
		return nil
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
	if journalPath != "" {
		writer, err := journal.Create(journalPath, engineCfg.Symbol, engineCfg.InitialCash)
		if err != nil {
			return err
		}
		defer writer.Close()
		engineCfg.Journal = writer
	}
	summary, err := engine.Run(file, engineCfg, log.New(diagnostics, "", 0))
	if err != nil {
		return err
	}
	if engineCfg.Journal != nil {
		if err := engineCfg.Journal.Close(); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(output, "PAPER summary: quotes=%d intents=%d approvals=%d rejections=%d orders=%d fills=%d cash=%s position=%d equity=%s pnl=%s\n", summary.QuotesProcessed, summary.IntentsGenerated, summary.RiskApprovals, summary.RiskRejections, summary.OrdersCreated, summary.FillsApplied, summary.FinalCash, summary.FinalPosition, summary.FinalEquity, summary.FinalPnL)
	return err
}
