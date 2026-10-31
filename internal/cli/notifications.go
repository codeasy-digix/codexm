//go:build legacy

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

func notifyCommand(ctx context.Context, c config.Config, args []string) error {
	if len(args) == 0 || args[0] == "status" {
		fmt.Printf("5h Discord alerts: enabled=%t accounts=%v\nEmpty accounts means all saved Codex accounts. Alerts include all accounts and current managed task status.\n", c.Notifications.FiveHourDiscord, c.Notifications.Accounts)
		return nil
	}
	if args[0] != "five-hour" && args[0] != "off" {
		return errors.New("usage: codexm notify five-hour [ACCOUNT ...] | off | status")
	}
	if args[0] == "five-hour" {
		names, e := accounts.List(c)
		if e != nil {
			return e
		}
		for _, name := range args[1:] {
			if !slices.Contains(names, name) {
				return errors.New("choose saved account nicknames")
			}
		}
		home, _ := os.UserHomeDir()
		if e = exec.CommandContext(ctx, filepath.Join(home, ".local", "bin", "agent_job_done"), "--check").Run(); e != nil {
			return errors.New("configure the .home agent_job_done Discord helper first; no message sent")
		}
	}
	_, _, e := config.Update(c.Path, "", func(next *config.Config) error {
		next.Notifications.FiveHourDiscord = args[0] == "five-hour"
		next.Notifications.Accounts = slices.Clone(args[1:])
		return nil
	})
	if e == nil {
		fmt.Println("Discord quota notification preference saved; a running codexm service applies it within one minute. No test message sent.")
	}
	return e
}
