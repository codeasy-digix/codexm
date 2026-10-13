package accounts

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/codeasy-digix/codexm/internal/config"
)

// CurrentNickname recognizes an existing login without refreshing it, copying
// credentials, or returning any credential/account identifier.
func CurrentNickname(c config.Config, codexHome, account string) string {
	names, e := List(c)
	if e != nil {
		return ""
	}
	root := config.Expand(c.Runtime.AccountsDir)
	if codexHome == "" {
		codexHome = config.Expand(c.Runtime.SharedCodexHome)
	}
	resolved, e := filepath.EvalSymlinks(codexHome)
	if e != nil {
		return ""
	}
	for _, name := range names {
		home, e := filepath.EvalSymlinks(filepath.Join(root, name))
		if e == nil && home == resolved && (account == "" || account == name) {
			return name
		}
	}
	// A native default link identifies the selected nickname even when several
	// saved aliases hold credentials for the same underlying account.
	if selected, e := filepath.EvalSymlinks(filepath.Join(codexHome, "auth.json")); e == nil {
		for _, name := range names {
			p := filepath.Join(root, name, "auth.json")
			if info, e := os.Lstat(p); e != nil || !info.Mode().IsRegular() {
				continue
			}
			p, e := filepath.EvalSymlinks(p)
			if e == nil && selected == p && (account == "" || account == name) {
				return name
			}
		}
	}
	id := accountID(filepath.Join(codexHome, "auth.json"))
	if id == "" {
		return ""
	}
	matching := []string{}
	for _, name := range names {
		if accountID(filepath.Join(root, name, "auth.json")) == id {
			matching = append(matching, name)
		}
	}
	if slices.Contains(matching, account) {
		return account
	}
	for _, name := range c.WorkerAccounts.Priority {
		if slices.Contains(matching, name) {
			return name
		}
	}
	return ""
}

func accountID(filename string) string {
	f, e := os.Open(filename)
	if e != nil {
		return ""
	}
	defer f.Close()
	var auth struct {
		Tokens struct {
			AccountID string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.NewDecoder(io.LimitReader(f, 256*1024)).Decode(&auth) != nil {
		return ""
	}
	return auth.Tokens.AccountID
}
