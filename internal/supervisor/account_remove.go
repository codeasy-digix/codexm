package supervisor

import (
	"errors"
	"github.com/codeasy-digix/codexm/internal/accounts"
)

func (s *Server) removeAccount(p accounts.RemoveArgs) (accounts.Removal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managerAccount == p.Account {
		return accounts.Removal{}, errors.New("관리자 계정입니다. 관리자 변경이 완료된 뒤 삭제하세요.")
	}
	current, err := s.Store.Jobs()
	if err != nil {
		return accounts.Removal{}, err
	}
	return accounts.RemoveRegistration(s.workerConfig(), p, current)
}
