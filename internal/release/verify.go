package release

import (
	"bufio"
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Bundled public keys of the builders who sign release checksums. See keys/README.md.
//
//go:embed keys/*.gpg
var keyrings embed.FS

// MinSignatures is how many distinct builders must have signed SHA256SUMS, the same
// default Bitcoin Core's contrib/verify-binaries uses.
const MinSignatures = 3

// ErrNoGPGV means signature checking isn't possible on this machine.
var ErrNoGPGV = errors.New("gpgv not found: install GnuPG (macOS: brew install gnupg; Debian/Ubuntu: apt install gpgv) or pass --skip-signatures")

func gpgvPath() (string, error) {
	if p, err := exec.LookPath("gpgv"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/gpgv", "/usr/local/bin/gpgv", "/usr/bin/gpgv"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", ErrNoGPGV
}

// VerifySums checks the detached signature over a SHA256SUMS file with gpgv, against
// the bundled keyring for impl. It returns the fingerprints of the builders whose
// signatures were valid, and an error unless there are at least MinSignatures.
func VerifySums(impl string, sums, sig []byte) ([]string, error) {
	gpgv, err := gpgvPath()
	if err != nil {
		return nil, err
	}
	ring, err := keyrings.ReadFile("keys/" + impl + ".gpg")
	if err != nil {
		return nil, fmt.Errorf("no bundled keys for %s", impl)
	}
	dir, err := os.MkdirTemp("", "sbx-verify-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	write := func(name string, b []byte) string {
		p := dir + "/" + name
		if err == nil {
			err = os.WriteFile(p, b, 0o600)
		}
		return p
	}
	ringPath, sumsPath, sigPath := write("keys.gpg", ring), write("SHA256SUMS", sums), write("SHA256SUMS.asc", sig)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(gpgv, "--keyring", ringPath, "--status-fd", "1", sigPath, sumsPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	_ = cmd.Run() // gpgv exits non-zero if any signature can't be checked (unknown signers); count the good ones

	seen := map[string]bool{}
	var good []string
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// [GNUPG:] VALIDSIG <subkey-fpr> <date> <ts> <expire> <ver> <res> <pk-algo> <hash> <class> <primary-fpr>
		if len(f) >= 3 && f[0] == "[GNUPG:]" && f[1] == "VALIDSIG" {
			fpr := f[len(f)-1]
			if !seen[fpr] {
				seen[fpr] = true
				good = append(good, fpr)
			}
		}
	}
	if len(good) < MinSignatures {
		return good, fmt.Errorf("only %d valid builder signatures on SHA256SUMS (need %d); refusing to trust it", len(good), MinSignatures)
	}
	return good, nil
}
