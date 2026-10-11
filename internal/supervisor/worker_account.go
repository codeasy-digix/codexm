package supervisor

import "github.com/codeasy-digix/codexm/internal/config"

func (s *Server) defaultWorker() string {
	return s.workerConfig().DefaultWorker()
}

// Runtime configuration is immutable for concurrent workers. Read the saved
// account policy separately so an account selection takes effect immediately.
func (s *Server) workerConfig() config.Config {
	c := s.Config
	if latest, e := config.Load(s.Config.Path); e == nil {
		c.WorkerAccounts = latest.WorkerAccounts
	}
	return c
}
