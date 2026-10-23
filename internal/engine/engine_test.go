package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thetimileyin/sbx/internal/release"
)

func TestSerialize(t *testing.T) {
	got := serialize(3, []Coin{{TxID: strings.Repeat("aa", 31) + "01", Vout: 1}}, SeqRBF,
		[]Out{{1000, "0014" + strings.Repeat("11", 20)}}, 0)
	if !strings.HasPrefix(got, "03000000"+"01"+"01"+strings.Repeat("aa", 31)) {
		t.Fatalf("version, input count or reversed txid wrong: %s", got[:80])
	}
	if len(got)/2 != 82 {
		t.Fatalf("size = %d bytes, want 82", len(got)/2)
	}
	if !strings.Contains(got, "fdffffff") {
		t.Fatal("RBF sequence missing")
	}
}

func TestOpReturn(t *testing.T) {
	if got := opReturn([]byte("ab")); got != "6a026162" {
		t.Fatalf("short push: %s", got)
	}
	if got := opReturn(make([]byte, 100)); !strings.HasPrefix(got, "6a4c64") {
		t.Fatalf("OP_PUSHDATA1 expected above 75 bytes: %s", got[:6])
	}
}

func TestExplain(t *testing.T) {
	cases := map[string]string{
		"min relay fee not met, 71 < 141": "minimum for relaying",
		"dust-nonanchor":                  "Knots",
		"insufficient fee, rejecting replacement abc, less fees than conflicting txs; 1 < 2": "evicts",
		"insufficient fee, rejecting replacement abc; new feerate 1 <= old feerate 1":        "higher than the original",
		"TRUC-violation, version=3 child tx is too big: 1350 > 1000 virtual bytes":           "TRUC",
		"something-new": "No plain-language explanation yet",
	}
	for reason, want := range cases {
		if got := Explain(reason); !strings.Contains(got, want) {
			t.Errorf("Explain(%q) = %q, want it to mention %q", reason, got, want)
		}
	}
}

func TestShortIDs(t *testing.T) {
	id := strings.Repeat("ab", 32)
	got := shortIDs("tx " + id + " (wtxid=" + id + ") is too big")
	if got != "tx abababab… is too big" {
		t.Fatalf("got %q", got)
	}
}

func TestCausesKnockOn(t *testing.T) {
	txs := []TxInfo{{Label: "parent", WTxID: "p"}, {Label: "child", WTxID: "c"}}
	s := submitResult{Msg: "transaction failed", PerTx: map[string]string{
		"p": "min relay fee not met, 0 < 15", "c": "bad-txns-inputs-missingorspent"}}
	out, knock := causes(s, txs)
	if len(out) != 1 || out[0].Tx != "parent" || len(knock) != 1 || knock[0] != "child" {
		t.Fatalf("causes=%v knockOn=%v", out, knock)
	}
}

func TestAgreement(t *testing.T) {
	r := func(e string) Verdict { return Verdict{Causes: []Cause{{Explain: e}}} }
	for _, tc := range []struct {
		vs   []Verdict
		want string
	}{
		{[]Verdict{{Accepted: true}, {Accepted: true}}, "all-accept"},
		{[]Verdict{{Accepted: true}, r("a")}, "disagree"},
		{[]Verdict{r("a"), r("a")}, "reject-same"},
		{[]Verdict{r("a"), r("b")}, "reject-different"},
		{[]Verdict{{Error: "x"}}, "error"},
	} {
		if got := agreement(tc.vs); got != tc.want {
			t.Errorf("agreement = %s, want %s", got, tc.want)
		}
	}
}

// Golden corpus: what each release line did with each example, recorded from the
// official release binaries. Keyed by line ("core-31", "knots-29") so a patch release
// is held to its line's expectations, and a release line with no expectations yet
// (a new major version) is reported rather than failed.
//
// Run with: SBX_INTEGRATION=1 SBX_HOME=... go test ./internal/engine -run Golden
// (nodes must be up: sbx up). SBX_GOLDEN_REPORT=file.md also writes a Markdown table.
func TestGolden(t *testing.T) {
	if os.Getenv("SBX_INTEGRATION") == "" {
		t.Skip("set SBX_INTEGRATION=1 with running nodes to run the golden corpus")
	}
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Examples map[string]map[string]string `json:"examples"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden.Examples) != len(Examples()) {
		t.Fatalf("golden.json has %d examples, Examples() has %d", len(golden.Examples), len(Examples()))
	}
	e, err := Open(DefaultHome())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Lines with no recorded expectations at all are new.
	newLine := map[string]bool{}
	for _, r := range e.Matrix {
		newLine[r.Line()] = true
		for _, exp := range golden.Examples {
			if _, ok := exp[r.Line()]; ok {
				newLine[r.Line()] = false
				break
			}
		}
	}

	got := map[string]map[string]string{}
	for _, ex := range Examples() {
		t.Run(ex.ID, func(t *testing.T) {
			req, err := e.BuildExample(ctx, ex.ID)
			if err != nil {
				t.Fatal(err)
			}
			res, err := e.Check(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			got[ex.ID] = map[string]string{}
			for i, v := range res.Verdicts {
				line := e.Matrix[i].Line()
				verdict := "R"
				switch {
				case v.Error != "":
					verdict = "E"
				case v.Accepted:
					verdict = "A"
				}
				got[ex.ID][line] = verdict
				if newLine[line] {
					continue
				}
				if want := golden.Examples[ex.ID][line]; verdict != want {
					t.Errorf("%s (%s): got %s, want %s", e.Matrix[i].Label, line, verdict, want)
				}
			}
		})
	}
	if path := os.Getenv("SBX_GOLDEN_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(goldenReport(e.Matrix, newLine, golden.Examples, got)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// goldenReport renders every example's verdict per release, marking new lines and
// any change compared with the previous line of the same implementation.
func goldenReport(m []release.Release, newLine map[string]bool, want, got map[string]map[string]string) string {
	var b strings.Builder
	b.WriteString("| Example |")
	for _, r := range m {
		label := r.Label
		if newLine[r.Line()] {
			label += " (new)"
		}
		b.WriteString(" " + label + " |")
	}
	b.WriteString(" Notes |\n|---|")
	for range m {
		b.WriteString("---|")
	}
	b.WriteString("---|\n")
	mark := map[string]string{"A": "✓", "R": "✗", "E": "⚠", "": "–"}
	changes := 0
	for _, ex := range Examples() {
		b.WriteString("| " + ex.Title + " |")
		var notes []string
		for i, r := range m {
			v := got[ex.ID][r.Line()]
			cell := mark[v]
			if w, ok := want[ex.ID][r.Line()]; ok && !newLine[r.Line()] && w != v {
				cell = "**" + cell + "**"
				notes = append(notes, fmt.Sprintf("%s changed from %s", r.Label, mark[w]))
			}
			if newLine[r.Line()] {
				for j := i - 1; j >= 0; j-- {
					if m[j].Impl == r.Impl {
						if prev := got[ex.ID][m[j].Line()]; prev != v {
							cell = "**" + cell + "**"
							notes = append(notes, fmt.Sprintf("%s differs from %s", r.Label, m[j].Label))
							changes++
						}
						break
					}
				}
			}
			b.WriteString(" " + cell + " |")
		}
		b.WriteString(" " + strings.Join(notes, "; ") + " |\n")
	}
	fmt.Fprintf(&b, "\n%d of %d examples behave differently on the new release lines.\n", changes, len(Examples()))
	return b.String()
}

func TestDescribeScript(t *testing.T) {
	cases := map[[2]string]string{
		{"2 02aa 03bb 03cc 3 OP_CHECKMULTISIG", "multisig"}:                            "2-of-3 multisig",
		{"02aa OP_CHECKSIG 03bb OP_CHECKSIGADD 03cc OP_CHECKSIGADD 2 OP_NUMEQUAL", ""}: "2-of-3 multisig",
		{"900000 OP_CHECKLOCKTIMEVERIFY OP_DROP 02aa OP_CHECKSIG", ""}:                 "Single signature, absolute timelock (CLTV 900000)",
		{"OP_SHA256 ab OP_EQUALVERIFY 02aa OP_CHECKSIG", ""}:                           "Single signature, hash lock (secret required)",
		{"OP_TRUE", ""}: "Custom script",
	}
	for in, want := range cases {
		if got := describeScript(in[0], in[1]); got != want {
			t.Errorf("describeScript(%q) = %q, want %q", in[0], got, want)
		}
	}
}

func TestRelativeLockAndOpReturn(t *testing.T) {
	if got := relativeLock(2, 10); !strings.Contains(got, "10 blocks") {
		t.Errorf("relativeLock(2, 10) = %q", got)
	}
	if got := relativeLock(1, 10); got != "" {
		t.Errorf("version 1 has no relative locks, got %q", got)
	}
	if got := relativeLock(2, 0xfffffffd); got != "" {
		t.Errorf("disable bit set, got %q", got)
	}
	if got := opReturnBytes(opReturn(make([]byte, 100))); got != 100 {
		t.Errorf("opReturnBytes = %d, want 100", got)
	}
}
