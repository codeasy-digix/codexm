package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Forms only edit local drafts. Callers decide when to submit an action.
type formOption struct{ Value, Label string }
type formField struct {
	Key, Label, Kind, Value, Help string
	Options                       []formOption
	Required                      bool
}
type formAction struct{ Key, Label string }
type formLayout struct{ Width, Height, Lines int }
type formState struct {
	Title        string
	Fields       []formField
	Actions      []formAction
	Index        int
	Editing      bool
	Error        string
	editor       textarea.Model
	before       string
	scroll       int
	manualScroll bool
	// View receivers copy the model; shared layout metrics keep scrolling bounded.
	layout *formLayout
}

func newForm(title string, fields []formField, actions []formAction) formState {
	editor := textarea.New()
	editor.ShowLineNumbers = false
	editor.Prompt = ""
	editor.CharLimit = 65536
	editor.SetHeight(1)
	return formState{Title: title, Fields: fields, Actions: actions, editor: editor, layout: &formLayout{Width: 80, Height: 10}}
}

func (f *formState) scrollBy(delta int, dimensions ...int) {
	f.manualScroll = true
	if len(dimensions) >= 2 {
		f.render(dimensions[0], dimensions[1], "")
	}
	limit := 0
	if f.layout != nil {
		limit = max(0, f.layout.Lines-f.layout.Height)
	}
	f.scroll = max(0, min(f.scroll, limit))
	f.scroll = max(0, min(f.scroll+delta, limit))
}

func (f *formState) value(key string) string {
	for i := range f.Fields {
		if f.Fields[i].Key == key {
			if f.Editing && f.Index == i {
				return f.editor.Value()
			}
			return f.Fields[i].Value
		}
	}
	return ""
}

func (f *formState) setValue(key, value string) {
	for i := range f.Fields {
		if f.Fields[i].Key == key {
			f.Fields[i].Value = value
			if f.Editing && f.Index == i {
				f.editor.SetValue(value)
			}
			return
		}
	}
}

func (f *formState) commit() {
	if f.Editing && f.Index >= 0 && f.Index < len(f.Fields) {
		f.Fields[f.Index].Value = f.editor.Value()
	}
	f.Editing = false
	f.editor.Blur()
}

func (f *formState) validate() bool {
	f.commit()
	for i, field := range f.Fields {
		if field.Required && strings.TrimSpace(field.Value) == "" {
			f.Index, f.Error = i, field.Label+"을 입력하세요."
			return false
		}
		if field.Kind == "number" && field.Value != "" {
			if _, err := strconv.ParseInt(field.Value, 10, 64); err != nil {
				f.Index, f.Error = i, field.Label+"에 정수를 입력하세요."
				return false
			}
		}
	}
	f.Error = ""
	return true
}

func (f *formState) cycle(delta int) {
	if f.Index < 0 || f.Index >= len(f.Fields) {
		return
	}
	field := &f.Fields[f.Index]
	if field.Kind == "toggle" {
		field.Value = strconv.FormatBool(field.Value != "true")
		return
	}
	if len(field.Options) == 0 {
		return
	}
	index := -1
	for i, option := range field.Options {
		if option.Value == field.Value {
			index = i
			break
		}
	}
	if index == -1 {
		index = 0
	} else {
		index = (index + delta + len(field.Options)) % len(field.Options)
	}
	field.Value = field.Options[index].Value
}

// activate accepts a field/action index, including indices from rendered hits.
func (f *formState) activate(index int) string {
	f.commit()
	count := len(f.Fields) + len(f.Actions)
	if index < 0 || index >= count {
		return ""
	}
	f.Index = index
	f.manualScroll = false
	if index >= len(f.Fields) {
		return f.Actions[index-len(f.Fields)].Key
	}
	field := &f.Fields[index]
	if field.Kind == "info" {
		return ""
	}
	if field.Kind == "select" || field.Kind == "toggle" {
		f.cycle(1)
		return ""
	}
	f.Editing, f.before = true, field.Value
	f.editor.SetValue(field.Value)
	f.editor.CursorEnd()
	f.editor.SetHeight(1)
	if field.Kind == "multiline" {
		f.editor.SetHeight(3)
	}
	f.editor.Focus()
	return ""
}

func (f *formState) update(msg tea.Msg) (string, tea.Cmd, bool) {
	switch msg.(type) {
	case tea.PasteMsg, cursor.BlinkMsg, tea.FocusMsg, tea.BlurMsg:
		if f.Editing {
			var cmd tea.Cmd
			f.editor, cmd = f.editor.Update(msg)
			return "", cmd, true
		}
		_, paste := msg.(tea.PasteMsg)
		return "", nil, paste
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return "", nil, false
	}
	k := key.String()
	if k == "ctrl+c" {
		return "", nil, false
	}
	if k == "ctrl+s" {
		f.commit()
		return "save", nil, true
	}
	if k == "esc" {
		if f.Editing {
			f.editor.SetValue(f.before)
			f.commit()
			return "", nil, true
		}
		return "cancel", nil, true
	}
	count := len(f.Fields) + len(f.Actions)
	if k == "tab" || k == "shift+tab" {
		f.commit()
		f.manualScroll = false
		delta := 1
		if k == "shift+tab" {
			delta = -1
		}
		if count > 0 {
			f.Index = (f.Index + delta + count) % count
		}
		return "", nil, true
	}
	if !f.Editing && (k == "pgup" || k == "pgdown" || k == "home" || k == "end") {
		f.manualScroll = true
		switch k {
		case "pgup":
			f.scrollBy(-5)
		case "pgdown":
			f.scrollBy(5)
		case "home":
			f.scroll = 0
		case "end":
			f.scrollBy(1 << 20)
		}
		return "", nil, true
	}
	if f.Editing {
		multiline := f.Fields[f.Index].Kind == "multiline"
		if k == "ctrl+enter" || k == "enter" && !multiline {
			f.commit()
			return "", nil, true
		}
		if multiline && (k == "alt+enter" || k == "shift+enter") {
			f.editor.InsertString("\n")
			return "", nil, true
		}
		var cmd tea.Cmd
		f.editor, cmd = f.editor.Update(msg)
		return "", cmd, true
	}
	switch k {
	case "up", "down":
		f.manualScroll = false
		delta := 1
		if k == "up" {
			delta = -1
		}
		if count > 0 {
			f.Index = (f.Index + delta + count) % count
		}
	case "left", "right", " ", "space":
		delta := 1
		if k == "left" {
			delta = -1
		}
		f.cycle(delta)
	case "enter":
		return f.activate(f.Index), nil, true
	default:
		return "", nil, true
	}
	return "", nil, true
}

type formLine struct {
	Text   string
	Index  int
	Action string
}

// clippedLines keeps the selected control in view even on small terminals.
func clippedLines(lines []formLine, width, height, selected int, scroll *int) (string, []hitRegion) {
	width, height = max(1, width), max(1, height)
	start, end := -1, -1
	for i, line := range lines {
		if line.Index == selected {
			if start < 0 {
				start = i
			}
			end = i
		}
	}
	*scroll = max(0, min(*scroll, max(0, len(lines)-height)))
	if start >= 0 {
		if start < *scroll {
			*scroll = start
		}
		if end-start+1 > height {
			*scroll = start
		} else if end >= *scroll+height {
			*scroll = max(start, end-height+1)
		}
	}
	var text []string
	var hits []hitRegion
	for i := *scroll; i < len(lines) && len(text) < height; i++ {
		line := lines[i]
		y := len(text)
		text = append(text, ansi.Truncate(line.Text, width, ""))
		if line.Action != "" {
			hits = append(hits, hitRegion{X: 0, Y: y, W: width, H: 1, Action: line.Action})
		}
	}
	return strings.Join(text, "\n"), hits
}

func appendFormLine(lines []formLine, text string, width, index int, action string) []formLine {
	for _, line := range strings.Split(ansi.Wrap(plain(text), max(1, width), ""), "\n") {
		lines = append(lines, formLine{Text: line, Index: index, Action: action})
	}
	return lines
}

func (f *formState) render(width, height int, prefix string) (string, []hitRegion) {
	width = max(1, width)
	lines := appendFormLine(nil, f.Title, width, -1, "")
	lines = appendFormLine(lines, "Tab 이동 · Enter 편집/선택 · Ctrl+S 저장 · PgUp/PgDn 스크롤 · Esc 취소", width, -1, "")
	if f.Error != "" {
		lines = appendFormLine(lines, f.Error, width, -1, "")
	}
	for i, field := range f.Fields {
		marker := "  "
		if i == f.Index {
			marker = "› "
		}
		value := field.Value
		if field.Kind == "select" {
			for _, option := range field.Options {
				if option.Value == value {
					value = option.Label
				}
			}
		}
		if field.Kind == "toggle" {
			value = "[ ]"
			if field.Value == "true" {
				value = "[✓]"
			}
		}
		if field.Kind != "info" {
			value = strings.ReplaceAll(value, "\n", " ↵ ")
		}
		if value == "" {
			value = "(비어 있음)"
		}
		action := fmt.Sprintf("%s%d", prefix, i)
		if field.Kind == "info" {
			lines = appendFormLine(lines, marker+field.Label, width, i, action)
			lines = appendFormLine(lines, value, width, i, action)
		} else {
			lines = appendFormLine(lines, marker+field.Label+": "+value, width, i, action)
		}
		if f.Editing && f.Index == i {
			f.editor.SetWidth(max(1, width-2))
			for _, line := range strings.Split(f.editor.View(), "\n") {
				lines = append(lines, formLine{Text: line, Index: i, Action: action})
			}
			guide := "Enter 적용 · Esc 되돌리기"
			if field.Kind == "multiline" {
				guide = "Enter 줄바꿈 · Ctrl+Enter 적용 · Esc 되돌리기"
			}
			lines = appendFormLine(lines, guide, width, i, action)
		} else if field.Help != "" {
			lines = appendFormLine(lines, "  "+field.Help, width, i, action)
		}
		if field.Kind == "select" && f.Index == i {
			lines = appendFormLine(lines, "  ←→ 또는 Enter: 선택 변경", width, i, action)
		}
	}
	for i, action := range f.Actions {
		index := len(f.Fields) + i
		marker := "  "
		if index == f.Index {
			marker = "› "
		}
		lines = appendFormLine(lines, marker+"[ "+action.Label+" ]", width, index, fmt.Sprintf("%s%d", prefix, index))
	}
	selected := f.Index
	if f.manualScroll {
		selected = -2
	}
	if f.layout != nil {
		*f.layout = formLayout{Width: width, Height: max(1, height), Lines: len(lines)}
	}
	return clippedLines(lines, width, height, selected, &f.scroll)
}
