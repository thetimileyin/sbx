// Package engine runs the version matrix on a private regtest network and checks
// transactions against every version.
//
// Layout: one persistent node per matrix release (default policy, no wallet), plus a
// builder node that mines and holds the wallet used to construct example transactions.
// The builder never broadcasts test transactions. Checks run on throwaway probe nodes
// (see probe.go), so the persistent nodes' mempools never change.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/thetimileyin/sbx/internal/release"
	"github.com/thetimileyin/sbx/internal/rpc"
)

const (
	basePort   = 19400 // node i: rpc basePort+10i, p2p +1
	walletName = "sbx"
	rpcUser    = "sbx"
	minHeight  = 140 // stay below 150 so every coinbase is a full 50 BTC
)

type Engine struct {
	Home    string
	Matrix  []release.Release
	Builder release.Release
	pass    string
}

// DefaultHome is $SBX_HOME, or ~/.sbx.
func DefaultHome() string {
	if h := os.Getenv("SBX_HOME"); h != "" {
		return h
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return ".sbx"
	}
	return filepath.Join(dir, ".sbx")
}

// Open prepares home and loads (or creates) the local RPC password.
func Open(home string) (*Engine, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	passFile := filepath.Join(home, "rpc-password")
	b, err := os.ReadFile(passFile)
	if errors.Is(err, os.ErrNotExist) {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		b = []byte(hex.EncodeToString(buf))
		if err := os.WriteFile(passFile, b, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &Engine{
		Home:    home,
		Matrix:  release.Matrix,
		Builder: release.Newest("core"),
		pass:    strings.TrimSpace(string(b)),
	}, nil
}

type node struct {
	Name, Label string
	Rel         release.Release
	Port        int
	Builder     bool
}

func (e *Engine) nodes() []node {
	var out []node
	for i, r := range e.Matrix {
		out = append(out, node{r.Name, r.Label, r, basePort + 10*i, false})
	}
	return append(out, node{"builder", "Builder (" + e.Builder.Label + ")", e.Builder, e.builderPort(), true})
}

func (e *Engine) matrixNodes() []node { return e.nodes()[:len(e.Matrix)] }

func (e *Engine) builderPort() int         { return basePort + 10*len(e.Matrix) }
func (e *Engine) rpc(port int) *rpc.Client { return rpc.New(port, rpcUser, e.pass) }
func (e *Engine) builder() *rpc.Client     { return e.rpc(e.builderPort()) }
func (e *Engine) wallet() *rpc.Client      { return e.builder().Wallet(walletName) }

// writeConf keeps the RPC credentials out of the process list.
func (e *Engine) writeConf(datadir string) error {
	conf := fmt.Sprintf("rpcuser=%s\nrpcpassword=%s\n", rpcUser, e.pass)
	return os.WriteFile(filepath.Join(datadir, "bitcoin.conf"), []byte(conf), 0o600)
}

func commonArgs(datadir string, rpcPort int) []string {
	return []string{
		"-regtest", "-datadir=" + datadir, fmt.Sprintf("-rpcport=%d", rpcPort),
		"-rpcbind=127.0.0.1", "-rpcallowip=127.0.0.1", "-server=1",
		"-dnsseed=0", "-fixedseeds=0", "-printtoconsole=0", "-daemon=0",
	}
}

// Fetch installs every release the matrix and builder need.
func (e *Engine) Fetch(log io.Writer, skipSigs bool) error {
	for _, r := range e.Matrix {
		if err := release.Fetch(e.Home, r, log, skipSigs); err != nil {
			return err
		}
	}
	return nil
}

// Up starts any node that isn't running, connects the matrix to the builder,
// funds the builder's wallet and waits until every node has the same chain.
func (e *Engine) Up(ctx context.Context, log io.Writer) error {
	for _, n := range e.nodes() {
		c := e.rpc(n.Port)
		var info struct {
			Subversion string `json:"subversion"`
		}
		if c.Call(ctx, &info, "getnetworkinfo") == nil {
			fmt.Fprintf(log, "  %-14s running   %s\n", n.Name, info.Subversion)
			continue
		}
		bin, err := release.Bitcoind(e.Home, n.Rel)
		if err != nil {
			return err
		}
		datadir := filepath.Join(e.Home, "nodes", n.Name)
		if err := os.MkdirAll(datadir, 0o700); err != nil {
			return err
		}
		if err := e.writeConf(datadir); err != nil {
			return err
		}
		args := append(commonArgs(datadir, n.Port),
			fmt.Sprintf("-port=%d", n.Port+1), "-bind=127.0.0.1", "-listen=1")
		if n.Builder {
			// Wallet-only settings so the builder can construct low-fee transactions.
			args = append(args, "-fallbackfee=0.0001", "-mintxfee=0.00000001")
		} else {
			args = append(args, "-disablewallet=1")
		}
		p, err := start(bin, args)
		if err != nil {
			return fmt.Errorf("%s: %w", n.Name, err)
		}
		if err := waitReady(ctx, c, p, time.Minute); err != nil {
			return fmt.Errorf("%s didn't start (%v); see %s", n.Name, err,
				filepath.Join(datadir, "regtest", "debug.log"))
		}
		_ = c.Call(ctx, &info, "getnetworkinfo")
		fmt.Fprintf(log, "  %-14s started   %s\n", n.Name, info.Subversion)
	}

	peer := fmt.Sprintf("127.0.0.1:%d", e.builderPort()+1)
	for _, n := range e.matrixNodes() {
		_ = e.rpc(n.Port).Call(ctx, nil, "addnode", peer, "add") // errors if already added
	}
	if err := e.ensureWallet(ctx); err != nil {
		return err
	}
	var height int
	if err := e.builder().Call(ctx, &height, "getblockcount"); err != nil {
		return err
	}
	if height < minHeight {
		var addr string
		if err := e.wallet().Call(ctx, &addr, "getnewaddress"); err != nil {
			return err
		}
		if err := e.builder().Call(ctx, nil, "generatetoaddress", minHeight-height, addr); err != nil {
			return err
		}
	}
	if err := e.syncAll(ctx); err != nil {
		return err
	}
	fmt.Fprintf(log, "\n  all nodes synced at height %d\n", max(height, minHeight))
	return nil
}

func (e *Engine) ensureWallet(ctx context.Context) error {
	var loaded []string
	if err := e.builder().Call(ctx, &loaded, "listwallets"); err != nil {
		return err
	}
	if slices.Contains(loaded, walletName) {
		return nil
	}
	if e.builder().Call(ctx, nil, "loadwallet", walletName) == nil {
		return nil
	}
	return e.builder().Call(ctx, nil, "createwallet", walletName)
}

func (e *Engine) syncAll(ctx context.Context) error {
	var target string
	if err := e.builder().Call(ctx, &target, "getbestblockhash"); err != nil {
		return err
	}
	for _, n := range e.matrixNodes() {
		if err := waitTip(ctx, e.rpc(n.Port), target, time.Minute); err != nil {
			return fmt.Errorf("%s: %w", n.Name, err)
		}
	}
	return nil
}

func waitTip(ctx context.Context, c *rpc.Client, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var tip string
		if c.Call(ctx, &tip, "getbestblockhash") == nil && tip == target {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("didn't sync blocks from the builder in time")
}

// Down stops every node.
func (e *Engine) Down(ctx context.Context, log io.Writer) {
	for _, n := range e.nodes() {
		if e.rpc(n.Port).Call(ctx, nil, "stop") == nil {
			fmt.Fprintf(log, "  %-14s stopping\n", n.Name)
		}
	}
}

type NodeStatus struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Agent   string `json:"agent,omitempty"`
	Running bool   `json:"running"`
	Height  int    `json:"height"`
	Builder bool   `json:"builder"`
}

func (e *Engine) Status(ctx context.Context) []NodeStatus {
	var out []NodeStatus
	for _, n := range e.nodes() {
		s := NodeStatus{Name: n.Name, Label: n.Label, Builder: n.Builder}
		c := e.rpc(n.Port)
		var info struct {
			Subversion string `json:"subversion"`
		}
		if c.Call(ctx, &info, "getnetworkinfo") == nil {
			s.Running, s.Agent = true, info.Subversion
			_ = c.Call(ctx, &s.Height, "getblockcount")
		}
		out = append(out, s)
	}
	return out
}

// Ready returns an error naming the first node that isn't running.
func (e *Engine) Ready(ctx context.Context) error {
	for _, s := range e.Status(ctx) {
		if !s.Running {
			return fmt.Errorf("%s isn't running; run: sbx up", s.Label)
		}
	}
	return nil
}
