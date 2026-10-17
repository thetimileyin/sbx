// Command sbx checks whether Bitcoin transactions would be accepted by several
// node versions at once, on a private regtest network.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/thetimileyin/sbx/internal/engine"
	"github.com/thetimileyin/sbx/internal/web"
)

const usage = `sbx — will it relay?

Checks a transaction against several Bitcoin Core and Knots versions at once, on a
private regtest network. Nothing is broadcast.

Usage:
  sbx fetch [--skip-signatures]      download the node releases; each SHA256SUMS must be
                                     signed by at least 3 known builders (needs gpgv)
  sbx up                             start the nodes and fund the sandbox wallet
  sbx status                         show which nodes are running
  sbx check [flags] [HEX ...]        check a tx, or a package (parents first, child last)
      --file FILE                    read transactions from FILE ("-" for stdin), one per line
      --mempool FILE                 load these transactions into the mempool first
      --json                         print the result as JSON
  sbx demo [basic|package|mempool]   run the built-in examples (all groups by default)
  sbx serve [--addr 127.0.0.1:8338]  local web page and JSON API
  sbx down                           stop the nodes
  sbx version                        print the sbx version

Every command takes --home DIR (default $SBX_HOME or ~/.sbx).

Exit codes for check: 0 every version accepts, 3 at least one rejects, 1 error, 2 usage.
`

const exitRejected = 3

// version is set at release time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("sbx", version)
		return
	}
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		fmt.Print(usage)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code, err := run(ctx, os.Args[1], os.Args[2:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "sbx:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func run(ctx context.Context, cmd string, args []string) (int, error) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	home := fs.String("home", engine.DefaultHome(), "data directory")
	var (
		asJSON  = fs.Bool("json", false, "print JSON")
		file    = fs.String("file", "", "transactions file, one hex per line (- for stdin)")
		mempool = fs.String("mempool", "", "mempool setup file, one hex per line")
		addr    = fs.String("addr", "127.0.0.1:8338", "listen address (loopback only)")
		noSigs  = fs.Bool("skip-signatures", false, "fetch: don't verify builder signatures (not recommended)")
	)
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	e, err := engine.Open(*home)
	if err != nil {
		return 1, err
	}

	switch cmd {
	case "fetch":
		return 0, e.Fetch(os.Stdout, *noSigs)
	case "up":
		return 0, e.Up(ctx, os.Stdout)
	case "down":
		e.Down(ctx, os.Stdout)
		return 0, nil
	case "status":
		for _, s := range e.Status(ctx) {
			state := "down"
			if s.Running {
				state = fmt.Sprintf("up   height %d  %s", s.Height, s.Agent)
			}
			fmt.Printf("  %-22s %s\n", s.Label, state)
		}
		return 0, nil
	case "check":
		req, err := readRequest(fs.Args(), *file, *mempool)
		if err != nil {
			return 2, err
		}
		res, err := e.Check(ctx, req)
		if err != nil {
			return 1, err
		}
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(res)
		} else {
			printResult(os.Stdout, "", res)
		}
		if !res.AllAccepted() {
			return exitRejected, nil
		}
		return 0, nil
	case "demo":
		return demo(ctx, e, fs.Args(), *asJSON)
	case "serve":
		if _, err := web.CheckAddr(*addr); err != nil {
			return 2, err
		}
		if err := e.Up(ctx, io.Discard); err != nil {
			return 1, err
		}
		fmt.Printf("sbx is serving http://%s (Ctrl-C to stop; the nodes keep running until `sbx down`)\n", *addr)
		return 0, web.Serve(ctx, e, *addr)
	}
	fmt.Print(usage)
	return 2, fmt.Errorf("unknown command %q", cmd)
}

func readLines(path string) ([]string, error) {
	var r io.Reader = os.Stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 8<<20) // transactions can be long lines
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

func readRequest(args []string, file, mempool string) (engine.Request, error) {
	req := engine.Request{Txs: args}
	if file != "" {
		txs, err := readLines(file)
		if err != nil {
			return req, err
		}
		req.Txs = append(req.Txs, txs...)
	}
	if len(req.Txs) == 0 {
		return req, errors.New("give transaction hex as arguments or with --file (see sbx --help)")
	}
	if mempool != "" {
		m, err := readLines(mempool)
		if err != nil {
			return req, err
		}
		req.Mempool = m
	}
	return req, nil
}

func demo(ctx context.Context, e *engine.Engine, groups []string, asJSON bool) (int, error) {
	want := map[string]bool{}
	for _, g := range groups {
		want[g] = true
	}
	type entry struct {
		ID     string         `json:"id"`
		Title  string         `json:"title"`
		Result *engine.Result `json:"result"`
	}
	var all []entry
	for _, ex := range engine.Examples() {
		if len(want) > 0 && !want[ex.Group] {
			continue
		}
		req, err := e.BuildExample(ctx, ex.ID)
		if err != nil {
			return 1, fmt.Errorf("%s: %w", ex.ID, err)
		}
		res, err := e.Check(ctx, req)
		if err != nil {
			return 1, fmt.Errorf("%s: %w", ex.ID, err)
		}
		if asJSON {
			all = append(all, entry{ex.ID, ex.Title, res})
		} else {
			printResult(os.Stdout, ex.Title, res)
		}
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return 0, enc.Encode(all)
	}
	return 0, nil
}
