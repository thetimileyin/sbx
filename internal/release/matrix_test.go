package release

import "testing"

func TestMatrixDerivesTheSameReleases(t *testing.T) {
	m, err := ParseMatrix([]byte(`[
		{"impl": "core", "version": "28.4"},
		{"impl": "core", "version": "31.1"},
		{"impl": "knots", "version": "29.4.2.knots20260508"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, label, base, line string }{
		{"core-28.4", "Core 28.4", "https://bitcoincore.org/bin/bitcoin-core-28.4/", "core-28"},
		{"core-31.1", "Core 31.1", "https://bitcoincore.org/bin/bitcoin-core-31.1/", "core-31"},
		{"knots-29.4.2", "Knots 29.4.2", "https://bitcoinknots.org/files/29.x/29.4.2.knots20260508/", "knots-29"},
	}
	if len(m) != len(want) {
		t.Fatalf("got %d releases, want %d", len(m), len(want))
	}
	for i, w := range want {
		r := m[i]
		if r.Name != w.name || r.Label != w.label || r.Base != w.base || r.Line() != w.line {
			t.Errorf("%s: name %q label %q base %q line %q", w.name, r.Name, r.Label, r.Base, r.Line())
		}
	}
}

// The shipped matrix must parse and list both implementations, whatever its versions.
func TestShippedMatrix(t *testing.T) {
	impls := map[string]bool{}
	for _, r := range Matrix {
		impls[r.Impl] = true
	}
	if !impls["core"] || !impls["knots"] {
		t.Fatalf("matrix lists %v", impls)
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
