package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/panelcommand"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	assets "github.com/codeasy-digix/codexm/plugin"
)

type Installer struct {
	Config     config.Config
	Self, Home string
}
type installReceipt struct {
	Root, Marketplace, Source string
	Version                   string
}

func (i Installer) home() string {
	if i.Home != "" {
		return i.Home
	}
	h, _ := os.UserHomeDir()
	return h
}
func (i Installer) root() string {
	return filepath.Join(i.home(), ".local", "share", "codexm", "plugins")
}
func (i Installer) catalog() string {
	return filepath.Join(i.home(), ".agents", "plugins", "marketplace.json")
}
func (i Installer) receipt() string { return filepath.Join(i.Config.StateDir(), "plugin-install.json") }
func (i Installer) load() (installReceipt, error) {
	var r installReceipt
	b, e := os.ReadFile(i.receipt())
	if e != nil {
		return r, e
	}
	e = json.Unmarshal(b, &r)
	if e == nil && (r.Marketplace != i.catalog() || !strings.HasPrefix(r.Root, i.root()+string(filepath.Separator))) {
		e = errors.New("plugin receipt ownership mismatch")
	}
	return r, e
}
func (i Installer) mutateCatalog(fn func(map[string]json.RawMessage, []json.RawMessage) ([]json.RawMessage, error)) error {
	p := i.catalog()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	fd, e := syscall.Open(p+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	defer syscall.Close(fd)
	if e = syscall.Flock(fd, syscall.LOCK_EX); e != nil {
		return e
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	if st, e := os.Lstat(p); e == nil {
		if !st.Mode().IsRegular() {
			return errors.New("personal marketplace must be a regular file")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	b, e := os.ReadFile(p)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	doc := map[string]json.RawMessage{}
	if len(b) > 0 {
		if e = json.Unmarshal(b, &doc); e != nil {
			return e
		}
	} else {
		doc["name"] = raw("local-personal")
		doc["interface"] = raw(map[string]string{"displayName": "Personal plugins"})
	}
	var entries []json.RawMessage
	if v := doc["plugins"]; len(v) > 0 {
		if e = json.Unmarshal(v, &entries); e != nil {
			return e
		}
	}
	next, e := fn(doc, entries)
	if e != nil {
		return e
	}
	doc["plugins"] = raw(next)
	out, e := json.MarshalIndent(doc, "", "  ")
	if e != nil {
		return e
	}
	if len(b) > 0 {
		if e = config.AtomicWrite(p+".codexm.previous", b, 0600); e != nil {
			return e
		}
	}
	return config.AtomicWrite(p, append(out, '\n'), 0600)
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func entryIdentity(b json.RawMessage) (name, source string, err error) {
	var entry struct {
		Name   string          `json:"name"`
		Source json.RawMessage `json:"source"`
	}
	if err = json.Unmarshal(b, &entry); err != nil {
		return
	}
	name = entry.Name
	if name != "codexm" {
		return
	}
	if json.Unmarshal(entry.Source, &source) == nil {
		return
	}
	var local struct {
		Path string `json:"path"`
	}
	err = json.Unmarshal(entry.Source, &local)
	source = local.Path
	return
}
func (i Installer) Install() (installReceipt, error) {
	if !filepath.IsAbs(i.Self) || !filepath.IsAbs(i.Config.Path) {
		return installReceipt{}, errors.New("absolute executable/configuration paths required")
	}
	if _, e := cr.Locate(i.Config); e != nil {
		return installReceipt{}, e
	}
	legacy, e := assets.Files.ReadFile(".codex-plugin/plugin.json")
	if e != nil {
		return installReceipt{}, e
	}
	server := map[string]any{"command": i.Self, "args": []string{"--config", i.Config.Path, "plugin", "mcp"}}
	marketplace := "local-personal"
	if data, err := os.ReadFile(i.catalog()); err == nil {
		var catalog struct {
			Name string `json:"name"`
		}
		if err = json.Unmarshal(data, &catalog); err != nil {
			return installReceipt{}, err
		}
		if catalog.Name != "" {
			marketplace = catalog.Name
		}
	} else if !os.IsNotExist(err) {
		return installReceipt{}, err
	}
	if _, e = panelcommand.Link(marketplace, "/"); e != nil {
		return installReceipt{}, e
	}
	hooks, e := assets.Files.ReadFile("hooks/hooks.json")
	if e != nil {
		return installReceipt{}, e
	}
	hooks = []byte(strings.ReplaceAll(string(hooks), "--marketplace 'local-personal'", "--marketplace '"+marketplace+"'"))
	compat := raw(map[string]any{"mcpServers": map[string]any{"codexm": server}})
	// The official 0.162 engines ignore hooks in portable AgentPlugin packages.
	// Install the documented .codex-plugin format, without a root plugin.json
	// that would take precedence and silently disable prompt interception.
	// The package-local hook launcher calls our immutable binary; it never
	// wraps official Codex or depends on the desktop application's PATH.
	launcher := []byte("#!/bin/sh\nexec '" + strings.ReplaceAll(i.Self, "'", "'\"'\"'") + "' \"$@\"\n")
	files := map[string][]byte{".codex-plugin/plugin.json": legacy, ".mcp.json": compat, "bin/codexm": launcher, "hooks/hooks.json": hooks}
	e = fs.WalkDir(assets.Files, "skills", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := assets.Files.ReadFile(name)
		if err == nil {
			files[name] = data
		}
		return err
	})
	if e != nil {
		return installReceipt{}, e
	}
	// Include every packaged path and payload in a canonical digest, including
	// all skills, metadata and future supporting resources.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", name, len(files[name]))
		_, _ = h.Write(files[name])
	}
	dest := filepath.Join(i.root(), hex.EncodeToString(h.Sum(nil)[:12]))
	for name, data := range files {
		p := filepath.Join(dest, name)
		mode := os.FileMode(0600)
		if name == "bin/codexm" {
			mode = 0700
		}
		if st, e := os.Lstat(p); e == nil && !st.Mode().IsRegular() {
			return installReceipt{}, errors.New("plugin asset must be a regular file")
		} else if e != nil && !os.IsNotExist(e) {
			return installReceipt{}, e
		}
		if b, e := os.ReadFile(p); e == nil {
			if string(b) != string(data) {
				return installReceipt{}, errors.New("immutable plugin files differ; existing files preserved")
			}
			if st, e := os.Stat(p); e != nil || st.Mode().Perm() != mode {
				return installReceipt{}, errors.New("immutable plugin file permissions differ; existing files preserved")
			}
			continue
		} else if !os.IsNotExist(e) {
			return installReceipt{}, e
		}
		if e = config.AtomicWrite(p, data, mode); e != nil {
			return installReceipt{}, e
		}
	}
	rel, e := filepath.Rel(i.home(), dest)
	if e != nil || strings.HasPrefix(rel, "..") {
		return installReceipt{}, errors.New("plugin must be within the personal marketplace root")
	}
	r := installReceipt{dest, i.catalog(), "./" + filepath.ToSlash(rel), config.Version}
	old, oldErr := i.load()
	e = i.mutateCatalog(func(doc map[string]json.RawMessage, entries []json.RawMessage) ([]json.RawMessage, error) {
		var currentName string
		_ = json.Unmarshal(doc["name"], &currentName)
		if currentName != "" && currentName != marketplace {
			return nil, errors.New("marketplace changed during installation; existing entry preserved")
		}
		out := []json.RawMessage{}
		for _, b := range entries {
			name, source, e := entryIdentity(b)
			if e != nil {
				return nil, e
			}
			if name == "codexm" {
				if oldErr != nil || source != old.Source {
					return nil, errors.New("an unowned codexm marketplace entry exists; it was preserved")
				}
				continue
			}
			out = append(out, b)
		}
		return append(out, raw(map[string]any{"name": "codexm", "source": map[string]string{"source": "local", "path": r.Source}, "policy": map[string]string{"installation": "AVAILABLE", "authentication": "ON_INSTALL"}, "category": "Productivity"})), nil
	})
	if e != nil {
		return r, e
	}
	return r, config.AtomicWrite(i.receipt(), raw(r), 0600)
}
func (i Installer) Uninstall() error {
	r, e := i.load()
	if e != nil {
		return e
	}
	e = i.mutateCatalog(func(_ map[string]json.RawMessage, entries []json.RawMessage) ([]json.RawMessage, error) {
		out := []json.RawMessage{}
		for _, b := range entries {
			name, source, e := entryIdentity(b)
			if e != nil {
				return nil, e
			}
			if name == "codexm" {
				if source != r.Source {
					return nil, errors.New("codexm entry was changed outside the installer; preserved")
				}
				continue
			}
			out = append(out, b)
		}
		return out, nil
	})
	if e != nil {
		return e
	}
	// Keep version assets recoverable; remove only our registration receipt.
	return os.Remove(i.receipt())
}
func (i Installer) Status() map[string]any {
	r, e := i.load()
	result := map[string]any{"registered": e == nil, "version": config.Version, "transport": "stdio", "standalone_web_server": false, "host_ui_verified": false, "marketplace": i.catalog()}
	if e == nil {
		result["source"] = r.Root
	} else if !os.IsNotExist(e) {
		result["problem"] = e.Error()
	}
	return result
}
func Command(ctx context.Context, c config.Config, self string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: codexm plugin install|status|doctor|uninstall|mcp")
	}
	i := Installer{Config: c, Self: self}
	switch args[0] {
	case "mcp":
		return (MCP{Config: c, Self: self}).Serve(ctx, os.Stdin, out)
	case "install":
		r, e := i.Install()
		if e != nil {
			return e
		}
		fmt.Fprintf(out, "codexm plugin registered in %s\nOpen ChatGPT Plugins → Personal plugins → codexm and install/refresh it.\nReview and trust codexm's UserPromptSubmit hook in Codex Hooks before using @codexm manage without a model.\nLocal MCP tools and the MCP Apps UI are separate host capabilities; verify the manager panel in the app.\n", r.Marketplace)
		return nil
	case "uninstall":
		if e := i.Uninstall(); e != nil {
			return e
		}
		fmt.Fprintln(out, "codexm marketplace entry removed. Cached host installation, saved work and accounts were preserved.")
		return nil
	case "status", "doctor":
		s := i.Status()
		if args[0] == "doctor" {
			_, e := cr.Locate(c)
			s["official_codex_found"] = e == nil
			html, e := HTML()
			s["ui_resource_ready"] = e == nil && strings.Contains(html, "ui/initialize")
			s["mcp_tools"] = len(toolList())
			if s["registered"] == true {
				if discovery, e := i.Discovery(ctx); e == nil {
					for key, value := range discovery {
						s[key] = value
					}
				} else {
					s["host_discovery_problem"] = cr.SafeText(e.Error())
				}
			}
			s["panel_health"] = i.panelHealth()
		}
		return json.NewEncoder(out).Encode(s)
	default:
		return errors.New("usage: codexm plugin install|status|doctor|uninstall|mcp")
	}
}
