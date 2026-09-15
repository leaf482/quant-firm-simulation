package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	cfg := config{Mode: paperMode}
	flag.StringVar(&cfg.Mode, "mode", paperMode, "execution mode (PAPER only)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(1)
	}
	if err := cfg.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Starting quant-firm-simulation in %s mode\n", cfg.Mode)
}
