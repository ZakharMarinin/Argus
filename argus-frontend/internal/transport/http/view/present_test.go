package view

import "testing"

func TestPercentOrDash(t *testing.T) {
	cases := []struct {
		value float64
		ok    bool
		want  string
	}{
		{100, true, "100%"},
		{99.996, true, "99.99%"},
		{99.99, true, "99.99%"},
		{33.333, true, "33.33%"},
		{0, true, "0.00%"},
		{0, false, "—"},
	}

	for _, c := range cases {
		got := percentOrDash(c.value, c.ok)
		if got != c.want {
			t.Errorf("percentOrDash(%v, %v) = %q, want %q", c.value, c.ok, got, c.want)
		}
	}
}
