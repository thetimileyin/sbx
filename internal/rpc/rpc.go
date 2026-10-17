// Package rpc is a minimal JSON-RPC client for bitcoind.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Error is an error returned by bitcoind itself (as opposed to a transport failure).
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

type Client struct {
	url, user, pass string
	http            *http.Client
}

func New(port int, user, pass string) *Client {
	return &Client{
		url:  fmt.Sprintf("http://127.0.0.1:%d/", port),
		user: user,
		pass: pass,
		http: &http.Client{Timeout: 2 * time.Minute},
	}
}

// Wallet returns a client whose calls go to the named wallet.
func (c *Client) Wallet(name string) *Client {
	cp := *c
	cp.url = c.url + "wallet/" + name
	return &cp
}

// Call runs method with params and decodes the result into out (which may be nil).
func (c *Client) Call(ctx context.Context, out any, method string, params ...any) error {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "1.0", "id": "sbx", "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	// bitcoind reports RPC errors with a non-200 status and a JSON body, so parse regardless.
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%s: HTTP %d %s", method, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if env.Error != nil {
		return env.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

// Message returns bitcoind's message for an RPC error, or the error text otherwise.
func Message(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Message
	}
	return err.Error()
}
