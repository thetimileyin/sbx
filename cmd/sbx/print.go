package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/thetimileyin/sbx/internal/engine"
)

var headline = map[string]string{
	"all-accept":       "every version accepts",
	"disagree":         "◆ VERSIONS DISAGREE",
	"reject-same":      "every version rejects, for the same reason",
	"reject-different": "◇ every version rejects, for different reasons",
	"error":            "the check couldn't run",
}

func printResult(w io.Writer, title string, r *engine.Result) {
	if title != "" {
		fmt.Fprintf(w, "\n%s\n", title)
	}
	fmt.Fprintf(w, "  %s", headline[r.Agreement])
	if r.MempoolTxs > 0 {
		fmt.Fprintf(w, "  (%d mempool tx%s loaded first)", r.MempoolTxs, plural(r.MempoolTxs))
	}
	fmt.Fprintln(w)
	for _, t := range r.Txs {
		fee := "fee unknown"
		if t.Fee != nil {
			fee = fmt.Sprintf("%d sat, %.2f sat/vB", *t.Fee, *t.FeeRate)
		}
		fmt.Fprintf(w, "  %-9s %s…  v%d  %d vB  %s\n", t.Label, t.TxID[:12], t.Version, t.VSize, fee)
	}
	pad := "                    "
	if r.Network != nil {
		pad += "      "
	}
	for _, v := range r.Verdicts {
		label := fmt.Sprintf("%-16s", v.Label)
		if r.Network != nil {
			share := "    "
			if v.NetworkShare != nil {
				share = pct(*v.NetworkShare)
			}
			label += fmt.Sprintf(" %4s ", share)
		}
		switch {
		case v.Error != "":
			fmt.Fprintf(w, "  %s no result  %s\n", label, v.Error)
			continue
		case v.Accepted:
			line := "accept"
			if v.Evicts > 0 {
				line += fmt.Sprintf("  (replaces %d mempool tx%s)", v.Evicts, plural(v.Evicts))
			}
			fmt.Fprintf(w, "  %s %s\n", label, line)
		default:
			fmt.Fprintf(w, "  %s REJECT\n", label)
		}
		for _, c := range v.Causes {
			fmt.Fprintf(w, "%s%s: %s\n%s  → %s\n", pad, c.Tx, c.Reason, pad, c.Explain)
		}
		for _, k := range v.KnockOn {
			fmt.Fprintf(w, "%s%s: rejected only because its parent in this package was rejected\n", pad, k)
		}
		for _, s := range v.SetupIssues {
			reason, _, _ := strings.Cut(s.Reason, ",")
			fmt.Fprintf(w, "%s⚠ %s was refused here (%s), so this version isn't seeing the same mempool\n", pad, s.Tx, reason)
		}
	}
	if n := r.Network; n != nil {
		fmt.Fprintf(w, "  network: accepted by versions on %s of reachable nodes, rejected by %s; %s run versions sbx doesn't test\n",
			pct(n.Accepted), pct(n.Rejected), pct(n.Untested))
		fmt.Fprintf(w, "           (%d nodes, %s snapshot of %s; a share is for the whole release line)\n",
			n.Nodes, n.Source, n.Taken.Format("2 Jan 15:04 UTC"))
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "  note: %s\n", n)
	}
}

// pct formats a fraction as a whole percentage, showing "<1%" rather than "0%" for a
// small but non-zero share.
func pct(f float64) string {
	if f > 0 && f < 0.005 {
		return "<1%"
	}
	return fmt.Sprintf("%.0f%%", 100*f)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
