package config

import (
	"errors"
	"slices"
)

// RegisterAccount restores an explicitly removed alias and preserves the order
// of all existing workers. The native login and named home are separate state.
func (c *Config) RestoreAccountRegistration(name string) error {
	if !Nickname.MatchString(name) || name == "default" {
		return errors.New("invalid account nickname")
	}
	c.AccountRegistry.Removed = slices.DeleteFunc(c.AccountRegistry.Removed, func(n string) bool { return n == name })
	return nil
}

// RegisterAccount is retained for explicitly configured legacy worker policies.
// Compact native sign-in only restores registration, never worker priority.
func (c *Config) RegisterAccount(name string) error {
	if err := c.RestoreAccountRegistration(name); err != nil {
		return err
	}
	if !slices.Contains(c.WorkerAccounts.Priority, name) {
		position := len(c.WorkerAccounts.Priority)
		if fallback := slices.Index(c.WorkerAccounts.Priority, c.WorkerAccounts.FallbackAccount); fallback >= 0 {
			position = fallback
		}
		c.WorkerAccounts.Priority = slices.Insert(c.WorkerAccounts.Priority, position, name)
	}
	return nil
}
