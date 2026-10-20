package main

import (
	"fmt"
	"io"
	"nimbleterminal.dev/ntmaps/internal/city"
)

func readSource(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, city.MaxSourceBytes+1))
	if err == nil && len(b) > city.MaxSourceBytes {
		err = fmt.Errorf("source exceeds %d bytes", city.MaxSourceBytes)
	}
	return b, err
}
