// Package release knows which node versions make up the matrix, and how to
// download, verify and unpack their official release binaries.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Release struct {
	Name    string // directory and node name, e.g. "core-31.1"
	Label   string // for people, e.g. "Core 31.1"
	Impl    string // "core" or "knots"
	Version string // as it appears in release file names
	Base    string // release directory URL, ending in "/"
}

// Matrix is every version a transaction is checked against, oldest Core first, as
// listed in matrix.json (which the release watcher, cmd/sbx-watch, keeps current).
// Pin exact releases: results are only meaningful if everyone runs the same binaries.
var Matrix = mustParseMatrix(matrixJSON)

// Newest returns the last release of the given implementation in the matrix.
func Newest(impl string) Release {
	var out Release
	for _, r := range Matrix {
		if r.Impl == impl {
			out = r
		}
	}
	return out
}

// Platform is the release-file platform string for this machine.
func Platform() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "arm64-apple-darwin", nil
	case "darwin/amd64":
		return "x86_64-apple-darwin", nil
	case "linux/amd64":
		return "x86_64-linux-gnu", nil
	case "linux/arm64":
		return "aarch64-linux-gnu", nil
	}
	return "", fmt.Errorf("no release binaries for %s/%s", runtime.GOOS, runtime.GOARCH)
}

func (r Release) Tarball() (string, error) {
	p, err := Platform()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("bitcoin-%s-%s.tar.gz", r.Version, p), nil
}

func Dir(home string, r Release) string { return filepath.Join(home, "bin", r.Name) }

// Bitcoind returns the path of the installed bitcoind for r.
func Bitcoind(home string, r Release) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(Dir(home, r), "*", "bin", "bitcoind"))
	if len(matches) == 0 {
		return "", fmt.Errorf("%s isn't installed; run: sbx fetch", r.Label)
	}
	return matches[0], nil
}

var client = &http.Client{Timeout: 10 * time.Minute}

func get(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// Fetch downloads r's release archive and unpacks it, but only after:
//  1. SHA256SUMS carries valid signatures from at least MinSignatures builders whose
//     public keys are bundled with sbx (checked with gpgv), unless skipSigs is set;
//  2. the archive's SHA-256 matches its line in that file (corrupted downloads are
//     retried up to three times).
func Fetch(home string, r Release, log io.Writer, skipSigs bool) error {
	if _, err := Bitcoind(home, r); err == nil {
		fmt.Fprintf(log, "  %-14s already installed\n", r.Name)
		return nil
	}
	name, err := r.Tarball()
	if err != nil {
		return err
	}
	sums, err := get(r.Base + "SHA256SUMS")
	if err != nil {
		return err
	}
	if skipSigs {
		fmt.Fprintf(log, "  %-14s WARNING: signature check skipped; trusting SHA256SUMS from %s as-is\n", r.Name, r.Base)
	} else {
		sig, err := get(r.Base + "SHA256SUMS.asc")
		if err != nil {
			return err
		}
		signers, err := VerifySums(r.Impl, sums, sig)
		if err != nil {
			return fmt.Errorf("%s: %w", r.Label, err)
		}
		fmt.Fprintf(log, "  %-14s SHA256SUMS signed by %d builders (need %d)\n", r.Name, len(signers), MinSignatures)
	}
	var expected string
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == name {
			expected = f[0]
		}
	}
	if expected == "" {
		return fmt.Errorf("%s isn't listed in %sSHA256SUMS", name, r.Base)
	}

	var data []byte
	for attempt := 1; attempt <= 3 && data == nil; attempt++ {
		fmt.Fprintf(log, "  %-14s downloading %s (attempt %d)\n", r.Name, name, attempt)
		body, err := get(r.Base + name)
		if err != nil {
			fmt.Fprintf(log, "  %-14s %v\n", r.Name, err)
			continue
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != expected {
			fmt.Fprintf(log, "  %-14s hash mismatch on %d bytes, discarding\n", r.Name, len(body))
			continue
		}
		data = body
	}
	if data == nil {
		return fmt.Errorf("couldn't get a copy of %s matching SHA256SUMS; nothing was installed", name)
	}
	fmt.Fprintf(log, "  %-14s sha256 ok\n", r.Name)

	dest := Dir(home, r)
	partial := dest + ".partial"
	os.RemoveAll(partial)
	if err := extract(data, partial); err != nil {
		os.RemoveAll(partial)
		return fmt.Errorf("unpacking %s: %w", name, err)
	}
	os.RemoveAll(dest)
	return os.Rename(partial, dest)
}
