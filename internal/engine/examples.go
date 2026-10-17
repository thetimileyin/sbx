package engine

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Example is a ready-made case: a transaction or package, plus any mempool setup.
type Example struct {
	ID    string `json:"id"`
	Group string `json:"group"` // basic | package | mempool
	Title string `json:"title"`
	build func(b *Builder) (txs, mempool []string)
}

func one(h string) []string { return []string{h} }

// pair builds a parent spending a fresh coin and a child spending the parent's change
// (and its anchor output, if it has one).
func (b *Builder) pair(parentRate, childRate float64, version uint32, anchor bool, childExtra int) (Built, Built) {
	outs := []Out{{10_000, b.Address()}}
	if anchor {
		outs = append(outs, Out{0, P2A})
	}
	parent := b.Make([]Coin{b.FreshCoin()}, outs, Opts{FeeRate: parentRate, Version: version})
	spend := []Coin{parent.Outputs[len(parent.Outputs)-1]}
	if anchor {
		spend = append(spend, parent.Outputs[1])
	}
	var extra []Out
	for range childExtra {
		extra = append(extra, Out{1_000, b.Address()})
	}
	child := b.Make(spend, extra, Opts{FeeRate: childRate, Version: version})
	return parent, child
}

func pkg(p, c Built) ([]string, []string) { return []string{p.Hex, c.Hex}, nil }

var tprvRe = regexp.MustCompile(`^wpkh\(([tx]prv[0-9A-Za-z]+)/84h/1h/0h/0/\*\)`)

// multisig returns the scriptPubKey of a 2-of-3 P2WSH multisig the sandbox wallet can
// sign for. Descriptor wallets only sign scripts they know, so the first call imports
// wsh(multi(2, …/10/*, …/11/*, …/12/*)) built from the wallet's own regtest key.
func (b *Builder) multisig() string {
	var descs struct {
		Descriptors []struct {
			Desc string `json:"desc"`
		} `json:"descriptors"`
	}
	b.call(&descs, "listdescriptors", true)
	var key string
	for _, d := range descs.Descriptors {
		if m := tprvRe.FindStringSubmatch(d.Desc); m != nil {
			key = m[1]
			break
		}
	}
	if key == "" {
		panic(buildErr{fmt.Errorf("no private wpkh descriptor in the sandbox wallet")})
	}
	desc := fmt.Sprintf("wsh(multi(2,%[1]s/84h/1h/0h/10/*,%[1]s/84h/1h/0h/11/*,%[1]s/84h/1h/0h/12/*))", key)
	var info struct {
		Checksum string `json:"checksum"`
	}
	b.call(&info, "getdescriptorinfo", desc)
	desc += "#" + info.Checksum
	// Import once. The wallet widens the range itself afterwards, and re-importing
	// with a smaller range is refused, so skip it when the descriptor is already there.
	var pub struct {
		Descriptors []struct {
			Desc string `json:"desc"`
		} `json:"descriptors"`
	}
	b.call(&pub, "listdescriptors")
	have := false
	for _, d := range pub.Descriptors {
		if strings.HasPrefix(d.Desc, "wsh(multi(2,") && strings.Contains(d.Desc, "/10/*") {
			have = true
		}
	}
	if !have {
		var imported []struct {
			Success bool `json:"success"`
		}
		b.call(&imported, "importdescriptors", []map[string]any{{"desc": desc, "timestamp": "now", "range": []int{0, 0}}})
		if len(imported) == 0 || !imported[0].Success {
			panic(buildErr{fmt.Errorf("couldn't import the multisig descriptor")})
		}
	}
	var addrs []string
	b.call(&addrs, "deriveaddresses", desc, []int{0, 0})
	var ai struct {
		SPK string `json:"scriptPubKey"`
	}
	b.call(&ai, "getaddressinfo", addrs[0])
	return ai.SPK
}

// replacement builds an original spending a fresh coin and a conflicting
// replacement spending the same coin.
func (b *Builder) replacement(orig, repl Opts) (Built, Built) {
	coin := b.FreshCoin()
	o := b.Make([]Coin{coin}, []Out{{10_000, b.Address()}}, orig)
	r := b.Make([]Coin{coin}, []Out{{20_000, b.Address()}}, repl)
	return o, r
}

var examples = []Example{
	// Single transactions.
	{ID: "plain", Group: "basic", Title: "Plain payment at 2 sat/vB",
		build: func(b *Builder) ([]string, []string) { return one(b.Simple(2, nil, 2)), nil }},
	{ID: "fee-0.5", Group: "basic", Title: "Low fee: 0.5 sat/vB",
		build: func(b *Builder) ([]string, []string) { return one(b.Simple(0.5, nil, 2)), nil }},
	{ID: "fee-0.1", Group: "basic", Title: "Very low fee: 0.1 sat/vB",
		build: func(b *Builder) ([]string, []string) { return one(b.Simple(0.1, nil, 2)), nil }},
	{ID: "opreturn-80", Group: "basic", Title: "OP_RETURN with 80 bytes",
		build: func(b *Builder) ([]string, []string) {
			return one(b.Simple(2, []Out{{0, opReturn([]byte(strings.Repeat("x", 80)))}}, 2)), nil
		}},
	{ID: "opreturn-100", Group: "basic", Title: "OP_RETURN with 100 bytes",
		build: func(b *Builder) ([]string, []string) {
			return one(b.Simple(2, []Out{{0, opReturn([]byte(strings.Repeat("x", 100)))}}, 2)), nil
		}},
	{ID: "opreturn-two", Group: "basic", Title: "Two OP_RETURN outputs",
		build: func(b *Builder) ([]string, []string) {
			return one(b.Simple(2, []Out{{0, opReturn([]byte("aaaaaaaaaa"))}, {0, opReturn([]byte("bbbbbbbbbb"))}}, 2)), nil
		}},
	{ID: "dust", Group: "basic", Title: "Dust output (100 sats)",
		build: func(b *Builder) ([]string, []string) { return one(b.Simple(2, []Out{{100, b.Address()}}, 2)), nil }},
	{ID: "v3", Group: "basic", Title: "v3 (TRUC) transaction at 2 sat/vB",
		build: func(b *Builder) ([]string, []string) { return one(b.Simple(2, nil, 3)), nil }},
	{ID: "v3-zero", Group: "basic", Title: "v3 (TRUC) transaction with zero fee, on its own",
		build: func(b *Builder) ([]string, []string) { return one(b.Simple(0, nil, 3)), nil }},
	{ID: "locktime-future", Group: "basic", Title: "Locked until block 1,000 (not reached yet)",
		build: func(b *Builder) ([]string, []string) {
			t := b.Make([]Coin{b.FreshCoin()}, []Out{{10_000, b.Address()}},
				Opts{FeeRate: 2, Sequence: 0xfffffffe, Locktime: 1000})
			return one(t.Hex), nil
		}},
	{ID: "relative-lock", Group: "basic", Title: "Relative timelock of 10 blocks (already satisfied)",
		build: func(b *Builder) ([]string, []string) {
			t := b.Make([]Coin{b.FreshCoin()}, []Out{{10_000, b.Address()}}, Opts{FeeRate: 2, Sequence: 10})
			return one(t.Hex), nil
		}},

	// Packages: parent first, child last.
	{ID: "pkg-baseline", Group: "package", Title: "Parent 2 sat/vB + child 2 sat/vB",
		build: func(b *Builder) ([]string, []string) { return pkg(b.pair(2, 2, 2, false, 0)) }},
	{ID: "pkg-cpfp-zero-v2", Group: "package", Title: "Zero-fee v2 parent, child pays 20 sat/vB",
		build: func(b *Builder) ([]string, []string) { return pkg(b.pair(0, 20, 2, false, 0)) }},
	{ID: "pkg-cpfp-low-v2", Group: "package", Title: "0.5 sat/vB v2 parent, child pays 20 sat/vB",
		build: func(b *Builder) ([]string, []string) { return pkg(b.pair(0.5, 20, 2, false, 0)) }},
	{ID: "pkg-truc-zero", Group: "package", Title: "Zero-fee v3 parent, v3 child pays 20 sat/vB",
		build: func(b *Builder) ([]string, []string) { return pkg(b.pair(0, 20, 3, false, 0)) }},
	{ID: "pkg-anchor", Group: "package", Title: "Zero-fee v3 parent with a 0-value anchor (P2A), child spends it",
		build: func(b *Builder) ([]string, []string) { return pkg(b.pair(0, 20, 3, true, 0)) }},
	{ID: "pkg-truc-big-child", Group: "package", Title: "v3 parent with an oversized v3 child (40 outputs)",
		build: func(b *Builder) ([]string, []string) { return pkg(b.pair(0, 20, 3, false, 40)) }},
	{ID: "pkg-multisig", Group: "package", Title: "Parent pays a 2-of-3 multisig, child spends it",
		build: func(b *Builder) ([]string, []string) {
			parent := b.Make([]Coin{b.FreshCoin()}, []Out{{100_000, b.multisig()}}, Opts{FeeRate: 2})
			child := b.Make([]Coin{parent.Outputs[0]}, nil, Opts{FeeRate: 2})
			return pkg(parent, child)
		}},

	// Against a seeded mempool.
	{ID: "rbf-bump", Group: "mempool", Title: "Fee bump (RBF) from 2 to 4 sat/vB",
		build: func(b *Builder) ([]string, []string) {
			o, r := b.replacement(Opts{FeeRate: 2}, Opts{FeeRate: 4})
			return one(r.Hex), one(o.Hex)
		}},
	{ID: "rbf-small", Group: "mempool", Title: "Small fee bump: +0.5 sat/vB",
		build: func(b *Builder) ([]string, []string) {
			coin := b.FreshCoin()
			o := b.Make([]Coin{coin}, []Out{{10_000, b.Address()}}, Opts{FeeRate: 2})
			extra := int64(math.Ceil(0.5 * float64(o.VSize)))
			r := b.Make([]Coin{coin}, []Out{{10_000, b.Address()}}, Opts{Absolute: true, FeeAbs: o.Fee + extra})
			return one(r.Hex), one(o.Hex)
		}},
	{ID: "rbf-no-signal", Group: "mempool", Title: "Bump a transaction that didn't signal RBF",
		build: func(b *Builder) ([]string, []string) {
			o, r := b.replacement(Opts{FeeRate: 2, Sequence: SeqFinal}, Opts{FeeRate: 4, Sequence: SeqFinal})
			return one(r.Hex), one(o.Hex)
		}},
	{ID: "rbf-same-fee", Group: "mempool", Title: "Conflicting transaction paying the same fee",
		build: func(b *Builder) ([]string, []string) {
			coin := b.FreshCoin()
			o := b.Make([]Coin{coin}, []Out{{10_000, b.Address()}}, Opts{FeeRate: 2})
			r := b.Make([]Coin{coin}, []Out{{20_000, b.Address()}}, Opts{Absolute: true, FeeAbs: o.Fee})
			return one(r.Hex), one(o.Hex)
		}},
	{ID: "rbf-underpay-child", Group: "mempool", Title: "Bump a parent but pay less than its child",
		build: func(b *Builder) ([]string, []string) {
			coin := b.FreshCoin()
			parent := b.Make([]Coin{coin}, []Out{{10_000, b.Address()}}, Opts{FeeRate: 2})
			child := b.Make([]Coin{parent.Outputs[len(parent.Outputs)-1]}, nil, Opts{FeeRate: 10})
			r := b.Make([]Coin{coin}, []Out{{10_000, b.Address()}}, Opts{FeeRate: 4})
			return one(r.Hex), []string{parent.Hex, child.Hex}
		}},
	{ID: "chain-26", Group: "mempool", Title: "26th transaction in an unconfirmed chain",
		build: func(b *Builder) ([]string, []string) {
			var mempool []string
			prev := b.FreshCoin()
			for range 25 {
				t := b.Make([]Coin{prev}, nil, Opts{FeeRate: 2})
				mempool = append(mempool, t.Hex)
				prev = t.Outputs[len(t.Outputs)-1]
			}
			return one(b.Make([]Coin{prev}, nil, Opts{FeeRate: 2}).Hex), mempool
		}},
}

func Examples() []Example { return examples }

// BuildExample constructs a fresh copy of the example with the builder wallet.
// Nothing is broadcast, so the same coins can be reused by later builds.
func (e *Engine) BuildExample(ctx context.Context, id string) (Request, error) {
	for _, ex := range examples {
		if ex.ID != id {
			continue
		}
		if err := e.Ready(ctx); err != nil {
			return Request{}, err
		}
		var req Request
		err := e.build(ctx, func(b *Builder) { req.Txs, req.Mempool = ex.build(b) })
		return req, err
	}
	return Request{}, fmt.Errorf("no example called %q", id)
}
