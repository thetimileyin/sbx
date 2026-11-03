// Package network estimates how much of the reachable Bitcoin network runs each
// release line, from the public Bitnodes snapshot of listening mainnet nodes. Only
// the snapshot is downloaded; nothing about the transactions being checked leaves
// the machine.
package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

const (
	SnapshotURL = "https://bitnodes.io/api/v1/snapshots/latest/"
	Source      = "bitnodes.io"
	cacheFile   = "network.json"
	refresh     = 6 * time.Hour      // fetch a new snapshot after this
	staleLimit  = 7 * 24 * time.Hour // fall back to a cached snapshot up to this old
)

// Snapshot counts reachable nodes by release line ("core-31", "knots-29"). Nodes
// running anything else (other implementations, forks with their own policy such as
// Libre Relay, unparseable user agents) are only in Total.
type Snapshot struct {
	Source  string         `json:"source"`
	Taken   time.Time      `json:"taken"`
	Fetched time.Time      `json:"fetched"`
	Total   int            `json:"total"`
	Lines   map[string]int `json:"lines"`
}

// Share is the fraction of reachable nodes on line, between 0 and 1.
func (s *Snapshot) Share(line string) float64 {
	if s == nil || s.Total == 0 {
		return 0
	}
	return float64(s.Lines[line]) / float64(s.Total)
}

var (
	coreUA  = regexp.MustCompile(`^/Satoshi:(\d+)\.\d+[^/]*/$`)
	knotsUA = regexp.MustCompile(`^/Satoshi:(\d+)\.\d+[^/]*/Knots:[^/]*/$`)
)

// Line maps a user agent to a release line, or "" for anything else. Core before
// 22.0 used 0.x versions; those count as line core-0.
func Line(ua string) string {
	if m := coreUA.FindStringSubmatch(ua); m != nil {
		return "core-" + major(m[1])
	}
	if m := knotsUA.FindStringSubmatch(ua); m != nil {
		return "knots-" + major(m[1])
	}
	return ""
}

func major(s string) string {
	n, _ := strconv.Atoi(s)
	return strconv.Itoa(n)
}

// Parse reads a Bitnodes snapshot: {"timestamp": ..., "nodes": {"addr": [protocol, "ua", ...]}}.
func Parse(r io.Reader) (*Snapshot, error) {
	var raw struct {
		Timestamp int64                        `json:"timestamp"`
		Nodes     map[string][]json.RawMessage `json:"nodes"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("reading the Bitnodes snapshot: %w", err)
	}
	if len(raw.Nodes) == 0 {
		return nil, errors.New("the Bitnodes snapshot lists no nodes")
	}
	s := &Snapshot{Source: Source, Taken: time.Unix(raw.Timestamp, 0).UTC(), Total: len(raw.Nodes), Lines: map[string]int{}}
	for _, fields := range raw.Nodes {
		var ua string
		if len(fields) < 2 || json.Unmarshal(fields[1], &ua) != nil {
			continue
		}
		if line := Line(ua); line != "" {
			s.Lines[line]++
		}
	}
	return s, nil
}

var client = &http.Client{Timeout: 30 * time.Second}

func fetch() (*Snapshot, error) {
	resp, err := client.Get(SnapshotURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", SnapshotURL, resp.Status)
	}
	return Parse(resp.Body)
}

// Load returns a snapshot no older than a few hours, cached in home. If Bitnodes
// can't be reached it falls back to a cached snapshot up to a week old.
func Load(home string) (*Snapshot, error) {
	path := filepath.Join(home, cacheFile)
	cached := readCache(path)
	if cached != nil && time.Since(cached.Fetched) < refresh {
		return cached, nil
	}
	s, err := fetch()
	if err != nil {
		if cached != nil && time.Since(cached.Taken) < staleLimit {
			return cached, nil
		}
		return nil, fmt.Errorf("couldn't get node counts from %s: %w", Source, err)
	}
	s.Fetched = time.Now().UTC()
	if b, err := json.Marshal(s); err == nil {
		_ = os.MkdirAll(home, 0o755)
		_ = os.WriteFile(path, b, 0o644)
	}
	return s, nil
}

func readCache(path string) *Snapshot {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s Snapshot
	if json.Unmarshal(b, &s) != nil || s.Total == 0 {
		return nil
	}
	return &s
}
