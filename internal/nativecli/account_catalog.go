package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"golang.org/x/sys/unix"
)

type AccountPresentation struct {
	DisplayName   string `json:"display_name,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}
type presentationFile struct {
	Version    int                            `json:"version"`
	Identities map[string]AccountPresentation `json:"identities"`
	Order      []string                       `json:"order"`
}
type DesktopAccount struct {
	Name             string `json:"name"`
	Default          bool   `json:"default"`
	SelectedNickname string `json:"selected_nickname,omitempty"`
	accounts.LoginIdentity
	AccountPresentation
}

func readPresentation(c config.Config) (presentationFile, error) {
	p := presentationFile{Version: 1, Identities: map[string]AccountPresentation{}, Order: []string{}}
	b, err := os.ReadFile(c.Path + ".accounts.json")
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if len(b) > 1024*1024 || json.Unmarshal(b, &p) != nil || p.Version != 1 {
		return p, errors.New("invalid account presentation file; existing settings were preserved")
	}
	if p.Identities == nil {
		p.Identities = map[string]AccountPresentation{}
	}
	return p, nil
}

// AccountCatalog is independent of the conversation DB and can be polled by
// the menu bar without scanning chats, retaining an auth server, or refreshing tokens.
func AccountCatalog(c config.Config) ([]DesktopAccount, error) {
	named, err := Accounts(c)
	if err != nil {
		return nil, err
	}
	meta, err := readPresentation(c)
	if err != nil {
		return nil, err
	}
	def := accounts.CurrentNickname(c, config.Expand(c.Runtime.SharedCodexHome), "")
	result := []DesktopAccount{{Name: "default", Default: true, SelectedNickname: def, LoginIdentity: accounts.InspectIdentity(c, "default")}}
	for _, a := range named {
		result = append(result, DesktopAccount{Name: a.Name, Default: a.Default, LoginIdentity: accounts.InspectIdentity(c, a.Name)})
	}
	for i := range result {
		result[i].AccountPresentation = meta.Identities[result[i].Key]
	}
	ordered := make([]DesktopAccount, 0, len(result))
	for _, name := range meta.Order {
		for _, a := range result {
			if a.Name == name && !slices.ContainsFunc(ordered, func(x DesktopAccount) bool { return x.Name == name }) {
				ordered = append(ordered, a)
			}
		}
	}
	for _, a := range result {
		if !slices.ContainsFunc(ordered, func(x DesktopAccount) bool { return x.Name == a.Name }) {
			ordered = append(ordered, a)
		}
	}
	return ordered, nil
}

type PresentationChange struct {
	Account       string   `json:"account"`
	Revision      string   `json:"revision"`
	DisplayName   *string  `json:"display_name,omitempty"`
	WorkspaceName *string  `json:"workspace_name,omitempty"`
	Order         []string `json:"order,omitempty"`
}

func EditPresentation(c config.Config, change PresentationChange) error {
	if err := os.MkdirAll(filepath.Dir(c.Path), 0700); err != nil {
		return err
	}
	fd, err := unix.Open(c.Path+".accounts.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err = unix.Flock(fd, unix.LOCK_EX); err != nil {
		return err
	}
	meta, err := readPresentation(c)
	if err != nil {
		return err
	}
	all, err := AccountCatalog(c)
	if err != nil {
		return err
	}
	if change.Order != nil {
		if len(change.Order) != len(all) {
			return errors.New("account list changed; refresh before reordering")
		}
		seen := map[string]bool{}
		for _, n := range change.Order {
			if seen[n] || !slices.ContainsFunc(all, func(a DesktopAccount) bool { return a.Name == n }) {
				return errors.New("invalid account order")
			}
			seen[n] = true
		}
		meta.Order = change.Order
	} else {
		var selected *DesktopAccount
		for i := range all {
			if all[i].Name == change.Account {
				selected = &all[i]
				break
			}
		}
		if selected == nil || selected.Key == "" || change.Revision != selected.Revision {
			return errors.New("account identity changed or is unknown; refresh before editing its name")
		}
		entry := meta.Identities[selected.Key]
		for _, pair := range []struct {
			input  *string
			output *string
		}{{change.DisplayName, &entry.DisplayName}, {change.WorkspaceName, &entry.WorkspaceName}} {
			if pair.input != nil {
				value := strings.TrimSpace(*pair.input)
				if len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
					return errors.New("name must be one line and at most 256 bytes")
				}
				*pair.output = value
			}
		}
		if accounts.InspectIdentity(c, change.Account).Revision != selected.Revision {
			return errors.New("account changed while saving; refresh and retry")
		}
		meta.Identities[selected.Key] = entry
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return config.AtomicWrite(c.Path+".accounts.json", append(b, '\n'), 0600)
}

type DesktopUsage struct {
	Revision string            `json:"revision"`
	Snapshot accounts.Snapshot `json:"snapshot"`
}

func ReadDesktopUsage(ctx context.Context, c config.Config, name string) (DesktopUsage, error) {
	all, err := AccountCatalog(c)
	if err != nil {
		return DesktopUsage{}, err
	}
	if !slices.ContainsFunc(all, func(a DesktopAccount) bool { return a.Name == name }) {
		return DesktopUsage{}, errors.New("account is no longer registered")
	}
	before := accounts.InspectIdentity(c, name)
	r, err := Resolver(c)
	if err != nil {
		return DesktopUsage{}, err
	}
	s := accounts.New(r).Read(ctx, name)
	s.AccountID = ""
	if accounts.InspectIdentity(c, name).Revision != before.Revision {
		return DesktopUsage{}, errors.New("account changed during usage read; refresh the current account")
	}
	if s.Error != "" {
		return DesktopUsage{}, errors.New(s.Error)
	}
	return DesktopUsage{Revision: before.Revision, Snapshot: s}, nil
}
