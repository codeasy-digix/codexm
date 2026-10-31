//go:build legacy

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/store"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

// Listing does not require a manager login or start a supervisor/app-server.
func sessionCommand(ctx context.Context, c config.Config, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: codexm session list [--search TEXT] [--managed] [--active-only] [--json]")
	}
	f := flag.NewFlagSet("session list", flag.ContinueOnError)
	search := f.String("search", "", "search title, conversation ID, account or directory")
	managed := f.Bool("managed", false, "only conversations registered with codexm")
	activeOnly := f.Bool("active-only", false, "exclude archived conversations")
	limit := f.Int("limit", 0, "maximum rows (0: all conversations)")
	jsonOutput := f.Bool("json", false, "JSON metadata")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if *limit < 0 || len(f.Args()) != 0 {
		return errors.New("invalid session list arguments")
	}
	list, err := catalog.Read(ctx, config.Expand(c.Runtime.SharedCodexHome))
	if err != nil {
		return err
	}
	st, err := store.ReadOnly(c.StateDir())
	if err == nil {
		js, e := st.Jobs()
		st.Close()
		if e != nil {
			return e
		}
		list = catalog.Merge(list, js)
	} else if !os.IsNotExist(err) {
		return err
	}
	filtered := []catalog.Entry{}
	for _, e := range list.Sessions {
		if *managed && e.JobID == "" || *activeOnly && e.Archived {
			continue
		}
		if *search != "" && !strings.Contains(strings.ToLower(e.Title+" "+e.ID+" "+e.JobID+" "+e.Account+" "+e.Cwd), strings.ToLower(*search)) {
			continue
		}
		filtered = append(filtered, e)
	}
	total := len(filtered)
	if *limit > 0 && len(filtered) > *limit {
		filtered = filtered[:*limit]
	}
	list.Sessions = filtered
	if *jsonOutput {
		return output(list)
	}
	if list.Warning != "" {
		fmt.Fprintln(os.Stderr, "Codex conversation index:", list.Warning)
	}
	fmt.Printf("Codex conversations: %d (showing %d) · read-only index\n", total, len(filtered))
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CONVERSATION ID\tUPDATED\tSTATE\tACCOUNT\tTITLE")
	for _, e := range filtered {
		account := e.Account
		if account == "" {
			account = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.ID, time.UnixMilli(e.UpdatedAt).Local().Format("01-02 15:04"), e.State, account, ansi.Truncate(oneLine(e.Title), 60, "…"))
	}
	return w.Flush()
}

func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func existingJob(ctx context.Context, c config.Config, id string) (jobs.Job, error) {
	var status supervisor.Status
	if err := supervisor.Request(ctx, c, "status", nil, &status); err != nil {
		return jobs.Job{}, err
	}
	for _, j := range status.Jobs {
		if j.ID == id || j.ThreadID == id {
			return j, nil
		}
	}
	var j jobs.Job
	err := supervisor.Request(ctx, c, "resume", supervisor.JobArgs{ThreadID: id}, &j)
	return j, err
}
