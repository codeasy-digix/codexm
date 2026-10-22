package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/catalog"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Model a real catalog plus command output; short mocked conversations conceal
// repeated transcript formatting on every keystroke/cursor blink.
func largeChatModel() Model {
	m := dashboardReviewModel()
	j := m.Status.Jobs[0]
	m.CatalogReady = true
	for i := 0; i < 1000; i++ {
		m.Catalog.Sessions = append(m.Catalog.Sessions, catalog.Entry{ID: fmt.Sprintf("saved-%04d", i), ThreadID: fmt.Sprintf("saved-%04d", i), Title: fmt.Sprintf("일반 대화 %04d", i), Kind: "chat", State: "saved"})
	}
	m.Catalog = catalog.Merge(m.Catalog, m.Status.Jobs)
	m.enterChat(j)
	s := m.currentChat()
	s.ReadPending, s.ReadReady = false, true
	s.Thread = fixtureChatThread(j.ThreadID, strings.Repeat("완료한 작업 설명과 검증 결과입니다. ", 1500))
	s.Thread.Turns[0].Items = append(s.Thread.Turns[0].Items, cr.Item{ID: "command", Type: "commandExecution", Command: "fixture-test", Status: "completed", AggregatedOutput: strings.Repeat("isolated command output line\n", 10000)})
	m.prepareDetail()
	return m
}

func BenchmarkLargeChatTyping(b *testing.B) {
	m := largeChatModel()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		m = next.(Model)
		_ = m.View()
		if len(m.Input.Value()) > 100 {
			m.Input.Reset()
		}
	}
}
