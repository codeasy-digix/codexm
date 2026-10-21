// Package application shares management operations across local frontends.
package application

import (
	"context"
	"errors"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/service"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func Start(ctx context.Context, c config.Config, self, account string) error {
	c, e := config.Load(c.Path)
	if e != nil {
		return e
	}
	if c.Manager.Account == "" {
		if !config.Nickname.MatchString(account) {
			return errors.New("select a saved manager account first")
		}
		r, e := cr.Resolve(ctx, c)
		if e != nil {
			return e
		}
		snap := accounts.New(r).Read(ctx, account)
		if !snap.Authenticated || snap.Error != "" {
			return errors.New("manager login could not be verified")
		}
		c, _, e = config.Update(c.Path, "", func(c *config.Config) error {
			if c.Manager.Account != "" {
				return errors.New("manager was selected elsewhere; refresh")
			}
			c.Manager.Account = account
			return nil
		})
		if e != nil {
			return e
		}
	}
	if supervisor.Ping(ctx, c) != nil {
		svc := service.Manager{Config: c, Self: self}
		if svc.Registered() {
			e = svc.Start(ctx)
		} else {
			e = svc.Install(ctx, true)
		}
		if e != nil {
			return e
		}
		if e = service.WaitReady(ctx, func(ctx context.Context) error { return supervisor.Ping(ctx, c) }); e != nil {
			return e
		}
	}
	return nil
}
