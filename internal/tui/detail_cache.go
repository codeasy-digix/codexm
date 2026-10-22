package tui

import (
	"github.com/charmbracelet/x/ansi"
)

// Rendering a long transcript belongs to content/width changes, not keyboard or
// cursor events. Keys contain only metadata; no transcript is scanned for hits.
type detailRenderKey struct {
	Epoch, Data, ChatVersion                   uint64
	Tab, Panel, Selected, Target, Thread, Text string
	Title, Account, State, Model, LastError    string
	Pending, Error, ReadError                  string
	Paused, ReadReady                          bool
	Requests                                   int
}

type detailRenderCache struct {
	Key      detailRenderKey
	Width    int
	Valid    bool
	Revision uint64
}

func (m Model) detailKey() detailRenderKey {
	k := detailRenderKey{Epoch: m.ViewEpoch, Data: m.DetailGeneration, Tab: m.Tab, Panel: m.Panel, Selected: m.SelectedID, Target: m.Target, Text: m.DetailText}
	if m.inChat() && m.Panel == "" && m.DetailText == "" {
		k.Data = 0 // Unrelated account/status polls do not alter a transcript.
		k.Thread = m.Chat.ThreadID
		if s := m.currentChat(); s != nil {
			k.ChatVersion, k.Pending, k.Error, k.ReadError, k.ReadReady = s.ContentVersion, s.Pending, s.Error, s.ReadError, s.ReadReady
		}
		if j, ok := m.selected(); ok {
			k.Title, k.Account, k.State, k.Model, k.LastError = j.Title, j.Account, j.State, j.Model, j.LastError
			k.Paused, k.Requests = j.Paused, len(j.PendingRequests)
		}
	}
	return k
}

func (m Model) detailContent() string {
	if m.Tab == "logs" {
		return m.logsText()
	}
	return m.content()
}

func (m *Model) prepareDetail() {
	if m.Screen != "agents" || m.managementVisible() || m.TaskForm.Kind != "" {
		return
	}
	width, height := max(10, m.Width-2), max(2, m.Height-10)
	_, hits := m.dashboardFrame(true)
	for _, hit := range hits {
		if hit.Action == "area:detail" {
			width, height = max(1, hit.W), max(1, hit.H)
			break
		}
	}
	if m.Detail.Width() != width {
		m.Detail.SetWidth(width)
	}
	if m.Detail.Height() != height {
		m.Detail.SetHeight(height)
	}
	key := m.detailKey()
	if !m.DetailCache.Valid || m.DetailCache.Key != key || m.DetailCache.Width != width {
		m.Detail.SetContent(ansi.Wrap(plain(m.detailContent()), width, ""))
		m.DetailCache = detailRenderCache{Key: key, Width: width, Valid: true, Revision: m.DetailCache.Revision + 1}
	}
	if m.inChat() {
		if s := m.currentChat(); s != nil {
			if anchor := s.PrependAnchor; anchor != nil {
				if !m.AutoFollow {
					m.Detail.SetYOffset(anchor.Offset + max(0, m.Detail.TotalLineCount()-anchor.Lines))
					s.Offset = m.Detail.YOffset()
				}
				s.PrependAnchor = nil
			}
			s.ViewLines = m.Detail.TotalLineCount()
		}
	}
	if m.AutoFollow && m.Focus != "detail" && m.Panel == "" && m.DetailText == "" {
		m.Detail.GotoBottom()
	}
}
