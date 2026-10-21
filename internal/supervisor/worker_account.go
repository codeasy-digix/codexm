package supervisor

import "github.com/codeasy-digix/codexm/internal/config"

// Runtime configuration is immutable for concurrent workers. Read the saved
// account policy separately so panel priority changes take effect immediately.
func (s *Server) workerConfig() config.Config {
	c := s.Config
	if latest, e := config.Load(s.Config.Path); e == nil {
		c.WorkerAccounts = latest.WorkerAccounts
		c.AccountRegistry = latest.AccountRegistry
		c.Execution.MaxParallelJobs = latest.Execution.MaxParallelJobs
		c.Logging.UsageSampleSeconds = latest.Logging.UsageSampleSeconds
		c.Supervisor.ReconcileIntervalSeconds = latest.Supervisor.ReconcileIntervalSeconds
	}
	return c
}
