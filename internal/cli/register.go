package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func registerCommand(ctx context.Context, c config.Config, self string, args []string) error {
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	f := flag.NewFlagSet("register", flag.ContinueOnError)
	current := f.Bool("current", false, "register the current Codex chat")
	account := f.String("account", "", "worker account after handoff")
	jsonOutput := f.Bool("json", false, "safe registration metadata")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("usage: codexm register [THREAD_ID] [--current] [--account NAME] [--json]")
	}
	if *current {
		if id != "" {
			return errors.New("choose a thread ID or --current")
		}
		id = os.Getenv("CODEX_THREAD_ID")
	}
	if !config.Nickname.MatchString(id) {
		return errors.New("current local Codex thread is unavailable; pass its actual THREAD_ID or invoke from that chat")
	}
	if c.Manager.Account == "" {
		return errors.New("select and save a manager account in codexm setup first")
	}
	origin := ""
	if *current {
		origin = accounts.CurrentNickname(c, os.Getenv("CODEX_HOME"), os.Getenv("CODEX_ACCOUNT"))
	}
	name := *account
	if name == "" {
		name = origin
	}
	if name == "" {
		name = c.DefaultWorker()
	}
	if e := supervisor.Start(ctx, c, self); e != nil {
		return e
	}
	var j jobs.Job
	if e := supervisor.Request(ctx, c, "register", supervisor.JobArgs{ThreadID: id, Account: name, OriginAccount: origin}, &j); e != nil {
		return e
	}
	result := map[string]any{"job_id": j.ID, "thread_id": j.ThreadID, "account": j.Account, "state": j.State, "registered": j.Managed, "priority": c.WorkerAccounts.Priority, "registration": j.Registration}
	if j.Goal != nil {
		result["goal_status"] = j.Goal.Status
	}
	if *jsonOutput {
		return output(result)
	}
	fmt.Printf("Registered %s as %s · account %s · %s\n", j.ThreadID, j.ID, j.Account, j.State)
	if j.State == "external_writer" {
		fmt.Println("Original Codex still owns this chat. Monitoring is active; account handoff waits for its writer to release.")
	}
	return nil
}
