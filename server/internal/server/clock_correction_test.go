package server

import (
	"math"
	"testing"
)

func TestApplyClockCorrection(t *testing.T) {
	tests := []struct {
		name              string
		correctionPercent float32
		wantFrequency     float64
	}{
		{"negative correction", -0.5, 995},
		{"positive correction", 0.5, 1005},
		{"zero correction", 0, 1000},
	}

	const headerFrequency uint32 = 1000

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			measurements := []map[string]any{
				{"axis": "x", "sampling_frequency": headerFrequency},
				{"axis": "y", "sampling_frequency": headerFrequency},
				{"axis": "z", "sampling_frequency": headerFrequency},
			}

			applyClockCorrection(measurements, headerFrequency, tc.correctionPercent)

			for _, measurement := range measurements {
				got, ok := measurement["sampling_frequency"].(float64)
				if !ok {
					t.Fatalf("axis %v: expected sampling_frequency to be float64, got %T",
						measurement["axis"], measurement["sampling_frequency"])
				}

				const toleranceHz = 1e-9
				if math.IsNaN(got) || math.Abs(got-tc.wantFrequency) > toleranceHz {
					t.Errorf("axis %v: got %v Hz, want %v Hz",
						measurement["axis"], got, tc.wantFrequency)
				}
			}
		})
	}
}
