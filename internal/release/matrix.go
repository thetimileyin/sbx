package release

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

//go:embed matrix.json
var matrixJSON []byte

// Entry is one line of matrix.json: everything else about a release is derived.
type Entry struct {
	Impl    string `json:"impl"`    // "core" or "knots"
	Version string `json:"version"` // as in release file names, e.g. "31.1" or "29.4.2.knots20260508"
}

func mustParseMatrix(b []byte) []Release {
	m, err := ParseMatrix(b)
	if err != nil {
		panic("release/matrix.json: " + err.Error())
	}
	return m
}

func ParseMatrix(b []byte) ([]Release, error) {
	var entries []Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no releases listed")
	}
	var out []Release
	for _, e := range entries {
		r, err := FromEntry(e)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// FromEntry derives a Release (name, label, download URL) from impl and version.
func FromEntry(e Entry) (Release, error) {
	nums, _, err := ParseVersion(e.Version)
	if err != nil {
		return Release{}, err
	}
	switch e.Impl {
	case "core":
		return Release{
			Name: "core-" + e.Version, Label: "Core " + e.Version, Impl: "core", Version: e.Version,
			Base: "https://bitcoincore.org/bin/bitcoin-core-" + e.Version + "/",
		}, nil
	case "knots":
		short := strings.SplitN(e.Version, ".knots", 2)[0]
		return Release{
			Name: "knots-" + short, Label: "Knots " + short, Impl: "knots", Version: e.Version,
			Base: fmt.Sprintf("https://bitcoinknots.org/files/%d.x/%s/", nums[0], e.Version),
		}, nil
	}
	return Release{}, fmt.Errorf("unknown implementation %q", e.Impl)
}

func (r Release) Entry() Entry { return Entry{Impl: r.Impl, Version: r.Version} }

// Major is the release line's major version, e.g. 31 for Core 31.1.
func (r Release) Major() int {
	nums, _, _ := ParseVersion(r.Version)
	return nums[0]
}

// Line names the release line, e.g. "core-31" or "knots-29". Patch releases share a
// line, so expected behaviour carries over and a patch that changes policy shows up.
func (r Release) Line() string { return fmt.Sprintf("%s-%d", r.Impl, r.Major()) }

// ParseVersion splits "29.4.2.knots20260508" into [29 4 2] and the build date
// "20260508" (empty for Core).
func ParseVersion(v string) (nums []int, date string, err error) {
	base := v
	if i := strings.Index(v, ".knots"); i >= 0 {
		base, date = v[:i], v[i+len(".knots"):]
	}
	for _, p := range strings.Split(base, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, "", fmt.Errorf("bad version %q", v)
		}
		nums = append(nums, n)
	}
	return nums, date, nil
}

// Newer reports whether version a is newer than b (numbers first, then build date).
func Newer(a, b string) bool {
	an, ad, _ := ParseVersion(a)
	bn, bd, _ := ParseVersion(b)
	for i := 0; i < max(len(an), len(bn)); i++ {
		x, y := 0, 0
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			return x > y
		}
	}
	return ad > bd
}
