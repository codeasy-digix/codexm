package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/codeasy-digix/codexm/internal/config"
)

type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("Codex RPC %d: %s", e.Code, SafeText(e.Message))
}
func SafeText(s string) string {
	s = secretPattern.ReplaceAllString(s, "[redacted]")
	if len(s) > 1200 {
		s = s[:1200] + "…"
	}
	return s
}

var secretPattern = regexp.MustCompile(`(?i)(?:bearer\s+[^\s"']+|(?:sk-|sk-proj-)[A-Za-z0-9_-]{12,}|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+|(?:access_token|refresh_token|id_token|api_key|authorization)["']?\s*[:=]\s*["']?[^\s,"'}]+)`)

type Client struct {
	Events         chan Message
	Done           chan struct{}
	write          func([]byte) error
	closeTransport func() error
	pending        map[string]chan Message
	mu             sync.Mutex
	writeMu        sync.Mutex
	closeOnce      sync.Once
	next           atomic.Uint64
	err            error
}

func newClient() *Client {
	return &Client{Events: make(chan Message, 2048), Done: make(chan struct{}), pending: map[string]chan Message{}}
}
func (c *Client) receive(b []byte) {
	var m Message
	if e := json.Unmarshal(b, &m); e != nil {
		return
	}
	if m.Method != "" {
		select {
		case c.Events <- m:
		case <-c.Done:
		}
		return
	}
	if len(m.ID) == 0 {
		return
	}
	c.mu.Lock()
	ch := c.pending[string(m.ID)]
	c.mu.Unlock()
	if ch != nil {
		select {
		case ch <- m:
		default:
		}
	}
}
func (c *Client) finish(e error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.err = e
		c.mu.Unlock()
		close(c.Done)
		if c.closeTransport != nil {
			_ = c.closeTransport()
		}
	})
}
func (c *Client) Close() error { c.finish(nil); return nil }
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		return io.EOF
	}
	return c.err
}
func (c *Client) send(v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.Done:
		return c.Err()
	default:
	}
	return c.write(b)
}
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	id := c.next.Add(1)
	key := strconv.FormatUint(id, 10)
	ch := make(chan Message, 1)
	c.mu.Lock()
	c.pending[key] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	if e := c.send(map[string]any{"id": id, "method": method, "params": params}); e != nil {
		return e
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result != nil {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.Done:
		return c.Err()
	}
}
func (c *Client) Reply(id json.RawMessage, result any) error {
	return c.send(map[string]any{"id": id, "result": result})
}
func (c *Client) Reject(id json.RawMessage, reason string) error {
	return c.send(map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": reason}})
}
func (c *Client) Initialize(ctx context.Context) error {
	var r any
	e := c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "codexm", "title": "Codex Manager", "version": config.Version}, "capabilities": map[string]any{"experimentalApi": true}}, &r)
	if e != nil {
		return e
	}
	return c.send(map[string]any{"method": "initialized", "params": map[string]any{}})
}
func StartStdio(ctx context.Context, cmd *exec.Cmd) (*Client, error) {
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	c := newClient()
	c.write = func(b []byte) error { _, e := in.Write(append(b, '\n')); return e }
	c.closeTransport = func() error { _ = in.Close(); return nil }
	go func() {
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 65536), 32*1024*1024)
		for scanner.Scan() {
			c.receive(scanner.Bytes())
		}
		e := scanner.Err()
		if e == nil {
			e = io.EOF
		}
		c.finish(e)
	}()
	go func() { e := cmd.Wait(); c.finish(e) }()
	if e = c.Initialize(ctx); e != nil {
		c.Close()
		_ = cmd.Process.Kill()
		return nil, e
	}
	return c, nil
}
func DialUnix(ctx context.Context, socket string) (*Client, error) {
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	conn, resp, e := d.DialContext(ctx, "ws://localhost/", http.Header{})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if e != nil {
		return nil, e
	}
	conn.SetReadLimit(32 * 1024 * 1024)
	c := newClient()
	c.write = func(b []byte) error { return conn.WriteMessage(websocket.TextMessage, b) }
	c.closeTransport = conn.Close
	go func() {
		for {
			_, b, e := conn.ReadMessage()
			if e != nil {
				c.finish(e)
				return
			}
			c.receive(b)
		}
	}()
	if e = c.Initialize(ctx); e != nil {
		c.Close()
		return nil, e
	}
	return c, nil
}
func IsWriterCollision(e error) bool {
	return strings.Contains(strings.ToLower(fmt.Sprint(e)), "active writer") || strings.Contains(strings.ToLower(fmt.Sprint(e)), "already running")
}
func Timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 40*time.Second)
}

var ErrDisconnected = errors.New("Codex connection is unavailable")
