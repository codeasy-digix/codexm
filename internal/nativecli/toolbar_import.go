package nativecli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
)

type ToolbarImportItem struct {
	LegacyID string `json:"legacy_id"`
	Account  string `json:"account,omitempty"`
	Action   string `json:"action"`
	Reason   string `json:"reason,omitempty"`
}
type ToolbarImportResult struct {
	Applied  bool                `json:"applied"`
	Complete bool                `json:"complete"`
	Items    []ToolbarImportItem `json:"items"`
	Receipt  string              `json:"receipt,omitempty"`
}
type toolbarPayload struct {
	Accounts []struct {
		ID            string `json:"id"`
		Kind          string `json:"kind"`
		DisplayName   string `json:"displayName"`
		WorkspaceName string `json:"workspaceName"`
	} `json:"accounts"`
	Order        []string          `json:"accountOrder"`
	DefaultNames map[string]string `json:"systemDefaultWorkspaceNames"`
}

func DefaultToolbarRoot() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "Application Support", "Codex Usage")
}

// ImportToolbar performs a one-time, copy-only migration. Existing native
// credentials/default/history are never overwritten. The original app store
// and a private credential backup remain available for rollback.
func ImportToolbar(c config.Config, source string, apply bool) (ToolbarImportResult, error) {
	result := ToolbarImportResult{Applied: apply, Complete: true, Items: []ToolbarImportItem{}}
	if source == "" {
		source = DefaultToolbarRoot()
	}
	if !filepath.IsAbs(source) {
		return result, errors.New("legacy source must be absolute")
	}
	source = filepath.Clean(source)
	if resolved, e := filepath.EvalSymlinks(source); e != nil {
		if os.IsNotExist(e) {
			return result, nil
		}
		return result, e
	} else {
		source = resolved
	}
	registryPath := filepath.Join(source, "accounts.json")
	b, err := readImportFile(registryPath)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	var payload toolbarPayload
	if json.Unmarshal(b, &payload) != nil {
		return result, errors.New("legacy account registry is invalid")
	}
	sourceHash := sha256.Sum256([]byte(source))
	receiptPath := filepath.Join(c.StateDir(), "migrations", "toolbar-"+hex.EncodeToString(sourceHash[:8])+".json")
	if previous, e := os.ReadFile(receiptPath); e == nil {
		if json.Unmarshal(previous, &result) != nil {
			return result, errors.New("legacy import receipt is invalid; inspect it before retrying")
		}
		result.Applied = false
		for i := range result.Items {
			if result.Items[i].Action != "skipped" {
				result.Items[i].Action = "already-imported"
			}
		}
		return result, nil
	} else if !os.IsNotExist(e) {
		return result, e
	}
	var lock *os.File
	if apply {
		if err = os.MkdirAll(filepath.Dir(receiptPath), 0700); err != nil {
			return result, err
		}
		fd, e := unix.Open(receiptPath+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if e != nil {
			return result, e
		}
		lock = os.NewFile(uintptr(fd), "toolbar migration")
		defer lock.Close()
		if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
			return result, errors.New("another legacy import is in progress")
		}
		if _, e = os.Stat(receiptPath); e == nil {
			return result, errors.New("legacy import finished elsewhere; refresh")
		}
	}
	names, err := accounts.SavedLogins(c)
	if err != nil && !os.IsNotExist(err) {
		return result, err
	}
	byIdentity := map[string]string{}
	for _, n := range names {
		identity := accounts.InspectIdentity(c, n)
		if identity.Key != "" && !slices.Contains(c.AccountRegistry.Removed, n) {
			byIdentity[identity.Key] = n
		}
	}
	validID := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	mapping := map[string]string{"system-default": "default"}
	for _, old := range payload.Accounts {
		if old.Kind != "managed" {
			continue
		}
		item := ToolbarImportItem{LegacyID: old.ID}
		if !validID.MatchString(old.ID) {
			return result, errors.New("legacy account identifier is invalid")
		}
		authPath := filepath.Join(source, "Accounts", old.ID, "CodexHome", "auth.json")
		resolved, e := filepath.EvalSymlinks(authPath)
		if e != nil || resolved != authPath {
			item.Action = "skipped"
			item.Reason = "missing or linked legacy credentials require manual review"
			result.Items = append(result.Items, item)
			continue
		}
		data, e := readImportFile(authPath)
		if e != nil {
			return result, e
		}
		identity := accounts.CredentialIdentity(data)
		if identity.Key == "" {
			item.Action = "skipped"
			item.Reason = "login and workspace identity are not both known"
			result.Items = append(result.Items, item)
			continue
		}
		name := byIdentity[identity.Key]
		if name != "" {
			item.Action = "reuse"
		} else {
			name = "usage-" + strings.ToLower(old.ID)
			if slices.Contains(names, name) {
				item.Action = "skipped"
				item.Reason = "destination nickname already exists"
				result.Items = append(result.Items, item)
				continue
			}
			item.Action = "import"
			if apply {
				root := config.Expand(c.Runtime.AccountsDir)
				if e = os.MkdirAll(root, 0700); e != nil {
					return result, e
				}
				if info, e := os.Lstat(root); e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return result, errors.New("account root must be a real directory")
				}
				accountLock, e := cr.AccountLoginLock(c, name)
				if e != nil {
					return result, e
				}
				backup := filepath.Join(filepath.Dir(receiptPath), "toolbar-backup-"+hex.EncodeToString(sourceHash[:8]), old.ID)
				e = os.MkdirAll(backup, 0700)
				if e == nil {
					e = config.AtomicWrite(filepath.Join(backup, "auth.json"), data, 0600)
				}
				target := filepath.Join(root, name)
				if e == nil {
					_, e = os.Lstat(filepath.Join(target, "auth.json"))
					if os.IsNotExist(e) {
						e = nil
					} else if e == nil {
						e = errors.New("destination credentials already exist")
					}
				}
				if e == nil {
					e = writeNewCredential(filepath.Join(target, "auth.json"), data)
				}
				current, readErr := readImportFile(authPath)
				if e == nil && (readErr != nil || !bytes.Equal(data, current)) {
					e = errors.New("legacy credentials changed during import; the copied account is preserved; quit the old app and verify or sign in again")
				}
				accountLock.Close()
				if e != nil {
					return result, e
				}
			}
			byIdentity[identity.Key] = name
			names = append(names, name)
		}
		item.Account = name
		mapping[old.ID] = name
		result.Items = append(result.Items, item)
		if apply {
			meta, e := readPresentation(c)
			if e != nil {
				return result, e
			}
			if _, exists := meta.Identities[identity.Key]; !exists {
				revision := accounts.InspectIdentity(c, name).Revision
				if e = EditPresentation(c, PresentationChange{Account: name, Revision: revision, DisplayName: &old.DisplayName, WorkspaceName: &old.WorkspaceName}); e != nil {
					return result, e
				}
			}
		}
	}
	result.Complete = true
	for _, item := range result.Items {
		if item.Action == "skipped" {
			result.Complete = false
		}
	}
	if !apply {
		return result, nil
	}
	current, e := readImportFile(registryPath)
	if e != nil || !bytes.Equal(current, b) {
		return result, errors.New("legacy registry changed during import; imported accounts are preserved; quit the old app and retry")
	}
	meta, e := readPresentation(c)
	if e != nil {
		return result, e
	}
	if len(meta.Order) == 0 {
		catalog, e := AccountCatalog(c)
		if e != nil {
			return result, e
		}
		order := []string{}
		for _, old := range payload.Order {
			if n := mapping[old]; n != "" && !slices.Contains(order, n) {
				order = append(order, n)
			}
		}
		for _, a := range catalog {
			if !slices.Contains(order, a.Name) {
				order = append(order, a.Name)
			}
		}
		if e = EditPresentation(c, PresentationChange{Order: order}); e != nil {
			return result, e
		}
	}
	// Restore a default workspace label only when its old workspace+email key
	// matches the current login. Never guess from the last default's name.
	identity := accounts.InspectIdentity(c, "default")
	if identity.Key != "" && identity.Email != "" {
		authTarget, e := filepath.EvalSymlinks(filepath.Join(config.Expand(c.Runtime.SharedCodexHome), "auth.json"))
		if e == nil {
			if auth, e := readImportFile(authTarget); e == nil {
				var a struct {
					Tokens struct {
						AccountID string `json:"account_id"`
					} `json:"tokens"`
				}
				if json.Unmarshal(auth, &a) == nil && a.Tokens.AccountID != "" {
					h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(a.Tokens.AccountID))))
					key := sha256.Sum256([]byte(hex.EncodeToString(h[:]) + "\x00" + strings.ToLower(strings.TrimSpace(identity.Email))))
					if name := payload.DefaultNames[hex.EncodeToString(key[:])]; name != "" {
						meta, _ = readPresentation(c)
						if meta.Identities[identity.Key].WorkspaceName == "" {
							if e = EditPresentation(c, PresentationChange{Account: "default", Revision: identity.Revision, WorkspaceName: &name}); e != nil {
								return result, e
							}
						}
					}
				}
			}
		}
	}
	result.Receipt = receiptPath
	backupRegistry := filepath.Join(filepath.Dir(receiptPath), "toolbar-registry-"+time.Now().Format("20060102-150405")+".json")
	if e = config.AtomicWrite(backupRegistry, b, 0600); e != nil {
		return result, e
	}
	encoded, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		return result, e
	}
	if e = config.AtomicWrite(receiptPath, append(encoded, '\n'), 0600); e != nil {
		return result, e
	}
	return result, nil
}

func readImportFile(p string) ([]byte, error) {
	fd, e := unix.Open(p, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), p)
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !s.Mode().IsRegular() || s.Size() > 1024*1024 {
		return nil, fmt.Errorf("invalid legacy file: %s", filepath.Base(p))
	}
	return io.ReadAll(io.LimitReader(f, 1024*1024))
}

func writeNewCredential(target string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(target), ".codexm-import-auth-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	// link is exclusive: never replace even a concurrently created credential.
	return os.Link(f.Name(), target)
}
