package supervisor

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

func (s *Server) removeAccount(p accounts.RemoveArgs) (accounts.Removal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managerAccount == p.Account {
		return accounts.Removal{}, errors.New("관리자 계정입니다. 관리자 변경이 완료된 뒤 삭제하세요.")
	}
	result, err := accounts.RemoveRegistration(s.workerConfig(), p)
	if err != nil {
		return accounts.Removal{}, err
	}
	result.DetachedJobs, result.PendingHandoffs, err = accounts.DetachRemovedAssignments(s.workerConfig(), s.Store, s.dispatching)
	return result, err
}

// The registration tombstone is already saved. Parking does not depend on a
// replacement login being available; no new turn can use the removed account.
func (s *Server) reassignRemovedAccount(ctx context.Context, id string) error {
	j, err := s.Store.Job(id)
	if err != nil || j.AccountReassignment == nil {
		return err
	}
	p := *j.AccountReassignment
	if j.Account == p.From || j.Account != "" && slices.Contains(s.workerConfig().AccountRegistry.Removed, j.Account) {
		p.From = j.Account
		p.Continue = p.Continue || mayContinue(j)
		p.HadRuntime = p.HadRuntime || j.Session != ""
		c := s.client(id)
		if c != nil {
			select {
			case <-c.Done:
				c = nil
			default:
			}
		}
		if c != nil {
			// A send may have been in flight when removal saved its intent.
			thread, e := c.Read(ctx, j.ThreadID, true)
			if e != nil {
				return later("계정 삭제 인계: 기존 실행 상태를 확인하는 중입니다.")
			}
			if thread.Status.Type == "active" && len(thread.Turns) > 0 {
				turn := thread.Turns[len(thread.Turns)-1]
				if turn.Status == "inProgress" {
					p.Continue = true
					if err = s.update(id, func(cur *jobs.Job) { cur.TurnID = turn.ID }); err != nil {
						return err
					}
				}
			}
		}
		if err = s.update(id, func(cur *jobs.Job) { cur.AccountReassignment = &p }); err != nil {
			return err
		}
		if err = s.park(ctx, id, false); err != nil {
			return later("계정 삭제 인계: 소유한 worker의 안전한 종료를 기다립니다.")
		}
		if err = s.changeAccount(id, "", "계정 등록 삭제: 기존 배정 해제"); err != nil {
			return err
		}
	}
	j, err = s.Store.Job(id)
	if err != nil {
		return err
	}
	if p.Continue && j.Managed && mayContinueAfterChange(j) {
		if err = s.update(id, func(cur *jobs.Job) {
			cur.AutoAccount, cur.ContinuationDue = true, true
			cur.ResumeLimitedGoal = cur.Goal != nil && cur.Goal.Status == "usageLimited"
			cur.State = "queued"
		}); err != nil {
			return err
		}
		if err = s.ensureRuntime(ctx, id, false); err != nil {
			var wait *postpone
			if errors.As(err, &wait) {
				return err
			}
			return later("계정 삭제 인계: 다음 가용 계정 연결을 기다립니다.")
		}
		j, err = s.Store.Job(id)
		if err != nil {
			return err
		}
		if j.Kind == "goal" && j.Goal != nil && j.Goal.Status == "active" {
			s.enqueueOnce(j, "goal_continue", nil)
		} else if j.Kind != "goal" {
			if err = s.queueContinuation(j); err != nil {
				return err
			}
		}
	}
	retry := false
	err = s.update(id, func(cur *jobs.Job) {
		if cur.Account != "" && slices.Contains(s.workerConfig().AccountRegistry.Removed, cur.Account) {
			// A second account may have been removed while its replacement was
			// connecting. Retain the same durable intent and retire that run too.
			p.From = cur.Account
			cur.AccountReassignment = &p
			retry = true
			return
		}
		if !p.Continue && !cur.Paused && !jobs.Terminal(cur.State) && (cur.State == "switching" || cur.State == "unknown" || cur.State == "starting") {
			cur.State = p.PriorState
		}
		cur.AccountReassignment = nil
		cur.AutoAccount = true
		cur.LastAccountCheck = time.Now()
		cur.LastError = ""
	})
	if err != nil {
		return err
	}
	if retry {
		return later("교체 계정도 등록 해제되어 남은 계정을 다시 선택합니다.")
	}
	return nil
}
