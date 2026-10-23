package release

import "testing"

func TestMatrixDerivesTheSameReleases(t *testing.T) {
	want := map[string][2]string{
		"core-28.4":    {"Core 28.4", "https://bitcoincore.org/bin/bitcoin-core-28.4/"},
		"core-31.1":    {"Core 31.1", "https://bitcoincore.org/bin/bitcoin-core-31.1/"},
		"knots-29.4.2": {"Knots 29.4.2", "https://bitcoinknots.org/files/29.x/29.4.2.knots20260508/"},
	}
	found := 0
	for _, r := range Matrix {
		if w, ok := want[r.Name]; ok {
			found++
			if r.Label != w[0] || r.Base != w[1] {
				t.Errorf("%s: label %q base %q", r.Name, r.Label, r.Base)
			}
		}
	}
	if found != len(want) {
		t.Fatalf("matrix is missing expected releases")
	}
	if Matrix[3].Line() != "core-31" || Matrix[4].Line() != "knots-29" {
		t.Fatalf("lines: %s %s", Matrix[3].Line(), Matrix[4].Line())
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"31.1", "31.0", true},
		{"32.0", "31.9", true},
		{"28.4", "28.10", false},
		{"29.4.2.knots20260508", "29.4.knots20260508", true},
		{"29.4.2.knots20260601", "29.4.2.knots20260508", true},
		{"31.1", "31.1", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
