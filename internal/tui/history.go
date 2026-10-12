package tui

import "strings"

func (m *Model) rememberInput(value string) {
	if strings.TrimSpace(value) != "" && (len(m.History) == 0 || m.History[len(m.History)-1] != value) {
		m.History = append(m.History, value)
		if len(m.History) > 100 {
			m.History = m.History[len(m.History)-100:]
		}
	}
	m.HistoryIndex = len(m.History)
	m.HistoryDraft = ""
}

func (m *Model) historyMove(older bool) {
	if len(m.History) == 0 {
		return
	}
	if older {
		if m.HistoryIndex == len(m.History) {
			m.HistoryDraft = m.Input.Value()
		}
		m.HistoryIndex = max(0, m.HistoryIndex-1)
	} else {
		m.HistoryIndex = min(len(m.History), m.HistoryIndex+1)
	}
	value := m.HistoryDraft
	if m.HistoryIndex < len(m.History) {
		value = m.History[m.HistoryIndex]
	}
	m.Input.SetValue(value)
	m.Input.CursorEnd()
	m.PaletteDismissed = true
}
