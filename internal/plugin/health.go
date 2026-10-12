package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

// These are observations, not a claim that a native host displayed a panel.
// Keep separate atomic records so concurrent MCP processes cannot erase stages.
type panelObservation struct {
	At      time.Time `json:"at"`
	Version string    `json:"version"`
	UIBuild string    `json:"ui_build"`
	Bridge  string    `json:"bridge,omitempty"`
	Visible *bool     `json:"visible,omitempty"`
}

var panelStages = []string{"mcp_initialized", "open_requested", "resource_served", "panel_connected"}

func uiBuild(html string) string {
	sum := sha256.Sum256([]byte(html))
	return hex.EncodeToString(sum[:])
}

func validUIBuild(value string) bool {
	b, e := hex.DecodeString(value)
	return e == nil && len(b) == sha256.Size && strings.ToLower(value) == value
}

func (m MCP) observePanel(stage string, observed *panelObservation) error {
	known := false
	for _, name := range panelStages {
		known = known || name == stage
	}
	if !known {
		return errors.New("unknown panel diagnostic stage")
	}
	if observed == nil {
		_, build, e := uiResource()
		if e != nil {
			return e
		}
		observed = &panelObservation{UIBuild: build}
	}
	observed.At, observed.Version = time.Now().UTC(), config.Version
	data, e := json.Marshal(observed)
	if e != nil {
		return e
	}
	return config.AtomicWrite(filepath.Join(m.Config.StateDir(), "plugin-health", stage+".json"), append(data, '\n'), 0600)
}

func (i Installer) panelHealth() map[string]any {
	_, build, e := uiResource()
	result := map[string]any{"current_ui_build": build, "observations": map[string]panelObservation{}, "panel_connection_matches_current_ui": false, "proves_visible_ui": false}
	if e != nil {
		return result
	}
	observations := result["observations"].(map[string]panelObservation)
	for _, stage := range panelStages {
		data, e := os.ReadFile(filepath.Join(i.Config.StateDir(), "plugin-health", stage+".json"))
		if e != nil || len(data) > 4096 {
			continue
		}
		var observed panelObservation
		if json.Unmarshal(data, &observed) != nil || observed.At.IsZero() || !validUIBuild(observed.UIBuild) {
			continue
		}
		observations[stage] = observed
	}
	if observed, ok := observations["panel_connected"]; ok {
		result["panel_connection_matches_current_ui"] = observed.UIBuild == build && observed.Version == config.Version
	}
	return result
}

func (m MCP) panelReady(args json.RawMessage) (any, error) {
	var report struct {
		UIBuild string `json:"ui_build"`
		Bridge  string `json:"bridge"`
		Visible *bool  `json:"visible"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(args)))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&report); e != nil || !validUIBuild(report.UIBuild) || (report.Bridge != "mcp-app" && report.Bridge != "legacy") || report.Visible == nil {
		return nil, errors.New("valid UI build, bridge and visibility required")
	}
	_, current, e := uiResource()
	if e != nil {
		return nil, e
	}
	// Reporting is optional; an unwritable diagnostic directory must never block
	// opening the panel, saving settings or continuing work.
	e = m.observePanel("panel_connected", &panelObservation{UIBuild: report.UIBuild, Bridge: report.Bridge, Visible: report.Visible})
	return map[string]any{"recorded": e == nil, "ui_build_current": current == report.UIBuild}, nil
}
