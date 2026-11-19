package nativecli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

var leadingGoal = regexp.MustCompile(`^/goal(?:[\t\r\n ]+|$)`)

type webSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope,omitempty"`
	path        string
}

func (w *WebChat) listPromptSkills(ctx context.Context, c *webConnection, id string) ([]webSkill, error) {
	if !w.webOnly {
		return []webSkill{}, nil
	}
	thread, err := w.thread(ctx, id)
	if err != nil || thread == nil || thread.Cwd == "" {
		return nil, errors.New("이 채팅의 작업 폴더를 확인할 수 없습니다.")
	}
	var response struct {
		Data []struct {
			Skills []struct {
				Name, Path, Description, ShortDescription, Scope string
				Enabled                                          bool
			}
		}
	}
	if err := c.client.Call(ctx, "skills/list", map[string]any{"cwds": []string{thread.Cwd}}, &response); err != nil {
		return nil, err
	}
	result := []webSkill{}
	seen := map[string]bool{}
	for _, project := range response.Data {
		for _, skill := range project.Skills {
			if !skill.Enabled || skill.Name == "" || skill.Path == "" {
				continue
			}
			id := fmt.Sprintf("%x", sha256.Sum256([]byte(skill.Name+"\x00"+skill.Path)))
			if seen[id] {
				continue
			}
			seen[id] = true
			description := skill.ShortDescription
			if description == "" {
				description = skill.Description
			}
			result = append(result, webSkill{ID: id, Name: skill.Name, Description: description, Scope: skill.Scope, path: skill.Path})
		}
	}
	return result, nil
}

func (w *WebChat) preparePrompt(ctx context.Context, c *webConnection, p *webParams) error {
	if len(p.SkillIDs) == 0 {
		return nil
	}
	skills, err := w.listPromptSkills(ctx, c, p.Thread)
	if err != nil {
		return err
	}
	available := make(map[string]webSkill, len(skills))
	for _, skill := range skills {
		available[skill.ID] = skill
	}
	var mentions []string
	var parts []map[string]any
	p.selectedSkills = nil
	for _, id := range p.SkillIDs {
		skill, ok := available[id]
		if !ok {
			return errors.New("선택한 계정이나 프로젝트에 이 스킬이 없습니다. 스킬을 다시 선택해 주세요.")
		}
		delete(available, id)
		mentions = append(mentions, "$"+skill.Name)
		parts = append(parts, map[string]any{"type": "skill", "name": skill.Name, "path": skill.path})
		// The display copy is used in the web queue, so discard its path and description.
		p.selectedSkills = append(p.selectedSkills, webSkill{ID: id, Name: skill.Name})
	}
	p.nativeInput = append(cr.TextInput(p.Text+"\n\n"+strings.Join(mentions, " ")), parts...)
	return nil
}

func promptInput(p webParams) []map[string]any {
	if p.nativeInput != nil {
		return p.nativeInput
	}
	return cr.TextInput(p.Text)
}

func normalizeWebPrompt(p *webParams) error {
	if leadingGoal.MatchString(p.Text) {
		p.Goal = true
		p.Text = strings.TrimSpace(leadingGoal.ReplaceAllString(p.Text, ""))
	}
	if strings.TrimSpace(p.Text) == "" {
		if p.Goal {
			return errors.New("목표로 진행할 내용을 입력해 주세요.")
		}
		return errors.New("enter a message")
	}
	if len(p.SkillIDs) > 16 {
		return errors.New("한 입력에 스킬을 16개까지 적용할 수 있습니다.")
	}
	if p.Goal && utf8.RuneCountInString(strings.TrimSpace(p.Text)) > 4000 {
		return errors.New("목표 내용은 4,000자까지 입력할 수 있습니다.")
	}
	return nil
}

func goalObjective(text string, goal bool) string {
	if goal {
		return strings.TrimSpace(text)
	}
	return ""
}

func readGoalForChange(ctx context.Context, client *cr.Client, id, objective string) (*jobs.Goal, error) {
	current, err := client.Goal(ctx, id)
	if err != nil {
		return nil, err
	}
	if current != nil && current.Objective != objective && current.Status != "complete" {
		return nil, errors.New("이 채팅에 완료되지 않은 목표가 있습니다. 현재 목표의 내용을 확인해 주세요.")
	}
	return current, nil
}

// Activating a goal starts execution through the official server. Only inputs
// that also specify skills or turn settings need an explicit first turn.
func (w *WebChat) deliverGoal(ctx context.Context, owner *webInputOwner, selected *webConnection, request Request, p webParams) (any, error) {
	if err := w.verifyInputOwner(ctx, owner, p.Thread, owner.web == selected); err != nil {
		return nil, err
	}
	if err := w.verifyLogin(ctx, owner.web); err != nil {
		return nil, err
	}
	objective := strings.TrimSpace(p.Text)
	current, err := readGoalForChange(ctx, owner.client, p.Thread, objective)
	if err != nil {
		return nil, err
	}
	extraInput := len(p.SkillIDs) != 0 || p.Model != "" || p.Effort != ""
	status := "active"
	if extraInput && (current == nil || current.Status != "active") {
		status = "paused"
	}
	var reply struct{ Goal *jobs.Goal }
	params := map[string]any{"threadId": p.Thread, "objective": objective, "status": status, "origin": "user"}
	if err := callWebInput(ctx, owner.client, "thread/goal/set", params, &reply); err != nil {
		return nil, err
	}
	if reply.Goal == nil || reply.Goal.Objective != objective || reply.Goal.Status != status {
		return nil, errWebInputUnconfirmed
	}
	w.rememberAccount(p.Thread, owner.web.account)
	if !extraInput {
		return map[string]any{"thread_id": p.Thread, "mode": "goal", "status": "accepted"}, nil
	}
	result, err := w.deliverWebInput(ctx, owner, selected, request, p)
	if err != nil {
		return nil, err
	}
	if status == "paused" {
		if err := owner.client.SetGoalStatus(ctx, p.Thread, "active", "user"); err != nil {
			// Input was acknowledged. Report the goal separately without replaying it.
			result.(map[string]any)["goal_activation_unconfirmed"] = true
			return result, nil
		}
	}
	return result, nil
}
