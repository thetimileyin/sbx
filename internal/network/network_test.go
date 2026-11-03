package network

import (
	"strings"
	"testing"
)

func TestLine(t *testing.T) {
	for ua, want := range map[string]string{
		"/Satoshi:31.1.0/":                  "core-31",
		"/Satoshi:32.1.0(my comment)/":      "core-32",
		"/Satoshi:0.21.1/":                  "core-0",
		"/Satoshi:29.3.0/Knots:20260507/":   "knots-29",
		"/Satoshi:28.1.0/Knots:20250305/":   "knots-28",
		"/Satoshi:29.2.0/Corea:20251021/":   "", // a fork with its own policy
		"/Satoshi:32.1.0/Anarch:32.1.0(x)/": "",
		"/btcwire:0.5.0/btcd:0.25.1/":       "",
		"/CKCoinD:32.1.0/":                  "",
		"":                                  "",
	} {
		if got := Line(ua); got != want {
			t.Errorf("Line(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	snap := `{"timestamp": 1793699780, "nodes": {
		"a:8333": [70016, "/Satoshi:31.1.0/", 0, 0],
		"b:8333": [70016, "/Satoshi:31.0.0/", 0, 0],
		"c:8333": [70016, "/Satoshi:29.3.0/Knots:20260507/", 0, 0],
		"d:8333": [70016, "/btcwire:0.5.0/btcd:0.25.1/", 0, 0]}}`
	s, err := Parse(strings.NewReader(snap))
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 4 || s.Lines["core-31"] != 2 || s.Lines["knots-29"] != 1 || len(s.Lines) != 2 {
		t.Fatalf("got total %d, lines %v", s.Total, s.Lines)
	}
	if got := s.Share("core-31"); got != 0.5 {
		t.Errorf("Share(core-31) = %v, want 0.5", got)
	}
	if s.Taken.Unix() != 1793699780 {
		t.Errorf("Taken = %v", s.Taken)
	}
	if _, err := Parse(strings.NewReader(`{"nodes": {}}`)); err == nil {
		t.Error("an empty snapshot should be an error")
	}
}
