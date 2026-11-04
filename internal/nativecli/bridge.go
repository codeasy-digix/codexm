package nativecli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type Method struct {
	Name     string `json:"name"`
	Mutation bool   `json:"mutation"`
	Model    bool   `json:"model"`
}

var Methods = []Method{{"accounts.catalog", false, false}, {"accounts.usage", false, false}, {"accounts.presentation", true, false}, {"accounts.importLegacy", true, false}, {"accounts.activate", true, true}, {"capabilities", false, false}, {"snapshot", false, false}, {"accounts.refresh", false, false}, {"accounts.remove", true, false}, {"accounts.setDefault", true, false}, {"login.start", true, false}, {"login.status", false, false}, {"login.cancel", true, false}, {"chats.archive", true, false}, {"chats.unarchive", true, false}, {"terminal.open", true, false}, {"terminal.read", false, false}, {"terminal.input", true, false}, {"terminal.close", true, false}}

type Request struct {
	Version   int             `json:"protocolVersion"`
	ID        json.RawMessage `json:"id"`
	Method    string          `json:"method"`
	Owner     string          `json:"owner"`
	Operation string          `json:"operation_id"`
	Params    json.RawMessage `json:"params"`
}
type operation struct {
	Fingerprint string          `json:"fingerprint"`
	Done        bool            `json:"done"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
}
type Params struct {
	Thread   string `json:"thread_id"`
	Account  string `json:"account"`
	Session  string `json:"session"`
	Text     string `json:"text"`
	Key      string `json:"key"`
	Mode     string `json:"mode"`
	LoginID  string `json:"login_id"`
	Revision string `json:"revision"`
}

func Dispatch(ctx context.Context, c config.Config, self string, r Request) (any, error) {
	if r.Version != 1 {
		return nil, errors.New("unsupported bridge protocol")
	}
	var spec *Method
	for i := range Methods {
		if Methods[i].Name == r.Method {
			spec = &Methods[i]
			break
		}
	}
	if spec == nil {
		return nil, errors.New("unsupported compact command; manager and worker controls are retired")
	}
	var p Params
	if len(r.Params) > 0 {
		if e := json.Unmarshal(r.Params, &p); e != nil {
			return nil, e
		}
	}
	call := func() (any, error) {
		switch r.Method {
		case "capabilities":
			return map[string]any{"protocolVersion": 1, "generation": "native-cli-v1", "methods": Methods}, nil
		case "accounts.catalog":
			return AccountCatalog(c)
		case "accounts.usage":
			return ReadDesktopUsage(ctx, c, p.Account)
		case "accounts.presentation":
			var change PresentationChange
			if e := json.Unmarshal(r.Params, &change); e != nil {
				return nil, e
			}
			return true, EditPresentation(c, change)
		case "accounts.importLegacy":
			var args struct {
				Source string `json:"source"`
				Apply  bool   `json:"apply"`
			}
			if e := json.Unmarshal(r.Params, &args); e != nil {
				return nil, e
			}
			return ImportToolbar(c, args.Source, args.Apply)
		case "accounts.activate":
			var args ActivationRequest
			if e := json.Unmarshal(r.Params, &args); e != nil {
				return nil, e
			}
			return true, ActivateUsage(ctx, c, args)
		case "snapshot":
			list, e := List(ctx, c)
			if e != nil {
				return nil, e
			}
			a, e := Accounts(c)
			return map[string]any{"sessions": list.Sessions, "accounts": a, "terminals": Sessions(c), "warning": list.Warning, "mode": "native-cli", "paths": map[string]string{"config": c.Path, "history": config.Expand(c.Runtime.SharedCodexHome), "accounts": config.Expand(c.Runtime.AccountsDir)}}, e
		case "accounts.refresh":
			a, e := Accounts(c)
			if e != nil {
				return nil, e
			}
			exists := false
			for _, x := range a {
				if x.Name == p.Account {
					exists = true
				}
			}
			if !exists {
				return nil, errors.New("select an existing local account")
			}
			r, e := Resolver(c)
			if e != nil {
				return nil, e
			}
			quotaContext, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			s := accounts.New(r).Read(quotaContext, p.Account)
			s.AccountID = ""
			return s, nil
		case "accounts.remove", "accounts.setDefault":
			if err := requireRegisteredAccount(c, p.Account); err != nil {
				return nil, err
			}
			lock, err := cr.AccountLoginLock(c, p.Account)
			if err != nil {
				return nil, err
			}
			defer lock.Close()
			if p.Revision != "" && accounts.InspectIdentity(c, p.Account).Revision != p.Revision {
				return nil, errors.New("account changed while editing; refresh and try again")
			}
			if r.Method == "accounts.remove" {
				return true, RemoveAccount(c, p.Account)
			}
			_, err = accounts.SetNativeDefault(c, p.Account)
			return true, err
		case "terminal.open":
			return Open(ctx, c, self, p.Thread, p.Account)
		case "chats.archive", "chats.unarchive":
			return SetChatArchived(ctx, c, p.Thread, r.Method == "chats.archive")
		case "terminal.read":
			return Screen(ctx, c, p.Session)
		case "terminal.input":
			return true, Input(ctx, c, p.Session, p.Text, p.Key)
		case "terminal.close":
			return true, Close(ctx, c, p.Session)
		}
		return nil, errors.New("unsupported method")
	}
	if !spec.Mutation {
		return call()
	}
	return mutate(c, r, call, true)
}

func mutate(c config.Config, r Request, call func() (any, error), persistResult bool) (any, error) {
	if r.Owner == "" || !regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`).MatchString(r.Operation) {
		return nil, errors.New("authenticated owner and operation id required")
	}
	key := sha256.Sum256([]byte(r.Owner + "\x00" + r.Operation))
	finger := sha256.Sum256(append([]byte(r.Method+"\x00"), r.Params...))
	file := filepath.Join(root(c), "operations", hex.EncodeToString(key[:])+".json")
	if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
		return nil, e
	}
	op := operation{Fingerprint: hex.EncodeToString(finger[:])}
	f, e := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(e) {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var saved operation
		if json.Unmarshal(b, &saved) != nil || saved.Fingerprint != op.Fingerprint {
			return nil, errors.New("operation id reused or incomplete receipt")
		}
		if !saved.Done {
			return nil, errors.New("previous outcome unknown; inspect the terminal before retrying")
		}
		if saved.Error != "" {
			return nil, errors.New(saved.Error)
		}
		if !persistResult {
			return nil, errors.New("sign-in outcome is no longer in memory; inspect status before starting a new request")
		}
		return saved.Result, nil
	}
	if e != nil {
		return nil, e
	}
	e = json.NewEncoder(f).Encode(op)
	closeErr := f.Close()
	if e != nil {
		return nil, e
	}
	if closeErr != nil {
		return nil, closeErr
	}
	result, e := call()
	op.Done = true
	if e != nil {
		op.Error = cr.SafeText(e.Error())
	} else if persistResult {
		op.Result, e = json.Marshal(result)
		if e != nil {
			op.Error = e.Error()
		}
	}
	b, _ := json.Marshal(op)
	if saveErr := config.AtomicWrite(file, b, 0600); saveErr != nil {
		return nil, fmt.Errorf("operation outcome unknown: %w", saveErr)
	}
	return result, e
}

func Serve(ctx context.Context, c config.Config, self string, in io.Reader, out io.Writer) error {
	logins := newLoginSession(ctx)
	defer logins.Close()
	s := bufio.NewScanner(in)
	s.Buffer(make([]byte, 4096), 512*1024)
	encoder := json.NewEncoder(out)
	for s.Scan() {
		var r Request
		if e := json.Unmarshal(s.Bytes(), &r); e != nil {
			return e
		}
		current := c
		var e error
		if _, statErr := os.Stat(c.Path); statErr == nil {
			current, e = Load(c.Path)
		} else if !os.IsNotExist(statErr) {
			e = statErr
		}
		var result any
		if e == nil {
			if r.Method == "login.start" || r.Method == "login.status" || r.Method == "login.cancel" {
				result, e = logins.Dispatch(current, r)
			} else {
				result, e = Dispatch(ctx, current, self, r)
			}
		}
		reply := map[string]any{"protocolVersion": 1, "id": r.ID, "result": result}
		if e != nil {
			delete(reply, "result")
			reply["error"] = map[string]string{"code": "request_failed", "message": cr.SafeText(e.Error())}
		}
		if e = encoder.Encode(reply); e != nil {
			return e
		}
	}
	return s.Err()
}
