package engine

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const (
	MaxTxs     = 25  // Core's package limit
	MaxMempool = 200 // setup transactions loaded before a check
)

// Request is a transaction (or package, parents first and child last) to check,
// optionally against mempool transactions loaded in order beforehand.
type Request struct {
	Txs     []string `json:"txs"`
	Mempool []string `json:"mempool"`
}

type TxInfo struct {
	Label     string     `json:"label"`
	TxID      string     `json:"txid"`
	WTxID     string     `json:"wtxid"`
	Version   uint32     `json:"version"`
	VSize     int        `json:"vsize"`
	Fee       *int64     `json:"fee,omitempty"`     // nil when an input's value is unknown
	FeeRate   *float64   `json:"feerate,omitempty"` // sat/vB
	OpReturn  bool       `json:"op_return"`
	Anchor    bool       `json:"anchor"`
	Breakdown *Breakdown `json:"breakdown,omitempty"`
}

type Cause struct {
	Tx      string `json:"tx"`
	Reason  string `json:"reason"`  // raw reason from the node, ids shortened
	Explain string `json:"explain"` // plain language
}

type Verdict struct {
	Node        string   `json:"node"`
	Label       string   `json:"label"`
	Accepted    bool     `json:"accepted"`
	Causes      []Cause  `json:"causes,omitempty"`
	KnockOn     []string `json:"knock_on,omitempty"`     // rejected only because a parent was
	SetupIssues []Cause  `json:"setup_issues,omitempty"` // mempool setup txs this version refused
	Evicts      int      `json:"evicts,omitempty"`
	Error       string   `json:"error,omitempty"` // the probe failed; no verdict
}

type Result struct {
	Txs        []TxInfo  `json:"txs"`
	MempoolTxs int       `json:"mempool_txs"`
	Verdicts   []Verdict `json:"verdicts"`
	// all-accept | disagree | reject-same | reject-different | error
	Agreement string   `json:"agreement"`
	Notes     []string `json:"notes,omitempty"`
}

// AllAccepted reports whether every version accepted.
func (r *Result) AllAccepted() bool { return r.Agreement == "all-accept" }

func cleanHex(list []string) []string {
	var out []string
	for _, s := range list {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !strings.HasPrefix(s, "#") {
			out = append(out, s)
		}
	}
	return out
}

func labels(n int) []string {
	if n == 1 {
		return []string{"tx"}
	}
	out := make([]string, n)
	for i := range n - 1 {
		out[i] = fmt.Sprintf("parent %d", i+1)
	}
	if n == 2 {
		out[0] = "parent"
	}
	out[n-1] = "child"
	return out
}

// Check runs req against every version in the matrix, in parallel.
func (e *Engine) Check(ctx context.Context, req Request) (*Result, error) {
	txs, mempool := cleanHex(req.Txs), cleanHex(req.Mempool)
	switch {
	case len(txs) == 0:
		return nil, fmt.Errorf("no transaction given")
	case len(txs) > MaxTxs:
		return nil, fmt.Errorf("a package can have at most %d transactions", MaxTxs)
	case len(mempool) > MaxMempool:
		return nil, fmt.Errorf("at most %d mempool setup transactions", MaxMempool)
	}
	for i, h := range append(append([]string{}, mempool...), txs...) {
		if _, err := hex.DecodeString(h); err != nil {
			return nil, fmt.Errorf("transaction %d isn't valid hex", i+1)
		}
	}
	if err := e.Ready(ctx); err != nil {
		return nil, err
	}

	decoded := map[string]decodedTx{}
	decode := func(h string, what string) (decodedTx, error) {
		var d decodedTx
		if err := e.builder().Call(ctx, &d, "decoderawtransaction", h); err != nil {
			return d, fmt.Errorf("%s isn't a valid transaction: %v", what, err)
		}
		decoded[h] = d
		return d, nil
	}
	for i, h := range mempool {
		if _, err := decode(h, fmt.Sprintf("mempool transaction %d", i+1)); err != nil {
			return nil, err
		}
	}
	res := &Result{MempoolTxs: len(mempool)}
	names := labels(len(txs))
	for i, h := range txs {
		d, err := decode(h, names[i])
		if err != nil {
			return nil, err
		}
		info := TxInfo{Label: names[i], TxID: d.TxID, WTxID: d.Hash, Version: d.Version, VSize: d.VSize}
		for _, o := range d.Vout {
			info.OpReturn = info.OpReturn || o.SPK.Type == "nulldata"
			info.Anchor = info.Anchor || o.SPK.Hex == P2A
		}
		res.Txs = append(res.Txs, info)
	}
	e.fillFees(ctx, res, txs, decoded)

	results := make([]probeResult, len(e.Matrix))
	var wg sync.WaitGroup
	for i, n := range e.matrixNodes() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = e.probe(ctx, n, mempool, txs)
		}()
	}
	wg.Wait()

	for i, n := range e.matrixNodes() {
		res.Verdicts = append(res.Verdicts, verdict(n, results[i], res.Txs))
	}
	res.Agreement = agreement(res.Verdicts)
	res.Notes = notes(res)
	return res, nil
}

// fillFees works out each tx's fee from input values found in the request itself
// or in the builder's UTXO set, and attaches the policy-relevant breakdown.
// Unknown inputs leave the fee unset.
func (e *Engine) fillFees(ctx context.Context, res *Result, txs []string, decoded map[string]decodedTx) {
	prev := e.prevouts(ctx, decoded, txs)
	for i, h := range txs {
		d := decoded[h]
		res.Txs[i].Breakdown = e.breakdown(ctx, d, prev)
		var in, out int64
		known := true
		for _, vin := range d.Vin {
			p, ok := prev[fmt.Sprintf("%s:%d", vin.TxID, vin.Vout)]
			if !ok {
				known = false
				break
			}
			in += p.Sats
		}
		if !known {
			continue
		}
		for _, o := range d.Vout {
			out += btcToSats(o.Value)
		}
		fee := in - out
		rate := float64(fee) / float64(d.VSize)
		res.Txs[i].Fee, res.Txs[i].FeeRate = &fee, &rate
	}
}

func verdict(n node, pr probeResult, txs []TxInfo) Verdict {
	v := Verdict{Node: n.Name, Label: n.Label}
	if pr.Err != "" {
		v.Error = pr.Err
		return v
	}
	for k, s := range pr.Seeded {
		if !s.ok() {
			r := s.firstReason()
			v.SetupIssues = append(v.SetupIssues, Cause{fmt.Sprintf("mempool tx %d", k+1), shortIDs(r), Explain(r)})
		}
	}
	v.Accepted = pr.Submit.ok()
	if v.Accepted {
		v.Evicts = pr.Evicted
		return v
	}
	v.Causes, v.KnockOn = causes(pr.Submit, txs)
	return v
}

// causes separates root causes from knock-on rejections: a child whose only
// problem is that its parent in the same package was rejected.
func causes(s submitResult, txs []TxInfo) (out []Cause, knockOn []string) {
	if s.Msg != "transaction failed" {
		return []Cause{{"package", shortIDs(s.Msg), Explain(s.Msg)}}, nil
	}
	for _, t := range txs {
		r := s.PerTx[t.WTxID]
		if r == "" {
			continue
		}
		if k := reasonKey(r); (k == "missing-inputs" || k == "bad-txns-inputs-missingorspent") && len(out) > 0 {
			knockOn = append(knockOn, t.Label)
			continue
		}
		out = append(out, Cause{t.Label, shortIDs(r), Explain(r)})
	}
	if len(out) == 0 {
		out = []Cause{{"package", shortIDs(s.firstReason()), Explain(s.firstReason())}}
	}
	return out, knockOn
}

func agreement(vs []Verdict) string {
	accepted, rejected, failed := 0, 0, 0
	why := map[string]bool{}
	for _, v := range vs {
		switch {
		case v.Error != "":
			failed++
		case v.Accepted:
			accepted++
		default:
			rejected++
			var keys []string
			for _, c := range v.Causes {
				keys = append(keys, c.Explain)
			}
			sort.Strings(keys)
			why[strings.Join(keys, "|")] = true
		}
	}
	switch {
	case accepted+rejected == 0 && failed > 0:
		return "error"
	case rejected == 0:
		return "all-accept"
	case accepted > 0:
		return "disagree"
	case len(why) > 1:
		return "reject-different"
	}
	return "reject-same"
}
