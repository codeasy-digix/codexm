package nativecli

import (
	"context"
	"errors"
	"slices"
	"time"
)

type webModel struct {
	Model         string   `json:"model"`
	DisplayName   string   `json:"displayName"`
	IsDefault     bool     `json:"isDefault"`
	DefaultEffort string   `json:"defaultEffort"`
	Efforts       []string `json:"efforts"`
}

// Cache only the picker fields on this account's existing app-server connection.
// Selecting a model never starts a turn or changes a global preference.
func (c *webConnection) availableModels(ctx context.Context) ([]webModel, error) {
	c.mu.Lock()
	models, fresh := c.models, time.Since(c.modelsAt) < 2*time.Minute
	c.mu.Unlock()
	if fresh {
		return models, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	models = []webModel{}
	seenModels := map[string]bool{}
	seenCursors := map[string]bool{}
	args := map[string]any{"limit": 100, "includeHidden": false}
	for range 10 {
		var page struct {
			Data []struct {
				ID            string `json:"id"`
				Model         string `json:"model"`
				DisplayName   string `json:"displayName"`
				IsDefault     bool   `json:"isDefault"`
				DefaultEffort string `json:"defaultReasoningEffort"`
				Hidden        bool   `json:"hidden"`
				Efforts       []struct {
					Value string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			Next string `json:"nextCursor"`
		}
		if err := c.client.Call(ctx, "model/list", args, &page); err != nil {
			return nil, err
		}
		for _, entry := range page.Data {
			model := entry.Model
			if model == "" {
				model = entry.ID
			}
			if model == "" || entry.Hidden || seenModels[model] {
				continue
			}
			seenModels[model] = true
			option := webModel{Model: model, DisplayName: entry.DisplayName, IsDefault: entry.IsDefault, DefaultEffort: entry.DefaultEffort, Efforts: []string{}}
			for _, effort := range entry.Efforts {
				if effort.Value != "" && !slices.Contains(option.Efforts, effort.Value) {
					option.Efforts = append(option.Efforts, effort.Value)
				}
			}
			models = append(models, option)
		}
		if page.Next == "" {
			c.mu.Lock()
			c.models, c.modelsAt = models, time.Now()
			c.mu.Unlock()
			return models, nil
		}
		if seenCursors[page.Next] {
			break
		}
		seenCursors[page.Next] = true
		args["cursor"] = page.Next
	}
	return nil, errors.New("Codex did not finish listing the available models")
}

func (c *webConnection) validateTurnModel(ctx context.Context, model, effort string) error {
	models, err := c.availableModels(ctx)
	if err != nil {
		return errors.New("모델 목록을 확인할 수 없습니다. 다시 불러온 뒤 시도해 주세요.")
	}
	for _, option := range models {
		if option.Model == model {
			if effort == "" || slices.Contains(option.Efforts, effort) {
				return nil
			}
			return errors.New("선택한 모델에서 지원하는 effort를 골라 주세요.")
		}
	}
	return errors.New("이 계정의 Codex 목록에 있는 모델을 골라 주세요.")
}
