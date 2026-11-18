package theory

import (
	"reflect"
	"testing"
)

func TestNumbersReadBothLocales(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []float64
	}{
		{"english grouping", "causing 1,234 damage", []float64{1234}},
		{"spanish grouping", "causa 1.234 de daño", []float64{1234}},
		{"two groups", "1.234.567 de daño", []float64{1234567}},
		{"english decimal", "reduces by 2.5 sec", []float64{2.5}},
		{"spanish decimal", "reduce en 2,5 s", []float64{2.5}},
		{"percent and words", "por un 20% durante 15 sec.", []float64{20, 15}},
		{"trailing punctuation", "durante 15 sec.", []float64{15}},
		{"signs are not part of the number", "+5 de fuerza, -3 de ira", []float64{5, 3}},
		{"two numbers, one sentence", "Hiere al objetivo y le provoca 61 de daño en 15 s.", []float64{61, 15}},
		{"no numbers", "Se lanza sin números.", nil},
		{"decimal then grouping", "de 0,5 a 1.000", []float64{0.5, 1000}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Numbers(c.text)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Numbers(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

// The same sentence in both languages gives the same numbers in the same order.
func TestNumbersSameOrderAcrossLocales(t *testing.T) {
	en := "Wounds the target causing them to bleed for 61 damage over 15 sec."
	es := "Hiere al objetivo y le provoca un sangrado de 61 de daño durante 15 s."
	if a, b := Numbers(en), Numbers(es); !reflect.DeepEqual(a, b) {
		t.Fatalf("order differs: en %v, es %v", a, b)
	}
}
