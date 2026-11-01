// Package webhost owns only a local web process and its bridge, never workers.
package webhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/webbundle"
	"golang.org/x/sys/unix"
)

type Spec struct {
	Session, Origin, Directory, Program, Config, Binary, State string
	Listen                                                     string
	Args                                                       []string
	PID                                                        int
}

func Command(ctx context.Context, c config.Config, self string, args []string, out io.Writer) error {
	action := "start"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	f := flag.NewFlagSet("web", flag.ContinueOnError)
	dev := f.String("dev", "", "Meteor web directory for development")
	bundle := f.String("bundle", "", "built Meteor bundle directory")
	port := f.Int("port", 4317, "loopback port")
	noOpen := f.Bool("no-open", false, "print the local URL without opening a browser")
	publicOrigin := f.String("public-origin", "", "HTTPS origin served by a loopback reverse proxy")
	username := f.String("username", "", "web login username (web user)")
	passwordStdin := f.Bool("password-stdin", false, "read web password from stdin (web user)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected web argument")
	}
	if *bundle == "" {
		*bundle = filepath.Join(filepath.Dir(self), "web", "bundle")
	}
	state := filepath.Join(c.StateDir(), "web")
	if action == "user" {
		return configureLogin(state, *username, *passwordStdin, out)
	}
	if *username != "" || *passwordStdin {
		return errors.New("login options require codexm web user")
	}
	path := filepath.Join(state, "launch.json")
	var spec Spec
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &spec)
	alive := spec.Session != "" && exec.CommandContext(ctx, "tmux", "has-session", "-t", "="+spec.Session).Run() == nil
	switch action {
	case "verify":
		m, err := webbundle.Verify(config.Expand(*bundle), true)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(m)
	case "status":
		return json.NewEncoder(out).Encode(map[string]any{"running": alive && ready(state, spec.Origin), "starting": alive && !ready(state, spec.Origin), "url": spec.Origin, "session": spec.Session, "state": state})
	case "logs":
		b, err := os.ReadFile(filepath.Join(state, "web.log"))
		if err != nil {
			return err
		}
		if len(b) > 16000 {
			b = b[len(b)-16000:]
		}
		_, err = out.Write(b)
		return err
	case "stop":
		if !alive {
			fmt.Fprintln(out, "Local web is stopped.")
			return nil
		}
		if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", spec.Session+":", "C-c").Run(); err != nil {
			return err
		}
		for range 100 {
			if exec.CommandContext(ctx, "tmux", "has-session", "-t", "="+spec.Session).Run() != nil {
				fmt.Fprintln(out, "Local web and bridge stopped. Native CLI terminals keep running.")
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return errors.New("web shutdown did not finish; inspect its named tmux session")
	case "start":
	default:
		return errors.New("usage: codexm web [status|stop|logs|user] [--public-origin https://HOST] [--no-open]")
	}
	if alive {
		if !ready(state, spec.Origin) {
			fmt.Fprintln(out, "Web is starting; inspect codexm web logs and retry when ready.")
			return nil
		}
		return open(ctx, state, spec.Origin, *noOpen, out)
	}
	if *port < 1024 || *port > 65532 {
		return errors.New("web port must be 1024–65532")
	}
	if err := os.MkdirAll(state, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(state, "launcher.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("web launch already in progress")
	}
	address := "127.0.0.1:" + strconv.Itoa(*port)
	probe, err := net.Listen("tcp", address)
	if err != nil {
		return errors.New("local web port is already in use")
	}
	probe.Close()
	spec = Spec{Session: "codexm-web", Origin: "http://" + address, Config: c.Path, Binary: self, State: state}
	spec.Listen = address
	if *publicOrigin != "" {
		spec.Origin, err = httpsOrigin(*publicOrigin)
		if err != nil {
			return err
		}
		if _, err = os.Stat(filepath.Join(state, "login.json")); err != nil {
			return errors.New("configure codexm web user before enabling a public origin")
		}
	}
	if *dev != "" {
		spec.Session = "codexm-web-dev"
		spec.Directory = config.Expand(*dev)
		spec.Program, err = exec.LookPath("meteor")
		spec.Args = []string{"run", "--port", address, "--app-port", "127.0.0.1:" + strconv.Itoa(*port+2)}
	} else {
		spec.Directory = config.Expand(*bundle)
		spec.Args = []string{"main.js"}
	}
	if err != nil {
		return err
	}
	if !filepath.IsAbs(spec.Directory) {
		return errors.New("web directory must be absolute")
	}
	required := "main.js"
	if *dev != "" {
		required = filepath.Join(".meteor", "release")
	}
	if _, err = os.Stat(filepath.Join(spec.Directory, required)); err != nil {
		return fmt.Errorf("web bundle is missing; build/install it or use --dev /absolute/path/to/web: %w", err)
	}
	if *dev == "" {
		if _, err = webbundle.Verify(spec.Directory, false); err != nil {
			return err
		}
		spec.Program, err = nodeProgram(ctx, c)
		if err != nil {
			return err
		}
	}
	if exec.CommandContext(ctx, "tmux", "has-session", "-t", "="+spec.Session).Run() == nil {
		return errors.New("named web session belongs to another configuration")
	}
	b, _ = json.Marshal(spec)
	if err = config.AtomicWrite(path, b, 0600); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(state, "ready.json"))
	command := cr.ShellQuote(self) + " --config " + cr.ShellQuote(c.Path) + " __web " + cr.ShellQuote(path)
	if err = exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", spec.Session, command).Run(); err != nil {
		return err
	}
	fmt.Fprintf(out, "tmux attach -t %s\n", spec.Session)
	for range 600 {
		if ready(state, spec.Origin) {
			return open(ctx, state, spec.Origin, *noOpen, out)
		}
		if exec.CommandContext(ctx, "tmux", "has-session", "-t", "="+spec.Session).Run() != nil {
			return errors.New("web exited during startup; inspect codexm web logs")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	fmt.Fprintf(out, "Building; check codexm web status and logs. URL: %s\n", spec.Origin)
	return nil
}

func nodeProgram(ctx context.Context, c config.Config) (string, error) {
	name := "node"
	if c.Runtime.WebNodeBinary != "" {
		name = config.Expand(c.Runtime.WebNodeBinary)
		if !filepath.IsAbs(name) {
			return "", errors.New("web Node binary must be an absolute path")
		}
	}
	program, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	version, err := exec.CommandContext(ctx, program, "--version").Output()
	if err != nil || !strings.HasPrefix(string(version), "v24.") {
		return "", errors.New("local web requires Node 24; set runtime.web_node_binary to its absolute path")
	}
	return program, nil
}

func ready(state, origin string) bool {
	var status struct {
		PID    int    `json:"pid"`
		Origin string `json:"origin"`
	}
	b, err := os.ReadFile(filepath.Join(state, "ready.json"))
	if err != nil || json.Unmarshal(b, &status) != nil || status.PID <= 0 || status.Origin != origin || syscall.Kill(status.PID, 0) != nil {
		return false
	}
	var spec Spec
	b, err = os.ReadFile(filepath.Join(state, "launch.json"))
	if err != nil || json.Unmarshal(b, &spec) != nil {
		return false
	}
	address, err := listenAddress(spec)
	if err != nil {
		return false
	}
	conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
func open(ctx context.Context, state, origin string, noOpen bool, out io.Writer) error {
	if _, err := os.Stat(filepath.Join(state, "login.json")); err == nil {
		fmt.Fprintln(out, origin)
		if noOpen {
			return nil
		}
		return openBrowser(ctx, origin)
	}
	// Each local invocation grants one new browser session. Existing cookies
	// remain valid; a consumed code must not strand a second browser.
	code := make([]byte, 32)
	if _, err := rand.Read(code); err != nil {
		return err
	}
	token := hex.EncodeToString(code)
	if err := config.AtomicWrite(filepath.Join(state, "access"), []byte(token+"\n"), 0600); err != nil {
		return err
	}
	fmt.Fprintln(out, origin)
	if noOpen {
		fmt.Fprintf(out, "Local access code file: %s\n", filepath.Join(state, "access"))
		return nil
	}
	return openBrowser(ctx, origin+"#"+token)
}
func openBrowser(ctx context.Context, target string) error {
	opener := "open"
	if _, err := exec.LookPath(opener); err != nil {
		opener = "xdg-open"
	}
	return exec.CommandContext(ctx, opener, target).Run()
}
func Run(ctx context.Context, c config.Config, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s Spec
	if err = json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s.Config != c.Path || s.State != filepath.Join(c.StateDir(), "web") {
		return errors.New("web launch configuration mismatch")
	}
	address, err := listenAddress(s)
	if err != nil {
		return err
	}
	_, port, _ := net.SplitHostPort(address)
	lock, err := os.OpenFile(filepath.Join(s.State, "process.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("another web process owns this configuration")
	}
	log, err := os.OpenFile(filepath.Join(s.State, "web.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(s.Program, s.Args...)
	cmd.Dir = s.Directory
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "BIND_IP=127.0.0.1", "ROOT_URL="+s.Origin, "PORT="+port, "CODEXM_BINARY="+s.Binary, "CODEXM_CONFIG="+s.Config, "CODEXM_WEB_STATE="+s.State, "DO_NOT_TRACK=1", "RSPACK_DEVSERVER_PORT="+strconv.Itoa(func() int { p, _ := strconv.Atoi(port); return p + 3 }()))
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case err = <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			err = <-done
		}
	}
	_ = os.Remove(filepath.Join(s.State, "ready.json"))
	return err
}

func httpsOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("public origin must be an HTTPS origin without credentials, paths or query strings")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid HTTPS port")
		}
	}
	return "https://" + strings.ToLower(strings.TrimSuffix(u.Host, ":443")), nil
}
func listenAddress(s Spec) (string, error) {
	address := s.Listen
	if address == "" {
		address = strings.TrimPrefix(s.Origin, "http://")
	}
	host, port, err := net.SplitHostPort(address)
	n, numberErr := strconv.Atoi(port)
	if err != nil || host != "127.0.0.1" || numberErr != nil || n < 1024 || n > 65532 {
		return "", errors.New("Meteor listener must remain on 127.0.0.1:1024–65532")
	}
	if s.Origin != "http://"+address {
		if _, err := httpsOrigin(s.Origin); err != nil {
			return "", err
		}
	}
	return address, nil
}
