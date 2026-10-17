package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/thetimileyin/sbx/internal/release"
	"github.com/thetimileyin/sbx/internal/rpc"
)

// submitResult is what one mempool submission (single tx or package) returned.
type submitResult struct {
	Msg   string            // "success", "transaction failed", or a package-level error
	PerTx map[string]string // wtxid -> reject reason
}

func (s submitResult) ok() bool { return s.Msg == "success" }

// firstReason is the most specific reason available.
func (s submitResult) firstReason() string {
	for _, r := range s.PerTx {
		if r != "" {
			return r
		}
	}
	return s.Msg
}

type probeResult struct {
	Err     string // the probe itself failed; no verdict
	Seeded  []submitResult
	Submit  submitResult
	Evicted int
}

// probe starts a fresh node of n's release in a temporary datadir, syncs the builder's
// chain, turns networking off, loads seed into its mempool in order, submits txs, and
// deletes the node. Nothing is relayed and the persistent nodes never change, so
// checks can run in parallel and never affect each other.
func (e *Engine) probe(ctx context.Context, n node, seed, txs []string) probeResult {
	bin, err := release.Bitcoind(e.Home, n.Rel)
	if err != nil {
		return probeResult{Err: err.Error()}
	}
	base := filepath.Join(e.Home, "probes")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return probeResult{Err: err.Error()}
	}
	datadir, err := os.MkdirTemp(base, n.Name+"-")
	if err != nil {
		return probeResult{Err: err.Error()}
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(datadir)
		}
	}()
	if err := e.writeConf(datadir); err != nil {
		return probeResult{Err: err.Error()}
	}
	port, err := freePort()
	if err != nil {
		return probeResult{Err: err.Error()}
	}
	args := append(commonArgs(datadir, port), "-listen=0", "-disablewallet=1",
		fmt.Sprintf("-connect=127.0.0.1:%d", e.builderPort()+1))
	p, err := start(bin, args)
	if err != nil {
		return probeResult{Err: err.Error()}
	}
	c := e.rpc(port)
	defer p.stop(c)

	if err := waitReady(ctx, c, p, time.Minute); err != nil {
		keep = true
		return probeResult{Err: fmt.Sprintf("probe node didn't start (%v); log kept at %s",
			err, filepath.Join(datadir, "regtest", "debug.log"))}
	}
	var target string
	if err := e.builder().Call(ctx, &target, "getbestblockhash"); err != nil {
		return probeResult{Err: "builder unreachable: " + rpc.Message(err)}
	}
	if err := waitTip(ctx, c, target, time.Minute); err != nil {
		return probeResult{Err: "probe " + err.Error()}
	}
	if err := c.Call(ctx, nil, "setnetworkactive", false); err != nil {
		return probeResult{Err: rpc.Message(err)}
	}

	var res probeResult
	for _, s := range seed {
		res.Seeded = append(res.Seeded, submit(ctx, c, []string{s}))
	}
	var before, after []string
	_ = c.Call(ctx, &before, "getrawmempool")
	res.Submit = submit(ctx, c, txs)
	_ = c.Call(ctx, &after, "getrawmempool")
	still := map[string]bool{}
	for _, id := range after {
		still[id] = true
	}
	for _, id := range before {
		if !still[id] {
			res.Evicted++ // replaced transactions and their descendants
		}
	}
	return res
}

// submit adds one transaction (sendrawtransaction) or a package (submitpackage).
// The probe's networking is off, so nothing leaves the node.
func submit(ctx context.Context, c *rpc.Client, txs []string) submitResult {
	if len(txs) == 1 {
		err := c.Call(ctx, nil, "sendrawtransaction", txs[0], 0)
		if err == nil {
			return submitResult{Msg: "success"}
		}
		var d decodedTx
		_ = c.Call(ctx, &d, "decoderawtransaction", txs[0])
		return submitResult{Msg: "transaction failed", PerTx: map[string]string{d.Hash: rpc.Message(err)}}
	}
	var res struct {
		Msg       string `json:"package_msg"`
		TxResults map[string]struct {
			Error string `json:"error"`
		} `json:"tx-results"`
	}
	if err := c.Call(ctx, &res, "submitpackage", txs); err != nil {
		return submitResult{Msg: rpc.Message(err)}
	}
	per := map[string]string{}
	for w, r := range res.TxResults {
		per[w] = r.Error
	}
	return submitResult{Msg: res.Msg, PerTx: per}
}
