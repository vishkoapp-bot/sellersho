//go:build !linux

package observability

func diskFreeRatio() (float64, bool) { return 0, false }

func processMetrics() map[string]float64 { return nil }
