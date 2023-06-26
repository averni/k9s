// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package model

import (
	"sort"
)

// SuggestionListener listens for suggestions.
type SuggestionListener interface {
	BuffWatcher

	// SuggestionChanged notifies suggestion changes.
	SuggestionChanged(text, sugg string)
}

type SuggestMode int

const (
	// SuggestNone indicates no suggestions.
	SuggestNone SuggestMode = iota
)

type SuggestModeListener interface {
	// SetSuggestModeChanged indicates the suggest mode has changed.
	SuggestModeChanged(SuggestMode)
}

// SuggestionFunc produces suggestions.
type SuggestionFunc func(text string, suggestMode SuggestMode) sort.StringSlice

// FishBuff represents a suggestion buffer.
type FishBuff struct {
	*CmdBuff

	suggestionFn         SuggestionFunc
	suggestions          []string
	suggestionIndex      int
	suggestMode          SuggestMode
	suggestModeListeners map[SuggestModeListener]struct{}
}

// NewFishBuff returns a new command buffer.
func NewFishBuff(key rune, kind BufferKind) *FishBuff {
	return &FishBuff{
		CmdBuff:              NewCmdBuff(key, kind),
		suggestionIndex:      0,
		suggestMode:          SuggestNone,
		suggestModeListeners: make(map[SuggestModeListener]struct{}),
	}
}

// SetSuggestMode sets the suggestion mode.
func (f *FishBuff) SetSuggestMode(mode SuggestMode) {
	if f.suggestMode != mode {
		f.fireSuggestionModeChanged(mode)
		f.suggestMode = mode
	}
}

// GetSuggestMode returns the suggestion mode.
func (f *FishBuff) GetSuggestMode() SuggestMode {
	return f.suggestMode
}

// PrevSuggestion returns the prev suggestion.
func (f *FishBuff) PrevSuggestion() (string, bool) {
	if len(f.suggestions) == 0 {
		return "", false
	}

	f.suggestionIndex--
	if f.suggestionIndex < 0 {
		f.suggestionIndex = len(f.suggestions) - 1
	}
	return f.suggestions[f.suggestionIndex], true
}

// NextSuggestion returns the next suggestion.
func (f *FishBuff) NextSuggestion() (string, bool) {
	if len(f.suggestions) == 0 {
		return "", false
	}
	f.suggestionIndex++
	if f.suggestionIndex >= len(f.suggestions) {
		f.suggestionIndex = 0
	}
	return f.suggestions[f.suggestionIndex], true
}

// ClearSuggestions clear out all suggestions.
func (f *FishBuff) ClearSuggestions() {
	if len(f.suggestions) > 0 {
		f.suggestions = f.suggestions[:0]
	}
	f.suggestionIndex = 0
}

// CurrentSuggestion returns the current suggestion.
func (f *FishBuff) CurrentSuggestion() (string, bool) {
	if len(f.suggestions) == 0 {
		return "", false
	}

	return f.suggestions[f.suggestionIndex], true
}

// AutoSuggests returns true if model implements auto suggestions.
func (f *FishBuff) AutoSuggests() bool {
	return true
}

// Suggestions returns suggestions.
func (f *FishBuff) Suggestions() []string {
	if f.suggestionFn != nil {
		return f.suggestionFn(string(f.buff), f.suggestMode)
	}
	return nil
}

// SetSuggestionFn sets up suggestions.
func (f *FishBuff) SetSuggestionFn(fn SuggestionFunc) {
	f.suggestionFn = fn
}

// Notify publish suggestions to all listeners.
func (f *FishBuff) Notify(delete bool) {
	if f.suggestionFn == nil {
		return
	}
	f.fireSuggestionChanged(f.suggestionFn(string(f.buff), f.suggestMode))
}

// Add adds a new character to the buffer.
func (f *FishBuff) Add(r rune) {
	f.CmdBuff.Add(r)
	f.Notify(false)
}

// Delete removes the last character from the buffer.
func (f *FishBuff) Delete() {
	f.CmdBuff.Delete()
	f.Notify(true)
}

func (f *FishBuff) fireSuggestionChanged(ss []string) {
	f.suggestions, f.suggestionIndex = ss, 0

	var suggest string
	if len(ss) > 0 && !f.CmdBuff.Empty() {
		suggest = ss[f.suggestionIndex]
	}

	f.SetText(f.GetText(), suggest)

}

func (f *FishBuff) AddSuggestModeListener(l SuggestModeListener) {
	f.mx.Lock()
	f.suggestModeListeners[l] = struct{}{}
	f.mx.Unlock()
}

// RemoveListener removes a listener.
func (f *FishBuff) RemoveSuggestModeListener(l SuggestModeListener) {
	f.mx.Lock()
	delete(f.suggestModeListeners, l)
	f.mx.Unlock()
}

func (f *FishBuff) fireSuggestionModeChanged(s SuggestMode) {
	for l := range f.suggestModeListeners {
		l.SuggestModeChanged(s)
	}
}
