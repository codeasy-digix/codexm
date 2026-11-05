package nativecli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var viewerSessionName = regexp.MustCompile(`^codexm-view-[a-f0-9]{16}$`)

func newViewerName() (string, error) {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return "codexm-view-" + hex.EncodeToString(id[:]), nil
}

// A pane ID alone lets tmux choose any session containing its window. Always
// anchor session-sensitive reads to the durable container, never a viewer.
func paneTarget(l Launch) string {
	if l.Window != "" {
		return "=" + containerSession(l) + ":" + l.Window + "." + l.Pane
	}
	return "=" + containerSession(l) + ":." + l.Pane
}

func viewerTarget(viewer string, l Launch) string {
	return viewer + ":" + l.Window + "." + l.Pane
}

// new-session opens the terminal BEFORE creating the group member. A failed
// noninteractive attach therefore cannot leave a detached viewer behind.
// Arm cleanup only after attachment: setting destroy-unattached on a detached
// session can destroy it before a subsequent attach can run.
func viewerAttachArgs(name string, l Launch) []string {
	target := "=" + name
	return []string{
		"new-session", "-s", name, "-t", WorkspaceSession,
		";", "set-option", "-t", name, "destroy-unattached", "on",
		";", "set-option", "-t", name, "detach-on-destroy", "on",
		";", "select-window", "-t", target + ":" + l.Window,
		";", "select-pane", "-t", viewerTarget(target, l),
	}
}

// The pane's TMUX environment does not identify which client supplied a key.
// Never let tmux guess a different, recently active client in a shared session.
func invokingClient(ctx context.Context, pane string) (string, error) {
	if !regexp.MustCompile(`^%[0-9]+$`).MatchString(pane) {
		return "", errors.New("cannot identify the invoking tmux pane; reconnect from a separate terminal")
	}
	clients, err := tmux(ctx, CodexTmuxServer, "list-clients", "-F", "#{client_name}|#{pane_id}")
	if err != nil {
		return "", err
	}
	var selected string
	for _, line := range strings.Split(strings.TrimSpace(clients), "\n") {
		client, current, ok := strings.Cut(line, "|")
		if !ok || current != pane || client == "" {
			continue
		}
		if selected != "" {
			return "", errors.New("multiple tmux clients are viewing this picker; reconnect from a separate terminal so only that terminal changes chats")
		}
		selected = client
	}
	if selected == "" {
		return "", errors.New("no attached tmux client is viewing this picker; the CLI remains running, reconnect from a separate terminal")
	}
	return selected, nil
}

// Switching from inside tmux must preserve the client's original PTY. This
// bounded operation creates a group member viewing the existing native CLI.
// The server reaps it when its last client leaves; failure also reaps any member
// we created but could not attach. The workspace and its windows are untouched.
func switchClientToViewer(ctx context.Context, client string, l Launch) error {
	name, err := newViewerName()
	if err != nil {
		return err
	}
	id, err := tmux(ctx, CodexTmuxServer, "new-session", "-d", "-P", "-F", "#{session_id}", "-s", name, "-t", WorkspaceSession)
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if !regexp.MustCompile(`^\$[0-9]+$`).MatchString(id) {
		return errors.New("tmux did not identify the new viewer")
	}
	// Use the never-reused server session ID, not a possibly renamed session.
	defer func() {
		_, _ = tmux(context.Background(), CodexTmuxServer, "set-option", "-t", id, "destroy-unattached", "on")
	}()
	_, err = tmux(ctx, CodexTmuxServer,
		"switch-client", "-c", client, "-t", viewerTarget(id, l),
		";", "set-option", "-t", id, "destroy-unattached", "on",
		";", "set-option", "-t", id, "detach-on-destroy", "on")
	if err != nil {
		return fmt.Errorf("connect independent tmux viewer: %w", err)
	}
	return nil
}
