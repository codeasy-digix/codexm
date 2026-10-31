//go:build legacy

package cli

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/service"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func serviceCommand(ctx context.Context, c config.Config, self string, args []string) error {
	m := service.Manager{Config: c, Self: self}
	if len(args) == 1 && args[0] == "upgrade" {
		return m.Upgrade(ctx, func(ctx context.Context) error { return supervisor.Ping(ctx, c) })
	}
	if len(args) > 0 && (args[0] == "stop" || args[0] == "uninstall") {
		if !m.Registered() {
			return errors.New("codexm service is not installed; no process was stopped")
		}
		if e := m.Disarm(ctx); e != nil {
			return e
		}
	}
	if len(args) > 0 && (args[0] == "stop" || args[0] == "uninstall") && supervisor.Ping(ctx, c) == nil {
		// Stop work explicitly before disabling OS recovery. A restart deliberately
		// keeps worker engines alive and only replaces the observer service.
		if err := supervisor.Request(ctx, c, "shutdown", nil, nil); err != nil {
			return err
		}
		deadline := time.Now().Add(20 * time.Second)
		for supervisor.Ping(ctx, c) == nil && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		if supervisor.Ping(ctx, c) == nil {
			return errors.New("manager is still stopping; inspect service status")
		}
	}
	if e := m.Command(ctx, args, os.Stdout); e != nil {
		return e
	}
	if len(args) > 0 && (args[0] == "start" || args[0] == "restart" || args[0] == "install" && len(args) == 1) {
		return service.WaitReady(ctx, func(ctx context.Context) error { return supervisor.Ping(ctx, c) })
	}
	return nil
}
