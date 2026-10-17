package release

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func needGPGV(t *testing.T) {
	t.Helper()
	if _, err := gpgvPath(); err != nil {
		t.Skip("gpgv not installed")
	}
}

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVerifySumsReal(t *testing.T) {
	needGPGV(t)
	for impl, prefix := range map[string]string{"core": "core-31.1", "knots": "knots-29.4.2"} {
		signers, err := VerifySums(impl, read(t, prefix+"-SHA256SUMS"), read(t, prefix+"-SHA256SUMS.asc"))
		if err != nil {
			t.Fatalf("%s: %v", impl, err)
		}
		if len(signers) < MinSignatures {
			t.Fatalf("%s: %d signers", impl, len(signers))
		}
	}
}

func TestVerifySumsRejectsTampering(t *testing.T) {
	needGPGV(t)
	sums := read(t, "core-31.1-SHA256SUMS")
	tampered := bytes.Replace(sums, []byte("arm64-apple-darwin.tar.gz"), []byte("arm64-apple-darwin.tar.gx"), 1)
	if _, err := VerifySums("core", tampered, read(t, "core-31.1-SHA256SUMS.asc")); err == nil {
		t.Fatal("tampered SHA256SUMS was accepted")
	}
	// Signatures from keys sbx doesn't bundle don't count: Core's file checked against Knots' keys.
	if signers, err := VerifySums("knots", sums, read(t, "core-31.1-SHA256SUMS.asc")); err == nil {
		t.Fatalf("Core signatures accepted with the Knots keyring (%d signers)", len(signers))
	}
}

// Full fetch of one release into a temporary home. Downloads ~35 MB, so opt-in.
func TestFetchEndToEnd(t *testing.T) {
	if os.Getenv("SBX_FETCH_TEST") == "" {
		t.Skip("set SBX_FETCH_TEST=1 to download and verify a real release")
	}
	needGPGV(t)
	home := t.TempDir()
	if err := Fetch(home, Matrix[0], io.Discard, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Bitcoind(home, Matrix[0]); err != nil {
		t.Fatal(err)
	}
}
