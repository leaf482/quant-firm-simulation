package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	var output, logs bytes.Buffer
	err := run([]string{"-csv", "../../testdata/quotes.csv", "-symbol", "AAPL", "-initial-cash", "1000", "-max-order-notional", "500", "-max-position", "10"}, &output, &logs)
	want := "PAPER summary: quotes=6 intents=5 approvals=5 rejections=0 orders=5 fills=5 cash=770.7700 position=1 equity=999.8700 pnl=-0.1300\n"
	if err != nil || output.String() != want {
		t.Fatalf("output=%q, error=%v", output.String(), err)
	}
	if !strings.Contains(logs.String(), "mode=PAPER") {
		t.Fatal("missing paper mode")
	}
}

func TestJournalCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paper.jsonl")
	var output, logs bytes.Buffer
	if err := run([]string{"-csv", "../../testdata/quotes.csv", "-journal", path}, &output, &logs); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	logs.Reset()
	if err := run([]string{"-recover", path}, &output, &logs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "records=20 orders=5 cash=770.7700 position=1") || strings.Count(output.String(), "status=FILLED") != 5 {
		t.Fatalf("recovery output=%s", output.String())
	}
	for _, args := range [][]string{
		{"-csv", "../../testdata/quotes.csv", "-journal", path},
		{"-recover", path, "-journal", path},
		{"-recover", path, "-initial-cash", "1000"},
		{"-recover", path, "-mode", "LIVE"},
	} {
		if err := run(args, &output, &logs); err == nil {
			t.Fatalf("accepted ambiguous/invalid CLI %v", args)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("inspection or rejected run modified journal")
	}
}

func TestInvalidCLI(t *testing.T) {
	for _, args := range [][]string{{"-mode", "LIVE"}, {"-initial-cash", "-1"}, {"-initial-cash", "1.00001"}, {"-max-order-notional", "0"}, {"-max-position", "0"}, {"-symbol", " "}, {"-csv", ""}, {"-csv", "missing.csv"}, {"unexpected"}, {"-unknown"}} {
		var output, logs bytes.Buffer
		if err := run(args, &output, &logs); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if output.Len() != 0 {
			t.Fatal("failed run emitted successful summary")
		}
	}
}
