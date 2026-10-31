package historysynccli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/historysync"
	"github.com/codeasy-digix/codexm/internal/service"
)

func relayService(configPath, self string) service.Manager {
	c := config.Defaults()
	c.Path = configPath
	// Distinct relay configs cannot share one service or receipt. The relay's
	// own store lock still protects aliases of the same data directory.
	h := sha256.Sum256([]byte(configPath))
	c.Storage.StateDir = filepath.Join(c.StateDir(), "history-sync-services", fmt.Sprintf("%x", h[:12]))
	return service.Manager{Config: c, Self: self, Kind: "history-sync"}
}

func legacyServicePath(platform string) string {
	if platform == "darwin" {
		return "/Library/LaunchDaemons/kr.digix.codex-history-sync.plist"
	}
	return "/etc/systemd/system/codex-history-sync.service"
}

func rejectLegacyService(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("existing system history relay was preserved: %s; stop and retire that unit before installing or starting a second user service", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func validateServiceConfig(path string, starting bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.UseNumber()
	d.DisallowUnknownFields()
	var c historysync.Config
	if err = d.Decode(&c); err != nil {
		return err
	}
	var trailing any
	if !errors.Is(d.Decode(&trailing), io.EOF) {
		return errors.New("extra JSON after configuration")
	}
	if _, err = historysync.NormalizeConfig(c); err != nil {
		return err
	}
	if starting && !c.Enabled {
		return errors.New("history synchronization is disabled in configuration")
	}
	return nil
}

func serviceCommand(ctx context.Context, configPath string, args []string, out io.Writer) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		fmt.Fprintln(out, "Usage: codexm history-sync [--config PATH] service install|start|stop|restart|status [--json]|logs|uninstall\nInstall only prepares a user service; start is explicit. Existing system relays are never replaced automatically.")
		return nil
	}
	action := args[0]
	if len(args) > 1 && !(action == "status" && len(args) == 2 && args[1] == "--json") {
		return errors.New("unexpected history-sync service arguments")
	}
	p, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	// Resolve aliases so an XDG symlink and its target manage the same unit.
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	m := relayService(p, self)
	legacy := legacyServicePath(runtime.GOOS)
	if action == "install" || action == "start" || action == "restart" {
		if err = rejectLegacyService(legacy); err != nil {
			return err
		}
		if err = validateServiceConfig(p, action != "install"); err != nil {
			return err
		}
	}
	if action == "install" {
		args = []string{"install", "--no-start"}
	}
	if action == "status" {
		s := m.Status(ctx)
		legacyDefinition := ""
		if _, err := os.Lstat(legacy); err == nil {
			legacyDefinition = legacy
		}
		if len(args) == 2 {
			return json.NewEncoder(out).Encode(struct {
				service.Status
				LegacyDefinition string `json:"legacy_definition,omitempty"`
			}{s, legacyDefinition})
		}
		if legacyDefinition != "" {
			fmt.Fprintln(out, "Existing system relay (separate lifecycle):", legacyDefinition)
		}
	}
	return m.Command(ctx, args, out)
}
