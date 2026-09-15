package main

import "testing"

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{name: "paper", mode: "PAPER"},
		{name: "empty", mode: "", wantErr: true},
		{name: "live", mode: "LIVE", wantErr: true},
		{name: "unknown", mode: "OTHER", wantErr: true},
		{name: "lowercase", mode: "paper", wantErr: true},
		{name: "whitespace", mode: " PAPER ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (config{Mode: tt.mode}).validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
