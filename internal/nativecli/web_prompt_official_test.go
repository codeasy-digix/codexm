package nativecli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebOnlyGoalControlsKeepScopeAndBudget(t *testing.T) {
	f := webOnlyFixture(t)
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"account": "alpha", "cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	source := w.connections["alpha"]
	const objective, budget = "GUI_FIXTURE_ACTIVE_TEST_GOAL", 30000
	if err := source.client.Call(f.ctx, "thread/goal/set", map[string]any{
		"threadId": id, "objective": objective, "status": "active", "origin": "user", "tokenBudget": budget,
	}, nil); err != nil {
		t.Fatal(err)
	}
	assertFixtureModelLogin(t, f, objective, "alpha")
	goal, err := source.client.Goal(f.ctx, id)
	if err != nil || goal == nil || goal.CreatedAt == 0 {
		t.Fatal("native goal reference unavailable", err)
	}
	change := func(mode, account string, switchAccount bool, createdAt int64) (map[string]any, error) {
		return call("web.goalChange", map[string]any{"thread_id": id, "account": account, "mode": mode,
			"switch_account": switchAccount, "expected_goal": webGoalRef{Objective: objective, CreatedAt: createdAt}})
	}
	if _, err := change("clear", "alpha", false, goal.CreatedAt-1); err == nil {
		t.Fatal("an old goal reference was accepted")
	}
	paused, err := change("pause", "alpha", false, goal.CreatedAt)
	if err != nil || paused["goal"].(map[string]any)["status"] != "paused" {
		t.Fatal("goal did not pause", paused, err)
	}
	resumed, err := change("resume", "beta", true, goal.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	value := resumed["goal"].(map[string]any)
	if value["status"] != "active" || value["objective"] != objective || value["tokenBudget"] != float64(budget) {
		t.Fatal("goal's objective or budget changed", resumed)
	}
	target := w.connections["beta"]
	if err := w.verifyOwner(f.ctx, target, id, true); err != nil {
		t.Fatal("resumed goal did not use the selected account", err)
	}
	if _, err := change("clear", "beta", false, goal.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if cleared, err := target.client.Goal(f.ctx, id); err != nil || cleared != nil {
		t.Fatal("the goal remained after confirmed clear", cleared, err)
	}
	if saved, err := target.client.Read(f.ctx, id, false); err != nil || saved.ID != id {
		t.Fatal("clearing the goal removed the chat", err)
	}
}

func TestWebOnlyPredictionReadsOfficialEvents(t *testing.T) {
	c := &webConnection{}
	const thread, turn = "00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000102"
	prediction := func(kind, text string) json.RawMessage {
		body, err := json.Marshal(map[string]any{"threadId": thread, "sourceTurnId": turn,
			"result": map[string]string{"type": kind, "text": text}})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	c.observePrediction(prediction("completed", "Next prompt from the official runtime"))
	if c.predictions[thread].SourceTurn != turn {
		t.Fatal("native prediction was not retained")
	}
	c.observePrediction(prediction("failed", ""))
	if _, ok := c.predictions[thread]; ok {
		t.Fatal("a failed prediction should not be displayed")
	}
}

func TestWebOnlyOfficialPromptGoalsAndSkills(t *testing.T) {
	f := webOnlyFixture(t)
	skillDir := filepath.Join(f.cmd.Dir, ".agents", "skills", "fixture-proof")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: fixture-proof\ndescription: Read this skill when explicitly named.\n---\n\nSKILL_FIXTURE_BODY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	newChat := func() string {
		t.Helper()
		result, err := call("web.create", map[string]any{"account": "alpha", "cwd": f.cmd.Dir})
		if err != nil {
			t.Fatal(err)
		}
		return result["thread"].(map[string]any)["id"].(string)
	}
	skillChat := newChat()
	args, _ := json.Marshal(map[string]any{"account": "alpha", "thread_id": skillChat})
	answer, err := w.Dispatch(f.ctx, Request{Version: 1, Method: "web.skills", Params: args})
	if err != nil {
		t.Fatal(err)
	}
	var skill webSkill
	for _, candidate := range answer.([]webSkill) {
		if candidate.Name == "fixture-proof" {
			skill = candidate
			break
		}
	}
	if len(skill.ID) != 64 {
		t.Fatal("official discovery did not expose the project skill")
	}
	b, _ := json.Marshal(skill)
	if strings.Contains(string(b), skillDir) {
		t.Fatal("the client received the private skill path")
	}
	run := func(id, prompt, mode string, skills []string) map[string]any {
		t.Helper()
		res, err := call("web.send", map[string]any{"thread_id": id, "account": "alpha", "text": prompt, "mode": mode, "skills": skills})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	run(skillChat, "GUI_FIXTURE_ACTIVE_SKILL_SELECTED", "immediate", []string{skill.ID})
	assert := func(parts ...string) {
		t.Helper()
		for until := time.Now().Add(7 * time.Second); time.Now().Before(until); {
			f.mu.Lock()
			for _, body := range f.bodies {
				matches := true
				for _, part := range parts {
					matches = matches && strings.Contains(body, part)
				}
				if matches {
					f.mu.Unlock()
					return
				}
			}
			f.mu.Unlock()
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("official model input did not contain %q", parts)
	}
	assert("GUI_FIXTURE_ACTIVE_SKILL_SELECTED", "$fixture-proof")
	goalChat := newChat()
	const objective = "GUI_FIXTURE_ACTIVE_EXPLICIT_WEB_GOAL"
	if r := run(goalChat, "/goal "+objective, "immediate", nil); r["mode"] != "goal" {
		t.Fatal("typed goal did not use the official goal endpoint", r)
	}
	assert(objective)
	c := w.connections["alpha"]
	current, err := c.client.Goal(f.ctx, goalChat)
	if err != nil || current == nil || current.Objective != objective || current.Status != "active" {
		t.Fatal("the official goal was not set", err)
	}
	history, err := call("web.history", map[string]any{"thread_id": goalChat, "account": "alpha"})
	shown, _ := history["goal"].(map[string]any)
	if err != nil || shown["status"] != "active" {
		t.Fatal("the web did not report the native goal", err)
	}
	const conflicting = "different test goal"
	_, err = call("web.send", map[string]any{"thread_id": goalChat, "account": "beta", "text": conflicting,
		"mode": "immediate", "goal": true})
	if err == nil || !strings.Contains(err.Error(), "완료되지 않은 목표") {
		t.Fatal("an account switch must preserve the existing goal", err)
	}
	if current, err := c.client.Goal(f.ctx, goalChat); err != nil || current == nil || current.Objective != objective || current.Status != "active" {
		t.Fatal("rejected goal replacement changed the existing execution", err)
	}
	mixedChat := newChat()
	const mixedGoal = "GUI_FIXTURE_ACTIVE_WEB_SKILL_GOAL"
	run(mixedChat, "/goal "+mixedGoal, "immediate", []string{skill.ID})
	assert(mixedGoal, "$fixture-proof")
	mixed, err := c.client.Goal(f.ctx, mixedChat)
	if err != nil || mixed == nil || mixed.Status != "active" {
		t.Fatal("goal with a selected skill did not activate", err)
	}
	invalid, err := call("web.send", map[string]any{"thread_id": skillChat, "account": "alpha", "text": "UNTRUSTED_SKILL",
		"mode": "immediate", "skills": []string{"/made-up/path/SKILL.md"}})
	if err == nil || invalid != nil {
		t.Fatal("the web bridge allowed an arbitrary skill path")
	}
}
