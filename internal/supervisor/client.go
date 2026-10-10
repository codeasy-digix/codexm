package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/codeasy-digix/codexm/internal/config"
	"net"
	"net/http"
	"time"
)

func Request(ctx context.Context, c config.Config, command string, args, result any) error {
	b, e := json.Marshal(args)
	if e != nil {
		return e
	}
	payload, e := json.Marshal(Call{Command: command, Args: b})
	if e != nil {
		return e
	}
	tr := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", Socket(c))
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	req, e := http.NewRequestWithContext(ctx, "POST", "http://codexm/rpc", bytes.NewReader(payload))
	if e != nil {
		return e
	}
	r, e := client.Do(req)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return errors.New("supervisor rejected request")
	}
	var reply Response
	if e = json.NewDecoder(r.Body).Decode(&reply); e != nil {
		return e
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	if result != nil {
		return json.Unmarshal(reply.Result, result)
	}
	return nil
}
func Ping(ctx context.Context, c config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return Request(ctx, c, "ping", nil, nil)
}
