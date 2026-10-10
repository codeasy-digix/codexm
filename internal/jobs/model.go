package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

func ID() string {
	var b [12]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}

type Job struct {
	ID                string          `json:"id"`
	ParentID          string          `json:"parent_id,omitempty"`
	Title             string          `json:"title"`
	Kind              string          `json:"kind"`
	Cwd               string          `json:"cwd"`
	Host              string          `json:"host"`
	Store             string          `json:"store"`
	ThreadID          string          `json:"thread_id,omitempty"`
	Account           string          `json:"account"`
	InitialAccount    string          `json:"initial_account"`
	RunID             string          `json:"run_id,omitempty"`
	Socket            string          `json:"socket,omitempty"`
	Session           string          `json:"session,omitempty"`
	EngineBinary      string          `json:"engine_binary,omitempty"`
	EngineVersion     string          `json:"engine_version,omitempty"`
	ContinuationDue   bool            `json:"continuation_due,omitempty"`
	ResumeLimitedGoal bool            `json:"resume_limited_goal,omitempty"`
	LastTurnID        string          `json:"last_turn_id,omitempty"`
	TokenUsage        json.RawMessage `json:"token_usage,omitempty"`
	State             string          `json:"state"`
	Managed           bool            `json:"managed"`
	Paused            bool            `json:"paused"`
	ReadOnly          bool            `json:"read_only"`
	TurnID            string          `json:"turn_id,omitempty"`
	Goal              *Goal           `json:"goal,omitempty"`
	Model             string          `json:"model,omitempty"`
	Effort            string          `json:"effort,omitempty"`
	Approval          string          `json:"approval,omitempty"`
	Sandbox           string          `json:"sandbox,omitempty"`
	LastInput         string          `json:"last_input,omitempty"`
	LastOutput        string          `json:"last_output,omitempty"`
	LastError         string          `json:"last_error,omitempty"`
	CompletionCommand []string        `json:"completion_command,omitempty"`
	PendingRequests   []Request       `json:"pending_requests,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	LastObserved      time.Time       `json:"last_observed"`
	LastAccountCheck  time.Time       `json:"last_account_check"`
	RetryCount        int             `json:"retry_count"`
}
type Goal struct {
	Objective       string `json:"objective"`
	Status          string `json:"status"`
	TokenBudget     *int64 `json:"tokenBudget,omitempty"`
	TokensUsed      int64  `json:"tokensUsed"`
	TimeUsedSeconds int64  `json:"timeUsedSeconds"`
}
type Request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	RunID  string          `json:"run_id"`
}
type Action struct {
	ID        string          `json:"id"`
	JobID     string          `json:"job_id"`
	RunID     string          `json:"run_id,omitempty"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	State     string          `json:"state"`
	Attempts  int             `json:"attempts"`
	NotBefore time.Time       `json:"not_before"`
	CreatedAt time.Time       `json:"created_at"`
	Error     string          `json:"error,omitempty"`
}
type Event struct {
	ID      int64           `json:"id"`
	Key     string          `json:"key"`
	JobID   string          `json:"job_id"`
	RunID   string          `json:"run_id"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
	At      time.Time       `json:"at"`
}
type Run struct {
	ID, JobID, Account, Socket, Session string
	EngineBinary, EngineVersion         string
	ThreadID                            string
	StartedAt                           time.Time
	EndedAt                             *time.Time
	State                               string
	UsageStart, UsageLatest             *AccountUsage
}

// AccountUsage observes the account's quota, shared with every other session.
// It is not an attribution of account consumption to this run.
type AccountUsage struct {
	At        time.Time    `json:"at"`
	LimitID   string       `json:"limit_id,omitempty"`
	Primary   *UsageWindow `json:"primary,omitempty"`
	Secondary *UsageWindow `json:"secondary,omitempty"`
	Error     string       `json:"error,omitempty"`
}
type UsageWindow struct {
	UsedPercent int    `json:"used_percent"`
	Minutes     *int   `json:"minutes,omitempty"`
	ResetsAt    *int64 `json:"resets_at,omitempty"`
}
type WorkChange struct {
	ID       string    `json:"id"`
	JobID    string    `json:"job_id"`
	ThreadID string    `json:"thread_id"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
}
type WorkLog struct {
	At      time.Time    `json:"at"`
	Note    string       `json:"note"`
	Runs    []Run        `json:"runs"`
	Changes []WorkChange `json:"changes"`
}
type Input struct {
	Text      string `json:"text"`
	Steer     bool   `json:"steer"`
	MessageID string `json:"message_id"`
}

func ValidKind(k string) bool { return k == "chat" || k == "task" || k == "goal" || k == "group" }
func Busy(s string) bool {
	switch s {
	case "running", "starting", "switching", "waiting_on_user", "waiting_on_approval":
		return true
	}
	return false
}
func Terminal(s string) bool { return s == "complete" || s == "stopped" || s == "archived" }
