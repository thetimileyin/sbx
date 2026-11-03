package engine

import (
	"math"
	"testing"

	"github.com/thetimileyin/sbx/internal/network"
	"github.com/thetimileyin/sbx/internal/release"
)

func TestAddNetwork(t *testing.T) {
	m := release.Matrix
	if len(m) < 3 {
		t.Skip("needs at least three versions in the matrix")
	}
	r := &Result{Verdicts: []Verdict{
		{Node: m[0].Name, Accepted: true},
		{Node: m[1].Name, Accepted: false},
		{Node: m[2].Name, Error: "probe failed"},
	}}
	s := &network.Snapshot{Source: "test", Total: 100, Lines: map[string]int{
		m[0].Line(): 50, m[1].Line(): 20, m[2].Line(): 10, "core-0": 5,
	}}
	r.AddNetwork(s)

	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	n := r.Network
	if n == nil || !near(n.Accepted, 0.5) || !near(n.Rejected, 0.2) || !near(n.Untested, 0.3) {
		t.Fatalf("network = %+v, want accepted 0.5, rejected 0.2, untested 0.3", n)
	}
	for i, want := range []float64{0.5, 0.2, 0.1} {
		if got := r.Verdicts[i].NetworkShare; got == nil || !near(*got, want) {
			t.Errorf("verdict %d share = %v, want %v", i, got, want)
		}
	}

	var empty Result
	empty.AddNetwork(nil)
	if empty.Network != nil {
		t.Error("a nil snapshot shouldn't add anything")
	}
}
