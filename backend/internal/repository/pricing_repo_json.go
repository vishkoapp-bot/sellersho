package repository

import (
	"encoding/json"
	"math"
	"strings"
)

// decodeFeeOverrides parses a JSON object mapping an id to a numeric fee
// into int64 values. It is used only for the historical absolute-override
// read path (D30); its output is never a price input.
func decodeFeeOverrides(text string, into map[string]int64) error {
	var raw map[string]json.Number
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	for k, v := range raw {
		f, err := v.Float64()
		if err != nil {
			continue
		}
		into[k] = int64(math.Round(f))
	}
	return nil
}
