package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
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

// Golden corpus: what each version did with each example when the spike was run on
// 2026-10-15. Order follows release.Matrix: Core 28.4, 29.4, 30.3, 31.1, Knots 29.4.2.
// A new release, or a regression in sbx, shows up here first.
var golden = map[string]string{
	"plain":              "AAAAA",
	"fee-0.5":            "AAAAR",
	"fee-0.1":            "AAAAR",
	"opreturn-80":        "AAAAA",
	"opreturn-100":       "RRAAR",
	"opreturn-two":       "RRAAR",
	"dust":               "RRRRR",
	"v3":                 "AAAAA",
	"v3-zero":            "RRRRR",
	"locktime-future":    "RRRRR",
	"relative-lock":      "AAAAA",
	"pkg-baseline":       "AAAAA",
	"pkg-cpfp-zero-v2":   "RRRAR",
	"pkg-cpfp-low-v2":    "AAAAR",
	"pkg-truc-zero":      "AAAAR",
	"pkg-anchor":         "RAAAR",
	"pkg-truc-big-child": "RRRRR",
	"pkg-multisig":       "AAAAA",
	"rbf-bump":           "AAAAA",
	"rbf-small":          "AAAAR",
	"rbf-no-signal":      "AAAAA",
	"rbf-same-fee":       "RRRRR",
	"rbf-underpay-child": "RRRRR",
	"chain-26":           "RRRAR",
}

// Run with: SBX_INTEGRATION=1 SBX_HOME=... go test ./internal/engine -run Golden
// (nodes must be up: sbx up).
func TestGolden(t *testing.T) {
	if os.Getenv("SBX_INTEGRATION") == "" {
		t.Skip("set SBX_INTEGRATION=1 with running nodes to run the golden corpus")
	}
	e, err := Open(DefaultHome())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if len(golden) != len(Examples()) {
		t.Fatalf("golden has %d entries, examples has %d", len(golden), len(Examples()))
	}
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
			var got strings.Builder
			for _, v := range res.Verdicts {
				switch {
				case v.Error != "":
					got.WriteByte('E')
				case v.Accepted:
					got.WriteByte('A')
				default:
					got.WriteByte('R')
				}
			}
			if got.String() != golden[ex.ID] {
				t.Errorf("got %s, want %s", got.String(), golden[ex.ID])
			}
		})
	}
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
