package main

import "fmt"

const paperMode = "PAPER"

type config struct {
	Mode string
}

func (c config) validate() error {
	if c.Mode != paperMode {
		return fmt.Errorf("invalid mode %q: only PAPER is supported", c.Mode)
	}
	return nil
}
