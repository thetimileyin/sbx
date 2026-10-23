// Command sbx-watch looks for new final Bitcoin Core and Knots releases and updates
// internal/release/matrix.json. Run daily by .github/workflows/watch.yml.
//
// Policy:
//   - Core: for every major line already in the matrix, move to its newest final patch;
//     add any major line newer than the newest one listed. Old lines are never dropped
//     automatically.
//   - Knots: keep one entry, the newest final release.
//   - Release candidates are ignored: a release only counts once its directory has a
//     SHA256SUMS signed by at least release.MinSignatures known builders.
//
//	go run ./cmd/sbx-watch                 # print what would change
//	go run ./cmd/sbx-watch --write         # update matrix.json
//	go run ./cmd/sbx-watch --from old.json # start from another matrix (testing)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thetimileyin/sbx/internal/release"
)

const matrixPath = "internal/release/matrix.json"

var client = &http.Client{Timeout: 2 * time.Minute}

func get(url string) ([]byte, int, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

type change struct {
	Kind     string // "update", "add", "replace"
	Old, New release.Release
	Signers  int
}

func main() {
	from := flag.String("from", matrixPath, "matrix to start from")
	write := flag.Bool("write", false, "write the result to "+matrixPath)
	summary := flag.String("summary", "", "write a Markdown summary of the changes to this file")
	flag.Parse()

	raw, err := os.ReadFile(*from)
	if err != nil {
		fail(err)
	}
	matrix, err := release.ParseMatrix(raw)
	if err != nil {
		fail(err)
	}

	core, err := finalCore(minMajor(matrix, "core"))
	if err != nil {
		fail(fmt.Errorf("listing Core releases: %w", err))
	}
	knots, err := newestKnots(minMajor(matrix, "knots"))
	if err != nil {
		fail(fmt.Errorf("listing Knots releases: %w", err))
	}

	next, changes := plan(matrix, core, knots)
	for i := range changes {
		c := &changes[i]
		n, err := verify(c.New)
		if err != nil {
			fail(fmt.Errorf("%s: %w", c.New.Label, err))
		}
		c.Signers = n
	}

	text := describe(changes)
	fmt.Print(text)
	if *summary != "" {
		if err := os.WriteFile(*summary, []byte(text), 0o644); err != nil {
			fail(err)
		}
	}
	if *write && (len(changes) > 0 || *from != matrixPath) {
		var entries []release.Entry
		for _, r := range next {
			entries = append(entries, r.Entry())
		}
		b, _ := json.MarshalIndent(entries, "", "  ")
		if err := os.WriteFile(matrixPath, append(b, '\n'), 0o644); err != nil {
			fail(err)
		}
	}
	if gh := os.Getenv("GITHUB_OUTPUT"); gh != "" {
		f, err := os.OpenFile(gh, os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			fmt.Fprintf(f, "changed=%t\nslug=%s\ntitle=%s\n", len(changes) > 0, slug(changes), title(changes))
			f.Close()
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "sbx-watch:", err)
	os.Exit(1)
}

func minMajor(m []release.Release, impl string) int {
	lo := 0
	for _, r := range m {
		if r.Impl == impl && (lo == 0 || r.Major() < lo) {
			lo = r.Major()
		}
	}
	return lo
}

var coreDir = regexp.MustCompile(`bitcoin-core-(\d+\.\d+(?:\.\d+)?)/`)

// finalCore returns the newest final release per Core major line, from minMajor up.
func finalCore(minMajor int) (map[int]release.Release, error) {
	page, _, err := get("https://bitcoincore.org/bin/")
	if err != nil {
		return nil, err
	}
	var versions []string
	seen := map[string]bool{}
	for _, m := range coreDir.FindAllStringSubmatch(string(page), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			versions = append(versions, m[1])
		}
	}
	sort.Slice(versions, func(i, j int) bool { return release.Newer(versions[i], versions[j]) })
	out := map[int]release.Release{}
	for _, v := range versions {
		r, err := release.FromEntry(release.Entry{Impl: "core", Version: v})
		if err != nil || r.Major() < minMajor {
			continue
		}
		if _, have := out[r.Major()]; have {
			continue // already found a newer final release on this line
		}
		// A release-candidate-only directory has no SHA256SUMS at its root.
		if _, code, err := get(r.Base + "SHA256SUMS"); err != nil || code != http.StatusOK {
			continue
		}
		out[r.Major()] = r
	}
	return out, nil
}

var (
	knotsMajorDir = regexp.MustCompile(`href="(\d+)\.x/"`)
	knotsRelDir   = regexp.MustCompile(`href="(\d+(?:\.\d+)+\.knots\d{8})/"`)
)

// newestKnots returns the newest final Knots release from minMajor up.
func newestKnots(minMajor int) (release.Release, error) {
	page, _, err := get("https://bitcoinknots.org/files/")
	if err != nil {
		return release.Release{}, err
	}
	var majors []int
	for _, m := range knotsMajorDir.FindAllStringSubmatch(string(page), -1) {
		if n, _ := strconv.Atoi(m[1]); n >= minMajor {
			majors = append(majors, n)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(majors)))
	for _, major := range majors {
		page, _, err := get(fmt.Sprintf("https://bitcoinknots.org/files/%d.x/", major))
		if err != nil {
			return release.Release{}, err
		}
		var versions []string
		for _, m := range knotsRelDir.FindAllStringSubmatch(string(page), -1) {
			versions = append(versions, m[1])
		}
		sort.Slice(versions, func(i, j int) bool { return release.Newer(versions[i], versions[j]) })
		for _, v := range versions {
			r, err := release.FromEntry(release.Entry{Impl: "knots", Version: v})
			if err != nil {
				continue
			}
			if _, code, err := get(r.Base + "SHA256SUMS"); err == nil && code == http.StatusOK {
				return r, nil
			}
		}
	}
	return release.Release{}, fmt.Errorf("no final Knots release found")
}

func plan(m []release.Release, core map[int]release.Release, knots release.Release) ([]release.Release, []change) {
	var next []release.Release
	var changes []change
	newestCore := 0
	for _, r := range m {
		switch r.Impl {
		case "core":
			newestCore = max(newestCore, r.Major())
			if latest, ok := core[r.Major()]; ok && release.Newer(latest.Version, r.Version) {
				changes = append(changes, change{Kind: "update", Old: r, New: latest})
				r = latest
			}
			next = append(next, r)
		case "knots":
			if knots.Version != "" && release.Newer(knots.Version, r.Version) {
				kind := "update"
				if knots.Major() != r.Major() {
					kind = "replace"
				}
				changes = append(changes, change{Kind: kind, Old: r, New: knots})
				r = knots
			}
			next = append(next, r)
		}
	}
	// New Core majors go after the newest Core line, before Knots.
	var majors []int
	for major := range core {
		if major > newestCore {
			majors = append(majors, major)
		}
	}
	sort.Ints(majors)
	insertAt := 0
	for i, r := range next {
		if r.Impl == "core" {
			insertAt = i + 1
		}
	}
	for _, major := range majors {
		r := core[major]
		changes = append(changes, change{Kind: "add", New: r})
		next = append(next[:insertAt], append([]release.Release{r}, next[insertAt:]...)...)
		insertAt++
	}
	return next, changes
}

// verify checks the release's SHA256SUMS signatures and that it ships the archives
// sbx needs on Linux and macOS.
func verify(r release.Release) (int, error) {
	sums, code, err := get(r.Base + "SHA256SUMS")
	if err != nil || code != http.StatusOK {
		return 0, fmt.Errorf("SHA256SUMS unavailable")
	}
	sig, code, err := get(r.Base + "SHA256SUMS.asc")
	if err != nil || code != http.StatusOK {
		return 0, fmt.Errorf("SHA256SUMS.asc unavailable")
	}
	signers, err := release.VerifySums(r.Impl, sums, sig)
	if err != nil {
		return len(signers), err
	}
	for _, p := range []string{"x86_64-linux-gnu", "aarch64-linux-gnu", "arm64-apple-darwin", "x86_64-apple-darwin"} {
		if !strings.Contains(string(sums), fmt.Sprintf("bitcoin-%s-%s.tar.gz", r.Version, p)) {
			return len(signers), fmt.Errorf("no %s archive listed", p)
		}
	}
	return len(signers), nil
}

func describe(changes []change) string {
	if len(changes) == 0 {
		return "The matrix is up to date.\n"
	}
	var b strings.Builder
	b.WriteString("### New releases\n\n")
	for _, c := range changes {
		switch c.Kind {
		case "add":
			fmt.Fprintf(&b, "- **Add %s**, a new release line", c.New.Label)
		case "replace":
			fmt.Fprintf(&b, "- **Replace %s with %s**, a new release line", c.Old.Label, c.New.Label)
		default:
			fmt.Fprintf(&b, "- **%s → %s**", c.Old.Label, c.New.Label)
		}
		fmt.Fprintf(&b, ": SHA256SUMS signed by %d builders (need %d). [Release files](%s)\n", c.Signers, release.MinSignatures, c.New.Base)
	}
	return b.String()
}

func slug(changes []change) string {
	var parts []string
	for _, c := range changes {
		parts = append(parts, c.New.Name)
	}
	return strings.Join(parts, "_")
}

func title(changes []change) string {
	var parts []string
	for _, c := range changes {
		parts = append(parts, c.New.Label)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Add " + strings.Join(parts, ", ") + " to the matrix"
}
