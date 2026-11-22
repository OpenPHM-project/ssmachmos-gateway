package server

import (
	"math"
	"testing"
)

// The expected temperatures come from the standard PT1000 curve (IEC 60751),
// NOT from the constants in measurements.go, so a wrong constant there makes
// these tests fail. The raw readings are what the ADC would give for the
// resistance of a PT1000 at that temperature:
//
//	resistance = raw / 32767 * 1500 ohm
//
// One ADC count is about 0.046 ohm, which is about 0.012 degrees C. Rounding the
// resistance to a whole count therefore moves the result by up to about
// 0.006 degrees C, so the tolerance is 0.01 degrees C.
const temperatureToleranceCelsius = 0.01

func TestParseTemperatureData_KnownValues(t *testing.T) {
	tests := []struct {
		name string
		raw  int16
		want float64 // degrees C
	}{
		// Below 1000 ohm (the quadratic approximation is used).
		{"-40 C (842.71 ohm)", 18409, -40},
		{"-20 C (921.60 ohm)", 20132, -20},
		// The ticket's example: the reading that gives 1000 ohm is about 0 C.
		{"0 C (1000 ohm)", 21845, 0},
		// 1000 ohm and above (the Callendar-Van Dusen equation is used).
		{"25 C (1097.35 ohm)", 23971, 25},
		{"50 C (1193.97 ohm)", 26082, 50},
		{"100 C (1385.06 ohm)", 30256, 100},
		{"125 C (1479.51 ohm)", 32319, 125},
		// Largest possible reading: 1500 ohm, the ADC reference resistor.
		// Checked with the standard curve: 1000 * (1 + A*T + B*T^2) = 1500 at T = 130.447 C.
		{"full scale (1500 ohm)", 32767, 130.447},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTemperatureData(tt.raw)

			if err != nil {
				t.Fatalf("parseTemperatureData(%d) returned error %v, want nil", tt.raw, err)
			}
			if math.Abs(got-tt.want) > temperatureToleranceCelsius {
				t.Errorf("parseTemperatureData(%d) = %.4f C, want %.4f C (tolerance %g)",
					tt.raw, got, tt.want, temperatureToleranceCelsius)
			}
		})
	}
}

// 1000 ohm is where the function switches between its two formulas
// (raw 21845 and above use one, 21844 and below the other). The result must not
// jump at that point: neighbouring readings differ by about one count (0.012 C).
func TestParseTemperatureData_NoJumpBetweenTheTwoFormulas(t *testing.T) {
	below, err := parseTemperatureData(21844) // 999.97 ohm
	if err != nil {
		t.Fatalf("raw 21844: %v", err)
	}
	above, err := parseTemperatureData(21845) // 1000.02 ohm
	if err != nil {
		t.Fatalf("raw 21845: %v", err)
	}

	if step := above - below; step <= 0 || step > 0.02 {
		t.Errorf("step between raw 21844 (%.4f C) and raw 21845 (%.4f C) is %.4f C, want a small positive step (about 0.012 C)",
			below, above, step)
	}
}

// A higher reading must never give a lower temperature, anywhere in the range.
// This catches a wrong constant in either formula.
func TestParseTemperatureData_IncreasesWithReading(t *testing.T) {
	previous, err := parseTemperatureData(math.MinInt16)
	if err != nil {
		t.Fatalf("raw %d: %v", math.MinInt16, err)
	}
	for raw := math.MinInt16 + 1; raw <= math.MaxInt16; raw++ {
		got, err := parseTemperatureData(int16(raw))
		if err != nil {
			t.Fatalf("raw %d: %v", raw, err)
		}
		if got <= previous {
			t.Fatalf("raw %d gives %.6f C, not higher than raw %d (%.6f C)", raw, got, raw-1, previous)
		}
		previous = got
	}
}

// This is what the NaN check is for: for every possible reading the function
// either returns an error or a real number, never NaN or infinity without an error.
func TestParseTemperatureData_NeverReturnsNaNOrInfinityWithoutAnError(t *testing.T) {
	for raw := math.MinInt16; raw <= math.MaxInt16; raw++ {
		got, err := parseTemperatureData(int16(raw))
		if err != nil {
			continue
		}
		if math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("parseTemperatureData(%d) = %v with no error", raw, got)
		}
	}
}

// The ticket asks for one bad value that returns an error. Today NO int16 value
// does: the square root inside the function is positive for every possible
// reading (checked for all 65536 values), so both error branches are unreachable.
// A negative reading means a negative resistance, which a real sensor cannot
// give, so it is the natural candidate. The function currently returns a
// temperature below absolute zero for it instead (-246 C for raw 0, -586 C for raw -32768).
func TestParseTemperatureData_NegativeReadingReturnsError(t *testing.T) {
	t.Skip("known gap: no input range check yet (proposal: return an error for raw readings below 0); remove this skip once it exists")

	for _, raw := range []int16{-1, -1000, math.MinInt16} {
		if got, err := parseTemperatureData(raw); err == nil {
			t.Errorf("parseTemperatureData(%d) = %.2f C with no error, want an error for a negative resistance", raw, got)
		}
	}
}