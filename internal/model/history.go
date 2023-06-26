// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package model

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// MaxHistory tracks max command history.
const MaxHistory = 20

type HistoryListener interface {
	// HistoryChanged notifies history updates.
	HistoryChanged([]string)
}

// History represents a command history.
type History struct {
	commands  []string
	limit     int
	listeners []HistoryListener
}

// NewHistory returns a new instance.
func NewHistory(limit int) *History {
	return &History{
		limit: limit,
	}
}

// SetLimit sets the max history limit.
func (h *History) SetLimit(l int) {
	h.limit = l
}

// List returns the current command history.
func (h *History) List() []string {
	return h.commands
}

// Push adds a new item.
func (h *History) Push(c string) {
	if c == "" {
		return
	}

	c = strings.ToLower(c)
	if len(h.commands) > 0 && h.commands[0] == c {
		return
	}
	if len(h.commands) < h.limit {
		h.commands = append([]string{c}, h.commands...)
	} else {
		h.commands = append([]string{c}, h.commands[:len(h.commands)-1]...)
	}
	h.fireHistoryChanged(h.commands)
}

func (h *History) Pop() string {
	if len(h.commands) == 0 {
		return ""
	}
	c := h.commands[0]
	h.commands = h.commands[1:]
	h.fireHistoryChanged(h.commands)
	return c
}

// Clear clears out the stack.
func (h *History) Clear() {
	log.Debug().Msgf("History CLEARED!!!")
	h.commands = nil
	h.fireHistoryChanged(h.commands)
}

// Empty returns true if no history.
func (h *History) Empty() bool {
	return len(h.commands) == 0
}

func (h *History) indexOf(s string) int {
	for i, c := range h.commands {
		if c == s {
			return i
		}
	}
	return -1
}

// Set the history stack.
func (h *History) Set(s []string) {
	h.commands = s
	if len(h.commands) > h.limit {
		h.commands = h.commands[:h.limit]
	}
	h.fireHistoryChanged(h.commands)
}

// AddListener registers a new history listener.
func (h *History) AddListener(l HistoryListener) {
	h.listeners = append(h.listeners, l)
}

// fireHistoryChanged notifies listeners of history changes.
func (h *History) fireHistoryChanged(ss []string) {
	for _, l := range h.listeners {
		l.HistoryChanged(ss)
	}
}
