package nativecli

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type webActivity struct {
	State  string `json:"state"`
	Source string `json:"source"`
	TurnID string `json:"turnId,omitempty"`
}

type webLiveItem struct {
	ID        string `json:"id"`
	Phase     string `json:"phase,omitempty"`
	Text      string `json:"text"`
	Streaming bool   `json:"streaming"`
}

type webLiveTurn struct {
	ID     string        `json:"id"`
	Status string        `json:"status"`
	Items  []webLiveItem `json:"items"`
}

type webActivityReply struct {
	WritersKnown bool                   `json:"writersKnown"`
	Threads      map[string]webActivity `json:"threads"`
	Turn         *webLiveTurn           `json:"turn,omitempty"`
	AccountTurn  bool                   `json:"accountTurn,omitempty"`
}

type webLiveEntry struct {
	activity webActivity
	turn     webLiveTurn
	touched  time.Time
}

const webLiveRetention = 15 * time.Second

// This is a bounded preview of assistant messages from the web's own official
// connection. Tool data, reasoning, account events and raw RPCs stay there.
func (c *webConnection) observeLiveEvent(event cr.Message) {
	switch event.Method {
	case "thread/status/changed", "thread/closed", "turn/started", "turn/completed",
		"item/started", "item/completed", "item/agentMessage/delta":
	default:
		return
	}
	var p struct {
		ThreadID string
		TurnID   string
		ItemID   string
		Delta    string
		Status   struct {
			Type        string
			ActiveFlags []string
		}
		Turn struct {
			ID     string
			Status string
		}
		Item struct {
			ID         string
			Type       string
			Text       string
			Phase      string
			Moderation *struct{ Moderated bool }
		}
	}
	if json.Unmarshal(event.Params, &p) != nil || !threadID.MatchString(p.ThreadID) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if event.Method == "thread/closed" || p.Status.Type == "notLoaded" {
		delete(c.live, p.ThreadID)
		return
	}
	entry := c.live[p.ThreadID]
	now := time.Now()
	if entry == nil {
		if event.Method != "turn/started" && p.Status.Type != "active" {
			return
		}
		for id, old := range c.live {
			if old.turn.Status != "inProgress" && now.Sub(old.touched) > webLiveRetention {
				delete(c.live, id)
			}
		}
		if len(c.live) >= 64 {
			return
		}
		if c.live == nil {
			c.live = make(map[string]*webLiveEntry)
		}
		entry = &webLiveEntry{activity: webActivity{State: "running", Source: "web"}}
		c.live[p.ThreadID] = entry
	}
	switch event.Method {
	case "thread/status/changed":
		switch p.Status.Type {
		case "active":
			entry.activity.State = "running"
			if slices.Contains(p.Status.ActiveFlags, "waitingOnApproval") || slices.Contains(p.Status.ActiveFlags, "waitingOnUserInput") {
				entry.activity.State = "waiting"
			}
		case "idle":
			entry.activity.State = "idle"
		case "systemError":
			entry.activity.State = "unknown"
		}
	case "turn/started":
		if p.Turn.ID == "" {
			return
		}
		entry.turn = webLiveTurn{ID: p.Turn.ID, Status: "inProgress", Items: []webLiveItem{}}
		entry.activity.State, entry.activity.TurnID = "running", p.Turn.ID
	case "turn/completed":
		if entry.turn.ID != p.Turn.ID || p.Turn.ID == "" {
			return
		}
		entry.turn.Status = p.Turn.Status
		entry.activity.State = "idle"
		for i := range entry.turn.Items {
			entry.turn.Items[i].Streaming = false
		}
	case "item/started", "item/completed":
		if entry.turn.ID == "" || p.TurnID != entry.turn.ID || p.Item.Type != "agentMessage" || p.Item.ID == "" || len(p.Item.ID) > 256 {
			return
		}
		index := slices.IndexFunc(entry.turn.Items, func(item webLiveItem) bool { return item.ID == p.Item.ID })
		if p.Item.Moderation != nil && p.Item.Moderation.Moderated {
			if index >= 0 {
				entry.turn.Items = slices.Delete(entry.turn.Items, index, index+1)
			}
			return
		}
		item := webLiveItem{ID: p.Item.ID, Phase: p.Item.Phase, Text: limitLiveText(p.Item.Text), Streaming: event.Method == "item/started"}
		if index >= 0 {
			entry.turn.Items[index] = item
		} else if len(entry.turn.Items) < 12 {
			entry.turn.Items = append(entry.turn.Items, item)
		}
	case "item/agentMessage/delta":
		if entry.turn.ID == "" || p.TurnID != entry.turn.ID || entry.turn.Status != "inProgress" {
			return
		}
		for i := range entry.turn.Items {
			item := &entry.turn.Items[i]
			if item.ID == p.ItemID && item.Streaming && len(item.Text) < liveTextLimit {
				item.Text = limitLiveText(item.Text + p.Delta)
				break
			}
		}
	}
	entry.touched = now
}

const liveTextLimit = 64 << 10

func limitLiveText(text string) string {
	if len(text) <= liveTextLimit {
		return text
	}
	end := liveTextLimit
	for !utf8.RuneStart(text[end]) {
		end--
	}
	return strings.Clone(text[:end])
}

func (w *WebChat) activity(selected, account string) webActivityReply {
	if account == "" {
		account = "default"
	}
	writers, known := w.writerSnapshot()
	result := webActivityReply{WritersKnown: known, Threads: make(map[string]webActivity, len(writers))}
	for id, writer := range writers {
		result.Threads[id] = writer.activity
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range w.connections {
		select {
		case <-c.client.Done:
			continue
		default:
		}
		for id, writer := range writers {
			if writer.pid == c.process.PID {
				activity := result.Threads[id]
				activity.Source = "web"
				result.Threads[id] = activity
			}
		}
		c.mu.Lock()
		for id, entry := range c.live {
			if writer, found := writers[id]; found && writer.pid != c.process.PID {
				continue
			}
			activity := entry.activity
			if activity.State == "idle" && time.Since(entry.touched) > webLiveRetention {
				continue
			}
			if activity.State == "running" && slices.ContainsFunc(entry.turn.Items, func(item webLiveItem) bool {
				return item.Streaming && item.Text != ""
			}) {
				activity.State = "responding"
			}
			result.Threads[id] = activity
			if id == selected && entry.turn.ID != "" {
				turn := entry.turn
				turn.Items = slices.Clone(turn.Items)
				result.Turn = &turn
				result.AccountTurn = c.account == account
			}
		}
		c.mu.Unlock()
	}
	return result
}
