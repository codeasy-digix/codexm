package application

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/store"
	"github.com/codeasy-digix/codexm/internal/supervisor"
	"golang.org/x/sys/unix"
)

var removalOperationID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// RemoveAccount shares online/offline policy across web, CLI and local panels.
// A stopped service's lease prevents it from starting between the job check and
// registry write. Online requests use the supervisor's durable operation IDs.
func RemoveAccount(ctx context.Context, c config.Config, p accounts.RemoveArgs, operation string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !removalOperationID.MatchString(operation) {
		return nil, errors.New("invalid_input: operation ID required")
	}
	var ping struct {
		Accounts int `json:"accounts_api"`
	}
	if supervisor.Request(ctx, c, "ping", nil, &ping) == nil {
		if ping.Accounts != 1 {
			return nil, errors.New("version_mismatch: 계정 삭제를 사용하려면 관리 서비스를 업그레이드하세요.")
		}
		raw, _ := json.Marshal(p)
		var result json.RawMessage
		err := supervisor.Request(ctx, c, "web-operation", supervisor.WebOperation{ID: operation, Command: "account-remove", Args: raw}, &result)
		return result, err
	}
	if err := os.MkdirAll(c.StateDir(), 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(c.StateDir(), "supervisor.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.New("관리 서비스가 응답하지 않습니다. 연결을 복구한 뒤 삭제하세요.")
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var current []jobs.Job
	if _, err = os.Stat(filepath.Join(c.StateDir(), "state.sqlite")); err == nil {
		st, err := store.ReadOnly(c.StateDir())
		if err != nil {
			return nil, err
		}
		current, err = st.Jobs()
		st.Close()
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return accounts.RemoveRegistration(c, p, current)
}
