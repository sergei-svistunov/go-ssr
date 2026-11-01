package reactive_test

import (
	"testing"

	"github.com/sergei-svistunov/go-ssr/pkg/reactive"
)

func TestParseValue_BoolTakesOnlyParseBoolForms(t *testing.T) {
	for in, want := range map[string]bool{"true": true, "1": true, "T": true, "false": false, "0": false, "F": false} {
		got, err := reactive.ParseValue[bool](in)
		if err != nil || got != want {
			t.Errorf("ParseValue[bool](%q) = %v, %v; want %v, nil", in, got, err, want)
		}
	}
	for _, in := range []string{"on", "yes", "off", "no", ""} {
		if got, err := reactive.ParseValue[bool](in); err == nil {
			t.Errorf("ParseValue[bool](%q) = %v, nil; want an error", in, got)
		}
	}
}

func TestParseValue_FloatRefusesNaNAndInfinities(t *testing.T) {
	for _, in := range []string{"NaN", "nan", "Inf", "+Inf", "-Inf", "infinity", "1e400"} {
		if got, err := reactive.ParseValue[float64](in); err == nil {
			t.Errorf("ParseValue[float64](%q) = %v, nil; want an error", in, got)
		}
	}
	for _, in := range []string{"NaN", "-Inf", "1e39"} {
		if got, err := reactive.ParseValue[float32](in); err == nil {
			t.Errorf("ParseValue[float32](%q) = %v, nil; want an error", in, got)
		}
	}
	if got, err := reactive.ParseValue[float64]("-2.5"); err != nil || got != -2.5 {
		t.Errorf("ParseValue[float64](%q) = %v, %v; want -2.5, nil", "-2.5", got, err)
	}
}

func TestParseValue_IntegersStayWithinTheirSize(t *testing.T) {
	if got, err := reactive.ParseValue[int8]("300"); err == nil {
		t.Errorf("ParseValue[int8](%q) = %v, nil; want an error", "300", got)
	}
	if got, err := reactive.ParseValue[uint8]("256"); err == nil {
		t.Errorf("ParseValue[uint8](%q) = %v, nil; want an error", "256", got)
	}
	if got, err := reactive.ParseValue[int16]("-32768"); err != nil || got != -32768 {
		t.Errorf("ParseValue[int16](%q) = %v, %v; want -32768, nil", "-32768", got, err)
	}
}
