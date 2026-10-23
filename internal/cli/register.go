package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

var hookSessionID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// registerPrompt is the deterministic current-chat registration used by the
// prompt hook. Its caller supplies the official hook session_id and deadline.
// Never fall back to an environment variable, a selected row or the newest chat.
// The hook only talks to an already running supervisor: service installation or
// recovery belongs to the ordinary CLI and the OS service manager.
func registerPrompt(ctx context.Context, configPath string, sessionID string) (string, error) {
	if !hookSessionID.MatchString(sessionID) {
		return registrationFailure("현재 대화 ID를 확인할 수 없어 등록하지 않았습니다. 로컬 Codex 대화에서 다시 실행하세요")
	}
	c, err := config.Load(configPath)
	if err != nil {
		return registrationFailure("codexm 설정을 읽을 수 없어 등록하지 않았습니다. codexm 설정을 확인하세요")
	}
	if c.Manager.Account == "" {
		return registrationFailure("codexm 초기 설정이 필요합니다. codexm setup에서 관리 계정을 저장한 뒤 다시 실행하세요")
	}
	a, err := registrationArgs(c, sessionID, "", true)
	if err != nil {
		return registrationFailure("현재 대화의 관리 등록 설정을 확인할 수 없습니다. codexm 계정 설정을 확인하세요")
	}
	if err := supervisor.Ping(ctx, c); err != nil {
		return registrationFailure("codexm 관리 서비스에 연결하지 못했습니다. codexm service status를 확인하고 서비스를 시작한 뒤 다시 실행하세요")
	}
	j, err := registerThread(ctx, c, a)
	if err != nil {
		// A cancelled request may have reached the supervisor. Do not claim that
		// registration failed or echo an RPC error containing conversation data.
		return registrationFailure("codexm 등록 결과를 확인하지 못했습니다. 관리 패널에서 이 대화의 등록 상태를 확인한 뒤 다시 실행하세요")
	}
	if !j.Managed || j.ThreadID != sessionID || (j.Account != "" && !config.Nickname.MatchString(j.Account)) || (j.Account == "" && !j.AutoAccount) {
		return registrationFailure("codexm이 현재 대화의 관리 등록을 확인하지 못했습니다. 관리 패널에서 등록 상태를 확인하세요")
	}
	message := fmt.Sprintf("현재 대화가 codexm 관리 대상으로 등록되어 있습니다. 인계 후 작업 계정: %s.", j.Account)
	if j.AutoAccount {
		message = "현재 대화가 codexm 관리 대상으로 등록되어 있습니다. 인계 후 작업 계정은 우선순위에서 가용 계정을 자동 선택합니다."
	}
	if j.State == "external_writer" {
		message += " 감시 중(external_writer): 기존 Codex가 대화의 실행권을 해제한 뒤에만 계정 인계와 재개가 가능합니다."
	}
	return message, nil
}

// This display text is authored locally; never pass a raw configuration or RPC
// error to the hook, as it may contain authentication or conversation details.
func registrationFailure(message string) (string, error) {
	return message, errors.New(message)
}

func registrationArgs(c config.Config, id, requestedAccount string, current bool) (supervisor.JobArgs, error) {
	if !config.Nickname.MatchString(id) {
		return supervisor.JobArgs{}, errors.New("current local Codex thread is unavailable; pass its actual THREAD_ID or invoke from that chat")
	}
	if c.Manager.Account == "" {
		return supervisor.JobArgs{}, errors.New("select and save a manager account in codexm setup first")
	}
	origin := ""
	if current {
		origin = accounts.CurrentNickname(c, os.Getenv("CODEX_HOME"), os.Getenv("CODEX_ACCOUNT"))
	}
	name := requestedAccount
	return supervisor.JobArgs{ThreadID: id, Account: name, OriginAccount: origin}, nil
}

func registerThread(ctx context.Context, c config.Config, a supervisor.JobArgs) (jobs.Job, error) {
	var j jobs.Job
	err := supervisor.Request(ctx, c, "register", a, &j)
	return j, err
}

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
	a, e := registrationArgs(c, id, *account, *current)
	if e != nil {
		return e
	}
	if e := supervisor.Start(ctx, c, self); e != nil {
		return e
	}
	j, e := registerThread(ctx, c, a)
	if e != nil {
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
