//go:build ignore

// Synthetic native CLI for the explicitly enabled tmux integration test.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	args := os.Args[1:]
	shared := os.Getenv("CODEX_SQLITE_HOME")
	home := os.Getenv("HOME")
	for _, a := range args {
		if a == "app-server" {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				var r struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				json.Unmarshal(scanner.Bytes(), &r)
				if len(r.ID) == 0 {
					continue
				}
				var result any = map[string]any{}
				switch r.Method {
				case "initialize":
					result = map[string]any{"userAgent": "fixture", "platformOs": "macos"}
				case "account/read":
					result = map[string]any{"account": map[string]any{"type": "chatgpt", "planType": "plus"}}
				case "account/rateLimits/read":
					used := 20
					if os.Getenv("CODEX_ACCOUNT") == "" {
						used = 96
					}
					if _, e := os.Stat(filepath.Join(home, "low-target")); e == nil && os.Getenv("CODEX_ACCOUNT") == "beta" {
						used = 95
					}
					result = map[string]any{"rateLimits": map[string]any{"primary": map[string]any{"usedPercent": used, "windowDurationMins": 300}, "secondary": map[string]any{"usedPercent": 20, "windowDurationMins": 10080}}}
				}
				json.NewEncoder(os.Stdout).Encode(map[string]any{"id": r.ID, "result": result})
			}
			return
		}
	}
	id := ""
	prompt := ""
	for i, a := range args {
		if a == "resume" && i+1 < len(args) {
			id = args[i+1]
			if i+2 < len(args) {
				prompt = args[i+2]
			}
		}
	}
	if id == "" {
		fmt.Println("codex-cli fixture")
		return
	}
	lockdir := filepath.Join(shared, "thread-writer-locks")
	os.MkdirAll(lockdir, 0700)
	f, e := os.OpenFile(filepath.Join(lockdir, id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		panic(e)
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		panic(e)
	}
	event := func(kind string) {
		f, e := os.OpenFile(filepath.Join(home, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			panic(e)
		}
		defer f.Close()
		json.NewEncoder(f).Encode(map[string]any{"event": kind, "account": os.Getenv("CODEX_ACCOUNT"), "thread": id, "prompt": prompt, "pid": os.Getpid()})
	}
	event("start")
	fmt.Println("fixture active", id)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	requested := false
	for {
		select {
		case <-signals:
			if _, e := os.Stat(filepath.Join(home, "ignore-term")); e == nil {
				continue
			}
			event("stop")
			return
		case <-ticker.C:
			if !requested && os.Getenv("CODEX_ACCOUNT") == "" {
				if _, e := os.Stat(filepath.Join(home, "switch-now")); e == nil {
					requested = true
					var params map[string]string
					b, _ := os.ReadFile(filepath.Join(home, "fixture-params.json"))
					json.Unmarshal(b, &params)
					cmd := exec.Command(params["binary"], "--config", params["config"], "handoff", id, "--account", "beta", "--if-low", "5")
					out, e := cmd.CombinedOutput()
					os.WriteFile(filepath.Join(home, "request-result"), []byte(strings.TrimSpace(string(out))+fmt.Sprint(e)), 0600)
					fmt.Println(string(out), e)
				}
			}
		}
	}
}
