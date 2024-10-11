// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package ui

import (
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/derailed/k9s/internal/config"
	"github.com/derailed/k9s/internal/model"
	"github.com/derailed/tcell/v2"
	"github.com/derailed/tview"
)

const (
	defaultPrompt = "%c%c [::b]%s"
	defaultSpacer = 4
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

// PromptModel represents a prompt buffer.
type PromptModel interface {
	// SetText sets the model text.
	SetText(txt, sug string, clear bool)

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

type Cursor struct {
	Position int32
}

// MoveToLastWord move the cursor to the previous word.
func (c *Cursor) MoveWordLeft(text string) int32 {
	if text == "" || c.Position == 0 {
		return 0
	}

	cursorPosition := c.Position
	for cursorPosition > 0 && text[cursorPosition-1] == ' ' {
		cursorPosition--
	}
	c.Position = int32(strings.LastIndex(text[:cursorPosition], " ") + 1)
	return c.Position
}

// MoveLeft moves the cursor to the left by one character.
func (c *Cursor) MoveLeft(text string) int32 {
	if text == "" || c.Position == 0 {
		return 0
	}
	c.Position--
	return c.Position
}

// MoveRight moves the cursor to the right by one character.
func (c *Cursor) MoveRight(text string) int32 {
	if text == "" || c.Position >= int32(len(text)) {
		return int32(len(text))
	}
	c.Position++
	return c.Position
}

// MoveWordRight moves the cursor to the right by one word.
func (c *Cursor) MoveWordRight(text string) int32 {
	if text == "" || c.Position >= int32(len(text)) {
		return int32(len(text))
	}

	cursorPosition := c.Position
	for cursorPosition < int32(len(text)) && text[cursorPosition] == ' ' {
		cursorPosition++
	}
	nextBlank := strings.Index(text[cursorPosition:], " ")
	if nextBlank == -1 {
		nextBlank = len(text) - int(cursorPosition)
	}
	c.Position = cursorPosition + int32(nextBlank)
	return c.Position
}

// Reset resets the cursor position to 0.
func (c *Cursor) Reset() {
	c.Position = 0
}

// MoveEnd moves the cursor to the end of the text.
func (c *Cursor) MoveEnd(text string) {
	c.Position = int32(len(text))
}

// Prompt captures users free from command input.
type Prompt struct {
	*tview.TextView

	app     *App
	noIcons bool
	icon    rune
	prefix  rune
	styles  *config.Styles
	model   PromptModel
	spacer  int
	cursor  Cursor
	mx      sync.RWMutex
}

// NewPrompt returns a new command view.
func NewPrompt(app *App, noIcons bool, styles *config.Styles) *Prompt {
	p := Prompt{
		app:      app,
		styles:   styles,
		noIcons:  noIcons,
		TextView: tview.NewTextView(),
		spacer:   defaultSpacer,
	}
	if noIcons {
		p.spacer--
	}
	p.SetWordWrap(true)
	p.SetWrap(true)
	p.SetDynamicColors(true)
	p.SetBorder(true)
	p.SetBorderPadding(0, 0, 1, 1)
	styles.AddListener(&p)
	p.SetInputCapture(p.keyboard)
	p.ShowCursor(true)

	return &p
}

// SendKey sends a keyboard event (testing only!).
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
	m, ok := p.model.(Suggester)
	if !ok {
		return evt
	}

	//nolint:exhaustive
	switch evt.Key() {
	case tcell.KeyBackspace2, tcell.KeyBackspace, tcell.KeyDelete:
		start, end := p.cursor.Position-1, p.cursor.Position-1
		if evt.Modifiers() == tcell.ModAlt {
			p.cursor.MoveWordLeft(p.model.GetText())
			start = p.cursor.Position
		} else {
			p.cursor.MoveLeft(p.model.GetText())
		}
		p.model.DeleteRange(int(start), int(end))

	case tcell.KeyRune:
		r := evt.Rune()
		// Filter out control characters and non-printable runes that may come from
		// terminal escape sequences (e.g., cursor position reports like [7;15R)
		// Only accept printable characters for user input
		if isValidInputRune(r) && int(p.cursor.Position) < len(p.model.GetText()) {
			p.model.Insert(r, int(p.cursor.Position))
		} else {
			p.model.Add(r)
		}
		p.cursor.MoveRight(p.model.GetText())

	case tcell.KeyEscape:
		p.model.ClearText(true)
		p.model.SetActive(false)
		p.cursor.Reset()

	case tcell.KeyEnter, tcell.KeyCtrlE:
		p.model.SetText(p.model.GetText(), "", true)
		p.model.SetActive(false)
		p.cursor.Reset()

	case tcell.KeyCtrlW, tcell.KeyCtrlU:
		p.model.ClearText(true)
		p.cursor.Reset()

	case tcell.KeyUp:
		if s, ok := m.NextSuggestion(); ok {
			p.model.SetText(p.model.GetText(), s, true)
		}

	case tcell.KeyDown:
		if s, ok := m.PrevSuggestion(); ok {
			p.model.SetText(p.model.GetText(), s, true)
		}

	case tcell.KeyTab, tcell.KeyRight, tcell.KeyCtrlF:
		if s, ok := m.CurrentSuggestion(); ok {
			p.model.SetText(p.model.GetText()+s, "", true)
			m.ClearSuggestions()
			p.cursor.MoveEnd(p.model.GetText())
		} else {
			if evt.Key() == tcell.KeyRight {
				if evt.Modifiers() == tcell.ModCtrl {
					p.cursor.MoveWordRight(p.model.GetText())
				} else {
					p.cursor.MoveRight(p.model.GetText())
				}
			}
		}

	case tcell.KeyLeft:
		if evt.Modifiers() == tcell.ModCtrl {
			p.cursor.MoveWordLeft(p.model.GetText())
		} else {
			p.cursor.MoveLeft(p.model.GetText())
		}

	case tcell.KeyHome:
		p.cursor.Reset()

	case tcell.KeyEnd:
		p.cursor.MoveEnd(p.model.GetText())
	}

	return nil
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
	p.SetCursorIndex(len(p.model.GetText()))
	p.write(p.model.GetText(), p.model.GetSuggestion())
	p.model.Notify(false)
	p.cursor.MoveEnd(p.model.GetText())
}

func (p *Prompt) Clear() {
	p.mx.Lock()
	defer p.mx.Unlock()

	p.TextView.Clear()
}

func (p *Prompt) Draw(sc tcell.Screen) {
	p.mx.RLock()
	defer p.mx.RUnlock()

	p.TextView.Draw(sc)
	x, y, _, height := p.GetInnerRect()
	sc.ShowCursor(x+p.spacer+int(p.cursor.Position), y+height-1)
}

func (p *Prompt) update(text, suggestion string) {
	p.Clear()
	p.write(text, suggestion)
}

func (p *Prompt) write(text, suggest string) {
	p.mx.Lock()
	defer p.mx.Unlock()

	p.SetCursorIndex(p.spacer + len(text))
	if suggest != "" {
		text += fmt.Sprintf("[%s::-]%s", p.styles.Prompt().SuggestColor, suggest)
	}
	p.StylesChanged(p.styles)
	_, _ = fmt.Fprintf(p, defaultPrompt, p.icon, p.prefix, text)
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
	p.update(text, suggestion)
}

// BufferActive indicates the buff activity changed.
func (p *Prompt) BufferActive(activate bool, kind model.BufferKind) {
	if activate {
		p.ShowCursor(true)
		p.SetBorder(true)
		p.SetTextColor(p.styles.FgColor())
		p.SetBorderColor(p.colorFor(kind))
		p.icon, p.prefix = p.prefixesFor(kind)
		p.activate()
		return
	}

	p.ShowCursor(false)
	p.SetBorder(false)
	p.SetBackgroundColor(p.styles.BgColor())
	p.Clear()
}

func (p *Prompt) prefixesFor(k model.BufferKind) (ic, prefix rune) {
	defer func() {
		if p.noIcons {
			ic = ' '
		}
	}()

	//nolint:exhaustive
	switch k {
	case model.CommandBuffer:
		return '🐶', '>'
	default:
		return '🐩', '/'
	}
}

// ----------------------------------------------------------------------------
// Helpers...

// isValidInputRune checks if a rune is valid for user input.
// It filters out control characters and non-printable characters that may
// come from terminal escape sequences (e.g., cursor position reports).
func isValidInputRune(r rune) bool {
	// Reject control characters (0x00-0x1F, 0x7F) except for common whitespace
	if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
		return false
	}
	// Only accept printable characters
	return unicode.IsPrint(r) || unicode.IsSpace(r)
}

func (p *Prompt) colorFor(k model.BufferKind) tcell.Color {
	//nolint:exhaustive
	switch k {
	case model.CommandBuffer:
		return p.styles.Prompt().Border.CommandColor.Color()
	default:
		return p.styles.Prompt().Border.DefaultColor.Color()
	}
}
