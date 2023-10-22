// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package ui

import (
	"fmt"
	"strings"

	"github.com/derailed/k9s/internal/config"
	"github.com/derailed/k9s/internal/model"
	"github.com/derailed/tcell/v2"
	"github.com/derailed/tview"
)

const (
	defaultPrompt      = "%c> [::b]%s"
	searchPrompt       = "%c(search)> [::b]%s"
	defaultSpacer      = 4
	searchSpacer       = 12
	defaultSuggestMode = model.SuggestAutoComplete
)

var (
	_ PromptModel = (*model.FishBuff)(nil)
	_ Suggester   = (*model.FishBuff)(nil)
)

// Suggester provides suggestions.
type Suggester interface {
	// CurrentSuggestion returns the current suggestion.
	CurrentSuggestion() (string, bool)

	// NextSuggestion returns the next suggestion.
	NextSuggestion() (string, bool)

	// PrevSuggestion returns the prev suggestion.
	PrevSuggestion() (string, bool)

	// ClearSuggestions clear out all suggestions.
	ClearSuggestions()
}

type ConfigurableSuggester interface {
	Suggester
	SetSuggestMode(model.SuggestMode)
	GetSuggestMode() model.SuggestMode
}

// PromptModel represents a prompt buffer.
type PromptModel interface {
	// SetText sets the model text.
	SetText(txt, sug string)

	// GetText returns the current text.
	GetText() string

	// GetSuggestion returns the current suggestion.
	GetSuggestion() string

	// ClearText clears out model text.
	ClearText(fire bool)

	// Notify notifies all listener of current suggestions.
	Notify(bool)

	// AddListener registers a command listener.
	AddListener(model.BuffWatcher)

	// AddListener registers a command listener.
	AddListenerWithPriority(model.BuffWatcher, int)

	// RemoveListener removes a listener.
	RemoveListener(model.BuffWatcher)

	// IsActive returns true if prompt is active.
	IsActive() bool

	// SetActive sets whether the prompt is active or not.
	SetActive(bool)

	// Add adds a new char to the prompt.
	Add(rune)

	// Delete deletes the last prompt character.
	Delete()

	// Insert inserts a new char at a given cursor position.
	Insert(rune, int)

	// DeleteAt deletes a char at a given position.
	DeleteAt(int)

	// DeleteRange deletes a range of chars.
	DeleteRange(int, int)
}

// Prompt captures users free from command input.
type Prompt struct {
	*tview.TextView

	app            *App
	noIcons        bool
	icon           rune
	styles         *config.Styles
	model          PromptModel
	spacer         int
	hasScrolled    bool
	cursorPosition int
	suggestMode    model.SuggestMode
	prompt         string
}

// NewPrompt returns a new command view.
func NewPrompt(app *App, noIcons bool, styles *config.Styles) *Prompt {
	p := Prompt{
		app:         app,
		styles:      styles,
		noIcons:     noIcons,
		TextView:    tview.NewTextView(),
		spacer:      defaultSpacer,
		suggestMode: model.SuggestAutoComplete,
		prompt:      defaultPrompt,
	}
	if noIcons {
		p.spacer--
	}
	p.SetWordWrap(true)
	p.SetWrap(true)
	p.SetDynamicColors(true)
	p.SetBorder(true)
	p.SetBorderPadding(0, 0, 1, 1)
	p.SetBackgroundColor(styles.K9s.Prompt.BgColor.Color())
	p.SetTextColor(styles.K9s.Prompt.FgColor.Color())
	styles.AddListener(&p)
	p.SetInputCapture(p.keyboard)
	p.ShowCursor(true)

	return &p
}

// SendKey sends an keyboard event (testing only!).
func (p *Prompt) SendKey(evt *tcell.EventKey) {
	p.keyboard(evt)
}

// SendStrokes (testing only!)
func (p *Prompt) SendStrokes(s string) {
	for _, r := range s {
		p.keyboard(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}

// Deactivate sets the prompt as inactive.
func (p *Prompt) Deactivate() {
	if p.model != nil {
		p.model.ClearText(true)
		p.model.SetActive(false)
	}
}

// SetModel sets the prompt buffer model.
func (p *Prompt) SetModel(m PromptModel) {
	if p.model != nil {
		p.model.RemoveListener(p)
	}
	p.model = m
	p.model.AddListener(p)
}

func (p *Prompt) keyboard(evt *tcell.EventKey) *tcell.EventKey {
	m, ok := p.model.(ConfigurableSuggester)
	if !ok {
		return evt
	}

	hasScrolled := false
	// nolint:exhaustive
	switch evt.Key() {
	case tcell.KeyBackspace2, tcell.KeyBackspace, tcell.KeyDelete:
		if evt.Modifiers() == tcell.ModAlt {
			text := p.model.GetText()
			if p.cursorPosition > 0 && p.cursorPosition <= len(text) {
				cursorPosition := p.cursorPosition
				for cursorPosition > 0 && text[cursorPosition-1] == ' ' {
					cursorPosition--
				}
				lastBlank := strings.LastIndex(text[:cursorPosition], " ") + 1
				p.model.DeleteRange(lastBlank, p.cursorPosition-1)
				p.cursorPosition = lastBlank
			}
		} else {
			if p.cursorPosition > 0 && p.cursorPosition <= len(p.model.GetText()) {
				p.model.DeleteAt(p.cursorPosition - 1)
				p.cursorPosition--
			}
		}
	case tcell.KeyRune:
		if p.cursorPosition < len(p.model.GetText()) {
			p.model.Insert(evt.Rune(), p.cursorPosition)
		} else {
			p.model.Add(evt.Rune())
		}
		p.cursorPosition++
	case tcell.KeyEscape:
		p.model.ClearText(true)
		p.model.SetActive(false)
		p.setSuggestMode(defaultSuggestMode)
		p.cursorPosition = 0
	case tcell.KeyEnter, tcell.KeyCtrlE:
		p.model.SetText(p.model.GetText(), "")
		p.model.SetActive(false)
		p.setSuggestMode(defaultSuggestMode)
		p.cursorPosition = 0
	case tcell.KeyCtrlW, tcell.KeyCtrlU:
		p.model.ClearText(true)
		p.cursorPosition = 0
	case tcell.KeyUp:
		if !p.hasScrolled && p.app.cmdBuff.Empty() {
			hasScrolled = true
		} else {
			hasScrolled = p.hasScrolled
		}
		if s, ok := m.PrevSuggestion(); ok {
			p.suggest(p.model.GetText(), s)
		}

	case tcell.KeyDown:
		if !p.hasScrolled && p.app.cmdBuff.Empty() {
			if s, ok := m.CurrentSuggestion(); ok {
				p.suggest(p.model.GetText(), s)
			}
		} else {
			if s, ok := m.NextSuggestion(); ok {
				p.suggest(p.model.GetText(), s)
			}
		}
		hasScrolled = true
	case tcell.KeyTab:
		p.setSuggestMode(defaultSuggestMode)
		if s, ok := m.CurrentSuggestion(); ok {
			p.model.SetText(p.formatSuggest(p.model.GetText(), s, false), "")
			m.ClearSuggestions()
			p.cursorPosition = len(p.model.GetText())
		} else {
			if s, ok := m.NextSuggestion(); ok {
				p.suggest(p.model.GetText(), s)
			}
		}
	case tcell.KeyLeft:
		hasScrolled = false
		if p.suggestMode != defaultSuggestMode {
			p.setSuggestMode(defaultSuggestMode)
			if s, ok := m.CurrentSuggestion(); ok {
				p.model.SetText(p.formatSuggest(p.model.GetText(), s, false), "")
				m.ClearSuggestions()
				p.cursorPosition = 0
			}
		} else {
			if p.model.GetText() != "" && p.cursorPosition > 0 {
				p.cursorPosition--
			}
		}
	case tcell.KeyRight:
		hasScrolled = false
		if p.suggestMode != defaultSuggestMode {
			p.setSuggestMode(defaultSuggestMode)
			if s, ok := m.CurrentSuggestion(); ok {
				p.model.SetText(p.formatSuggest(p.model.GetText(), s, false), "")
				m.ClearSuggestions()
				p.cursorPosition = len(p.model.GetText())
			}
		} else {
			end := len(p.model.GetText())
			if p.cursorPosition < end {
				p.cursorPosition++
			}
		}
	case tcell.KeyHome:
		p.cursorPosition = 0
	case tcell.KeyEnd:
		p.cursorPosition = len(p.model.GetText())
		if s, ok := m.CurrentSuggestion(); ok {
			p.model.SetText(p.formatSuggest(p.model.GetText(), s, false), "")
			m.ClearSuggestions()
			p.cursorPosition = len(p.model.GetText())
		}
	case tcell.KeyCtrlR:
		if p.suggestMode == model.SuggestFullText {
			if s, ok := m.PrevSuggestion(); ok {
				p.suggest(p.model.GetText(), s)
			}
			p.setSuggestMode(defaultSuggestMode)
		} else {
			p.setSuggestMode(model.SuggestFullText)
			p.model.ClearText(true)
			p.cursorPosition = 0
			p.model.SetText("", "")
		}
	}

	p.hasScrolled = hasScrolled

	return nil
}

func (p *Prompt) setSuggestMode(mode model.SuggestMode) {
	if p.suggestMode == mode {
		return
	}

	m, ok := p.model.(ConfigurableSuggester)
	if !ok {
		return
	}

	p.suggestMode = mode
	m.SetSuggestMode(p.suggestMode)
	switch p.suggestMode {
	case model.SuggestAutoComplete:
		p.spacer = defaultSpacer
		p.prompt = defaultPrompt
	case model.SuggestFullText:
		p.spacer = searchSpacer
		p.prompt = searchPrompt
	}

}

func (p *Prompt) SetPrompt(prompt string, spacer int) {
	p.prompt = prompt
	p.spacer = spacer
}

func (p *Prompt) GetSuggestMode() model.SuggestMode {
	return p.suggestMode
}

// StylesChanged notifies skin changed.
func (p *Prompt) StylesChanged(s *config.Styles) {
	p.styles = s
	p.SetBackgroundColor(s.K9s.Prompt.BgColor.Color())
	p.SetTextColor(s.K9s.Prompt.FgColor.Color())
}

// InCmdMode returns true if command is active, false otherwise.
func (p *Prompt) InCmdMode() bool {
	if p.model == nil {
		return false
	}
	return p.model.IsActive()
}

func (p *Prompt) activate() {
	p.Clear()
	p.write(p.model.GetText(), p.model.GetSuggestion())
	p.cursorPosition = len(p.model.GetText())
	p.suggestMode = defaultSuggestMode
	p.spacer = defaultSpacer
	p.hasScrolled = false
	p.model.Notify(false)
}

func (p *Prompt) update(text, suggestion string) {
	p.Clear()
	p.write(text, suggestion)
}

func (p *Prompt) suggest(text, suggestion string) {
	p.Clear()
	p.write(text, suggestion)
}

func (p *Prompt) Draw(screen tcell.Screen) {
	p.TextView.Draw(screen)
	if p.suggestMode == model.SuggestAutoComplete && p.cursorPosition >= 0 {
		x, y, _, height := p.GetInnerRect()
		screen.ShowCursor(x+p.spacer+p.cursorPosition, y+height-1)
	} else {
		screen.HideCursor()
	}
}

func (p *Prompt) formatSuggest(text string, suggest string, withColor bool) string {
	if text == suggest || text != "" && suggest == "" {
		return text
	}
	txt := text
	if text == "" && suggest != "" {
		txt = suggest
		if withColor {
			txt = fmt.Sprintf("[%s::-]%s", p.styles.K9s.Prompt.SuggestColor, txt)
		}
	} else {
		matchIndex := strings.Index(suggest, text)
		if matchIndex != -1 {
			left := suggest[:matchIndex]
			if withColor && matchIndex > 0 {
				left = fmt.Sprintf("[%s::b]%s[-::]", p.styles.K9s.Prompt.SuggestColor, suggest[:matchIndex])
			}
			right := suggest[matchIndex+len(text):]
			if withColor && matchIndex < len(suggest)-1 {
				right = fmt.Sprintf("[%s::b]%s[-::]", p.styles.K9s.Prompt.SuggestColor, suggest[matchIndex+len(text):])
			}
			txt = left + text + right
		} else {
			if withColor {
				txt += fmt.Sprintf("[%s::-]%s", p.styles.K9s.Prompt.SuggestColor, suggest)
			} else {
				txt += suggest
			}
		}
	}
	return txt
}

func (p *Prompt) write(text, suggest string) {
	txt := text
	if suggest != "" {
		txt = p.formatSuggest(text, suggest, true)
	}
	fmt.Fprintf(p, p.prompt, p.icon, txt)
}

// ----------------------------------------------------------------------------
// Event Listener protocol...

// BufferCompleted indicates input was accepted.
func (p *Prompt) BufferCompleted(text, suggestion string) {
	p.update(text, suggestion)
}

// BufferChanged indicates the buffer was changed.
func (p *Prompt) BufferChanged(text, suggestion string) {
	p.update(text, suggestion)
}

// SuggestionChanged notifies the suggestion changed.
func (p *Prompt) SuggestionChanged(text, suggestion string) {
	p.suggest(text, suggestion)
}

// BufferActive indicates the buff activity changed.
func (p *Prompt) BufferActive(activate bool, kind model.BufferKind) {
	if activate {
		p.ShowCursor(true)
		p.SetBorder(true)
		p.SetTextColor(p.styles.FgColor())
		p.SetBorderColor(p.colorFor(kind))
		p.icon = p.iconFor(kind)
		p.activate()
		return
	}

	p.ShowCursor(false)
	p.SetBorder(false)
	p.SetBackgroundColor(p.styles.BgColor())
	p.Clear()
}

func (p *Prompt) iconFor(k model.BufferKind) rune {
	if p.noIcons {
		return ' '
	}

	// nolint:exhaustive
	switch k {
	case model.CommandBuffer:
		return '🐶'
	default:
		return '🐩'
	}
}

// ----------------------------------------------------------------------------
// Helpers...

func (p *Prompt) colorFor(k model.BufferKind) tcell.Color {
	// nolint:exhaustive
	switch k {
	case model.CommandBuffer:
		return p.styles.Prompt().Border.CommandColor.Color()
	default:
		return p.styles.Prompt().Border.DefaultColor.Color()
	}
}
