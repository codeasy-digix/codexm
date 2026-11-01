package nativecli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

type loginBroker interface {
	Start(context.Context, string, string) (accounts.LoginStatus, error)
	Status(string) (accounts.LoginStatus, error)
	Cancel(context.Context, string) (accounts.LoginStatus, error)
	Close()
}
type loginOperation struct {
	Fingerprint [32]byte
	ID          string
}
type loginSession struct {
	ctx        context.Context
	broker     loginBroker
	owners     map[string]string
	latest     map[string]string
	operations map[string]loginOperation
	newBroker  func(context.Context, config.Config) loginBroker
}

func newLoginSession(ctx context.Context) *loginSession {
	return &loginSession{ctx: ctx, owners: map[string]string{}, latest: map[string]string{}, operations: map[string]loginOperation{}, newBroker: func(ctx context.Context, c config.Config) loginBroker { return accounts.NewNativeLoginBroker(ctx, c) }}
}
func (s *loginSession) Close() {
	if s.broker != nil {
		s.broker.Close()
	}
}
func (s *loginSession) Dispatch(c config.Config, r Request) (any, error) {
	if r.Version != 1 || r.Owner == "" {
		return nil, errors.New("authenticated browser required")
	}
	var p Params
	if err := json.Unmarshal(r.Params, &p); err != nil {
		return nil, err
	}
	if r.Method == "login.start" {
		if len(s.operations) >= 256 {
			return nil, errors.New("too many sign-in attempts; restart the web before trying again")
		}
		fingerprint := sha256.Sum256(r.Params)
		key := r.Owner + "\x00" + r.Operation
		if old, ok := s.operations[key]; ok {
			if old.Fingerprint != fingerprint {
				return nil, errors.New("sign-in operation id reused")
			}
			return s.broker.Status(old.ID)
		}
		return mutate(c, r, func() (any, error) {
			if s.broker == nil {
				resolver, err := Resolver(c)
				if err != nil {
					return nil, err
				}
				c.Runtime.CodexBinary = resolver.Binary
				s.broker = s.newBroker(s.ctx, c)
			}
			result, err := s.broker.Start(s.ctx, p.Account, p.Mode)
			if err == nil {
				s.owners[result.LoginID] = r.Owner
				s.latest[r.Owner] = result.LoginID
				s.operations[key] = loginOperation{fingerprint, result.LoginID}
			}
			return result, err
		}, false) // One-time auth URLs/codes never enter durable operation receipts.
	}
	id := p.LoginID
	if id == "" {
		id = s.latest[r.Owner]
	}
	if id == "" && r.Method == "login.status" {
		return accounts.LoginStatus{Status: "idle"}, nil
	}
	if s.broker == nil || s.owners[id] != r.Owner {
		return nil, errors.New("sign-in session does not belong to this browser")
	}
	if r.Method == "login.status" {
		return s.broker.Status(id)
	}
	if r.Method == "login.cancel" {
		return mutate(c, r, func() (any, error) { return s.broker.Cancel(s.ctx, id) }, false)
	}
	return nil, errors.New("unsupported login method")
}

func requireRegisteredAccount(c config.Config, name string) error {
	if name == "default" || !config.Nickname.MatchString(name) {
		return errors.New("select a registered named account")
	}
	all, err := Accounts(c)
	if err != nil {
		return err
	}
	for _, a := range all {
		if a.Name == name {
			return nil
		}
	}
	return errors.New("account is no longer registered; refresh the list")
}
