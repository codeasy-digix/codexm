package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/codeasy-digix/codexm/internal/config"
)

// LoginIdentity contains display claims, never credentials. Key distinguishes
// members of one workspace as well as one person's different workspaces.
// Claims are local identification hints, not proof of an authenticated session.
type LoginIdentity struct {
	Key      string `json:"identity,omitempty"`
	Revision string `json:"revision"`
	Email    string `json:"email,omitempty"`
	Plan     string `json:"plan,omitempty"`
}

func CredentialIdentity(data []byte) LoginIdentity {
	var auth struct {
		Tokens struct {
			AccountID string `json:"account_id"`
			IDToken   string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(data, &auth) != nil {
		return LoginIdentity{}
	}
	parts := strings.Split(auth.Tokens.IDToken, ".")
	if len(parts) != 3 {
		return LoginIdentity{}
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return LoginIdentity{}
	}
	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Auth    struct {
			UserID    string `json:"chatgpt_user_id"`
			AccountID string `json:"chatgpt_account_id"`
			Plan      string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(b, &claims) != nil {
		return LoginIdentity{}
	}
	workspace := auth.Tokens.AccountID
	if workspace == "" {
		workspace = claims.Auth.AccountID
	}
	user := claims.Auth.UserID
	if user == "" {
		user = claims.Subject
	}
	if user == "" {
		user = strings.ToLower(strings.TrimSpace(claims.Email))
	}
	i := LoginIdentity{Email: strings.TrimSpace(claims.Email), Plan: claims.Auth.Plan}
	if user != "" && workspace != "" {
		i.Key = identityHash(user + "\x00" + workspace)
	}
	return i
}

func InspectIdentity(c config.Config, name string) LoginIdentity {
	home := config.Expand(c.Runtime.SharedCodexHome)
	if name != "" && name != "default" {
		if !config.Nickname.MatchString(name) {
			return LoginIdentity{}
		}
		home = filepath.Join(config.Expand(c.Runtime.AccountsDir), name)
	}
	p := filepath.Join(home, "auth.json")
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return LoginIdentity{Revision: identityHash(p + "\x00missing")}
	}
	b, err := nativePrivateRead(resolved, false)
	if err != nil {
		return LoginIdentity{Revision: identityHash(p + "\x00unreadable")}
	}
	i := CredentialIdentity(b)
	// The resolved target detects an alias/default switch even when both files
	// describe the same login. Token refresh alone does not change the identity.
	i.Revision = identityHash(resolved + "\x00" + i.Key)
	if i.Key == "" {
		if stat, e := os.Stat(resolved); e == nil {
			i.Revision = identityHash(resolved + stat.ModTime().String())
		}
	}
	return i
}

func identityHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
