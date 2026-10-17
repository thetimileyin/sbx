package engine

import (
	"regexp"
	"strings"
)

// plain maps a reject reason's leading key to plain language. Reason strings differ
// between versions and implementations (Knots says "dust-nonanchor" where Core says
// "dust"), so the real tool should generate this list from each release's source.
var plain = map[string]string{
	"min relay fee not met":          "Fee rate is below this node's minimum for relaying.",
	"mempool min fee not met":        "The mempool is full and its fee floor has risen above your fee rate.",
	"dust":                           "An output is too small to be worth spending (dust).",
	"dust-nonanchor":                 "An output is too small to be worth spending (dust). Knots only allows tiny outputs for anchors.",
	"datacarrier":                    "OP_RETURN data is larger than this node allows.",
	"multi-op-return":                "More than one OP_RETURN output. This node allows only one.",
	"scriptpubkey":                   "An output script this node won't relay (non-standard), often an OP_RETURN that's too big.",
	"bare-multisig":                  "Bare multisig output, which this node doesn't relay by default.",
	"version":                        "A transaction version this node treats as non-standard.",
	"tx-size":                        "Larger than this node's standard transaction size limit.",
	"txn-mempool-conflict":           "Spends a coin another mempool transaction already spends, and isn't a valid replacement.",
	"txn-already-in-mempool":         "This exact transaction is already in the mempool.",
	"txn-already-known":              "This transaction is already known (in the mempool or confirmed).",
	"missing-inputs":                 "Spends coins this node doesn't know. Only sandbox coins can be checked.",
	"bad-txns-inputs-missingorspent": "Spends coins this node doesn't know or that are already spent.",
	"non-final":                      "Its timelock hasn't matured yet.",
	"non-BIP68-final":                "Its relative timelock (BIP68) hasn't matured yet.",
	"too-long-mempool-chain":         "Too many unconfirmed ancestors or descendants in the mempool.",
	"bad-txns-vin-empty":             "The transaction has no inputs.",
	"bad-txns-vout-empty":            "The transaction has no outputs.",
	"bad-txns-in-belowout":           "Outputs add up to more than the inputs.",
}

var rbfDetail = []struct{ needle, text string }{
	{"not enough additional fees to relay", "A replacement must add at least this node's incremental relay fee × its size on top of what it replaces. Bump the fee a bit more."},
	{"less fees than conflicting txs", "A replacement must pay more in total than everything it evicts, including children of the original. Outbid the parent and its descendants combined."},
	{"<= old feerate", "A replacement's fee rate must be higher than the original's. The same or lower doesn't count."},
	{"too many potential replacements", "The replacement would evict too many mempool transactions."},
}

func reasonKey(r string) string {
	k, _, _ := strings.Cut(r, ",")
	k, _, _ = strings.Cut(k, " (")
	return strings.TrimSpace(k)
}

// Explain turns a node's reject reason into plain language.
func Explain(r string) string {
	if r == "" {
		return ""
	}
	if strings.HasPrefix(r, "insufficient fee") {
		for _, d := range rbfDetail {
			if strings.Contains(r, d.needle) {
				return d.text
			}
		}
		return "The replacement doesn't pay enough more than the transaction it replaces."
	}
	if t, ok := plain[reasonKey(r)]; ok {
		return t
	}
	switch {
	case strings.HasPrefix(r, "mandatory-script-verify-flag-failed"):
		return "Invalid script or signature. The transaction itself is broken; no version will accept it."
	case strings.HasPrefix(r, "non-mandatory-script-verify-flag"):
		return "The script is valid but uses something this node treats as non-standard."
	case strings.Contains(r, "TRUC"):
		return "Breaks the v3 (TRUC) rules: one unconfirmed parent, one small child (at most 1,000 vB)."
	case strings.Contains(r, "package topology"), strings.Contains(r, "package-not-child-with"):
		return "The package must be one child with its unconfirmed parents, parents listed first."
	}
	return "No plain-language explanation yet. The reason above comes straight from the node."
}

var (
	wtxidParen = regexp.MustCompile(` \(wtxid=[0-9a-f]+\)`)
	longID     = regexp.MustCompile(`\b([0-9a-f]{8})[0-9a-f]{56}\b`)
)

func shortIDs(s string) string {
	return longID.ReplaceAllString(wtxidParen.ReplaceAllString(s, ""), "$1…")
}

// notes names known policy differences that match a result. They describe what
// this matrix has been observed to do, not guarantees about the wider network.
func notes(r *Result) []string {
	if r.Agreement != "disagree" && r.Agreement != "reject-different" {
		return nil
	}
	has := func(sub string) bool {
		for _, v := range r.Verdicts {
			for _, c := range v.Causes {
				if strings.Contains(c.Reason, sub) {
					return true
				}
			}
		}
		return false
	}
	anyTx := func(f func(TxInfo) bool) bool {
		for _, t := range r.Txs {
			if f(t) {
				return true
			}
		}
		return false
	}
	var out []string
	if has("too-long-mempool-chain") {
		out = append(out, "Core 31's cluster mempool replaced the 25-transaction ancestor and descendant limits with cluster limits. Older Core versions and Knots still enforce 25.")
	}
	if has("multi-op-return") || has("datacarrier") || (has("scriptpubkey") && anyTx(func(t TxInfo) bool { return t.OpReturn })) {
		out = append(out, "Core 30 made OP_RETURN outputs over 83 bytes, and more than one OP_RETURN per transaction, standard. Core 28–29 and Knots still reject them.")
	}
	if has("min relay fee not met") {
		out = append(out, "Minimum relay fees differ: Core in this matrix relays down to 0.1 sat/vB, Knots only from 1 sat/vB.")
		if len(r.Txs) > 1 {
			out = append(out, "Whether a child can pay for a low-fee parent also depends on the version, and on whether the parent is v3.")
		}
	}
	if has("not enough additional fees") {
		out = append(out, "The minimum fee bump differs: Core here accepts an extra 0.1 sat/vB × size, Knots requires 1 sat/vB × size.")
	}
	if has("dust") && anyTx(func(t TxInfo) bool { return t.Anchor }) {
		out = append(out, "Zero-value anchor outputs in zero-fee v3 parents (ephemeral dust) are accepted from Core 29. Core 28 and Knots treat them as dust.")
	}
	return out
}
