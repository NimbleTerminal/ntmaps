package main

import (
	"fmt"
	"github.com/NimbleMarkets/ntcharts-osm/internal/city"
	"io"
)

func readSource(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, city.MaxSourceBytes+1))
	if err == nil && len(b) > city.MaxSourceBytes {
		err = fmt.Errorf("source exceeds %d bytes", city.MaxSourceBytes)
	}
	return b, err
}
