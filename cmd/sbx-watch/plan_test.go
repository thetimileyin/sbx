package main

import (
	"testing"

	"github.com/thetimileyin/sbx/internal/release"
)

func rel(t *testing.T, impl, v string) release.Release {
	t.Helper()
	r, err := release.FromEntry(release.Entry{Impl: impl, Version: v})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPlan(t *testing.T) {
	m := []release.Release{rel(t, "core", "30.3"), rel(t, "core", "31.0"), rel(t, "knots", "29.4.2.knots20260508")}
	core := map[int]release.Release{
		30: rel(t, "core", "30.3"), // unchanged
		31: rel(t, "core", "31.1"), // patch update
		32: rel(t, "core", "32.0"), // new line
	}
	knots := rel(t, "knots", "30.1.knots20261101") // new major replaces the old one

	next, changes := plan(m, core, knots)
	var names []string
	for _, r := range next {
		names = append(names, r.Name)
	}
	want := []string{"core-30.3", "core-31.1", "core-32.0", "knots-30.1"}
	if len(names) != len(want) {
		t.Fatalf("next = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("next = %v, want %v", names, want)
		}
	}
	kinds := map[string]string{}
	for _, c := range changes {
		kinds[c.New.Name] = c.Kind
	}
	if kinds["core-31.1"] != "update" || kinds["core-32.0"] != "add" || kinds["knots-30.1"] != "replace" || len(changes) != 3 {
		t.Fatalf("changes = %+v", kinds)
	}
}

func TestPlanUpToDate(t *testing.T) {
	m := []release.Release{rel(t, "core", "31.1"), rel(t, "knots", "29.4.2.knots20260508")}
	_, changes := plan(m, map[int]release.Release{31: rel(t, "core", "31.1")}, rel(t, "knots", "29.4.2.knots20260508"))
	if len(changes) != 0 {
		t.Fatalf("expected no changes, got %+v", changes)
	}
}
