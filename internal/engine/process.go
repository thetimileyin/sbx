package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"syscall"
	"time"

	"github.com/thetimileyin/sbx/internal/rpc"
)

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// start launches bitcoind in its own session so persistent nodes outlive the CLI.
func start(bin string, args []string) (*process, error) {
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &process{cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	return p, nil
}

func waitReady(ctx context.Context, c *rpc.Client, p *process, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.Call(ctx, nil, "getnetworkinfo") == nil {
			return nil
		}
		select {
		case <-p.done:
			return fmt.Errorf("exited (%v)", p.cmd.ProcessState)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("timed out")
}

func (p *process) stop(c *rpc.Client) {
	_ = c.Call(context.Background(), nil, "stop")
	select {
	case <-p.done:
	case <-time.After(30 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
