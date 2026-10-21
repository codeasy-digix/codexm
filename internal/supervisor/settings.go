package supervisor

import (
	"context"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/settings"
)

type EditableSettings = settings.Values
type SettingsDocument = settings.Document

func (s *Server) settingsDocument() (SettingsDocument, error) { return settings.Read(s.Config) }
func (s *Server) saveSettings(ctx context.Context, b json.RawMessage) (SettingsDocument, error) {
	c, err := config.Load(s.Config.Path)
	if err != nil {
		return SettingsDocument{}, err
	}
	doc, err := settings.Save(ctx, c, s.Reader, b)
	if err == nil {
		s.mu.Lock()
		s.managerAccount = doc.Values.ManagerAccount
		s.mu.Unlock()
	}
	return doc, err
}
