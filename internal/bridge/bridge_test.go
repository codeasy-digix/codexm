package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/application"
	"github.com/codeasy-digix/codexm/internal/config"
)

func bridgeConfig(t *testing.T) config.Config {
	t.Helper()
	c := config.Defaults()
	dir := t.TempDir()
	c.Path = filepath.Join(dir, "config.toml")
	c.Storage.StateDir = filepath.Join(dir, "state")
	c.Runtime.AccountsDir = filepath.Join(dir, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(dir, "native")
	c.Runtime.EnginesDir = filepath.Join(dir, "engines")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestProtocolNegotiationAndEOF(t *testing.T) {
	c := bridgeConfig(t)
	app := application.New(context.Background(), c, "")
	defer app.Close()
	var output bytes.Buffer
	input := `{"protocolVersion":99,"id":"old","method":"capabilities","params":{}}
{"protocolVersion":1,"id":"current","method":"capabilities","params":{}}
{"protocolVersion":1,"id":"watch","method":"watch","params":{"instance":"missing","after":0}}
`
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), app, strings.NewReader(input), &output) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EOF left watcher running")
	}
	replies := map[string]map[string]any{}
	d := json.NewDecoder(&output)
	for {
		var reply map[string]any
		if err := d.Decode(&reply); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if id, ok := reply["id"].(string); ok {
			replies[id] = reply
		}
	}
	if replies["old"]["error"].(map[string]any)["code"] != "version_mismatch" || replies["current"]["result"].(map[string]any)["protocolVersion"] != float64(1) {
		t.Fatal("bad negotiation")
	}
}
func TestOnlyOneBridgeAndCancellationReleasesLease(t *testing.T) {
	c := bridgeConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, w := io.Pipe()
	defer w.Close()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c, "", in, io.Discard) }()
	// A successful capability response proves the lease is held before collision.
	for range 100 {
		if _, err := io.WriteString(w, "{\"protocolVersion\":1,\"id\":\"ping\",\"method\":\"capabilities\",\"params\":{}}\n"); err == nil {
			break
		}
	}
	if err := Run(context.Background(), c, "", strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "another web bridge") {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not close")
	}
	if err := Run(context.Background(), c, "", strings.NewReader(""), io.Discard); err != nil {
		t.Fatal("lease not released", err)
	}
}
