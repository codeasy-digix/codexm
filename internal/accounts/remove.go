package accounts

import (
	"errors"
	"slices"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type RemoveArgs struct {
	Account  string `json:"account"`
	Revision string `json:"revision"`
}

type Removal struct {
	Account  string `json:"account"`
	Revision string `json:"revision"`
}

// RemoveRegistration changes only the registry and selection policy. The caller
// must hold the supervisor dispatch gate or its offline lease while reading jobs
// and calling this function. Account homes, auth, histories and past runs remain.
func RemoveRegistration(c config.Config, p RemoveArgs, current []jobs.Job) (Removal, error) {
	if !config.Nickname.MatchString(p.Account) || p.Account == "default" || p.Revision == "" {
		return Removal{}, errors.New("invalid_input: select a registered account and reload its settings revision")
	}
	names, err := List(c)
	if err != nil {
		return Removal{}, err
	}
	if !slices.Contains(names, p.Account) {
		return Removal{}, errors.New("등록된 계정을 선택하세요. 계정 목록을 새로고침하세요.")
	}
	lock, err := cr.AccountLoginLock(c, p.Account)
	if err != nil {
		return Removal{}, errors.New("로그인 또는 계정 변경이 진행 중입니다. 완료 후 다시 시도하세요.")
	}
	defer lock.Close()
	_, rev, err := config.Update(c.Path, p.Revision, func(c *config.Config) error {
		names, err := List(*c)
		if err != nil {
			return err
		}
		if !slices.Contains(names, p.Account) {
			return errors.New("등록된 계정을 선택하세요. 계정 목록을 새로고침하세요.")
		}
		if c.Manager.Account == p.Account {
			return errors.New("관리자 계정입니다. 설정에서 다른 관리자를 선택한 뒤 삭제하세요.")
		}
		for _, j := range current {
			if j.TargetAccount == p.Account || j.Account == p.Account && (j.Session != "" || j.Managed && !jobs.Terminal(j.State)) {
				return errors.New("작업에서 사용하는 계정입니다. 해당 작업의 계정을 변경하거나 관리 등록을 해제한 뒤 삭제하세요.")
			}
		}
		c.WorkerAccounts.Priority = slices.DeleteFunc(c.WorkerAccounts.Priority, func(n string) bool { return n == p.Account })
		if c.WorkerAccounts.FallbackAccount == p.Account {
			c.WorkerAccounts.FallbackAccount = ""
		}
		c.AccountRegistry.Removed = append(c.AccountRegistry.Removed, p.Account)
		slices.Sort(c.AccountRegistry.Removed)
		return nil
	})
	return Removal{Account: p.Account, Revision: rev}, err
}
