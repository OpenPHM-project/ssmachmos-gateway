package server

import (
	"math"
	"testing"
)

// The expected values below are worked out by hand from the ticket's formula,
// NOT by reading the constants back out of measurements.go, so a wrong
// constant there makes these tests fail.
//
//	volts      = raw * 2.048 V / (gain 1 * 32768 counts)   = raw * 6.25e-5 V
//	millitesla = volts / (0.0122 A/mT * 100 ohm * 4)        = volts / 4.88 V per mT
//
// So one ADC count is exactly 1/78080 mT (about 1.2807e-5 mT).
//
// Floats are compared with a tolerance. 1e-12 mT is about a ten-millionth of
// one count: loose enough for rounding, tight enough to catch a wrong constant
// (a wrong shunt, gain or counts value changes results by at least 1e-5 mT).
const fluxToleranceMilliTesla = 1e-12

func TestParseFluxData_KnownValues(t *testing.T) {
	tests := []struct {
		name string
		raw  int16
		want float64 // millitesla
	}{
		{"zero", 0, 0},
		{"one count", 1, 1.0 / 78080.0},             // 0.0000128073770 mT
		{"positive", 1000, 25.0 / 1952.0},           // 0.0128073770 mT   (0.0625 V)
		{"negative", -1000, -25.0 / 1952.0},         // -0.0128073770 mT  (-0.0625 V)
		{"half scale", 16384, 64.0 / 305.0},         // 0.2098360656 mT   (1.024 V)
		{"maximum 32767", 32767, 32767.0 / 78080.0}, // 0.4196593238 mT   (2.0479375 V)
		{"minimum -32768", -32768, -128.0 / 305.0},  // -0.4196721311 mT  (-2.048 V)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFluxData(tt.raw)

			if math.IsNaN(got) || math.IsInf(got, 0) {
				t.Fatalf("parseFluxData(%d) = %v, want a finite number", tt.raw, got)
			}
			if math.Abs(got-tt.want) > fluxToleranceMilliTesla {
				t.Errorf("parseFluxData(%d) = %.12f mT, want %.12f mT (tolerance %g)",
					tt.raw, got, tt.want, fluxToleranceMilliTesla)
			}
		})
	}
}

// Whatever the constants are, the conversion must be a straight line through
// zero: the sign follows the raw value, and doubling the input doubles the output.
func TestParseFluxData_IsLinearAndSymmetric(t *testing.T) {
	for _, raw := range []int16{1, 100, 8000, 16383, 32767} {
		pos := parseFluxData(raw)
		neg := parseFluxData(-raw)

		if pos <= 0 {
			t.Errorf("parseFluxData(%d) = %g, want a positive value", raw, pos)
		}
		if math.Abs(pos+neg) > fluxToleranceMilliTesla {
			t.Errorf("parseFluxData(%d) = %g but parseFluxData(%d) = %g, want opposite values", raw, pos, -raw, neg)
		}
		if raw <= math.MaxInt16/2 {
			if double := parseFluxData(2 * raw); math.Abs(double-2*pos) > fluxToleranceMilliTesla {
				t.Errorf("parseFluxData(%d) = %g, want twice parseFluxData(%d) = %g", 2*raw, double, raw, 2*pos)
			}
		}
	}
}