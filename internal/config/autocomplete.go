package config

import (
	"time"

	"github.com/derailed/k9s/internal/client"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultAutocompleteRefreshRate tracks default autocomplete refresh rate.
	DefaultAutocompleteRefreshRate = "2m"
)

// View tracks view configuration options.
type Autocomplete struct {
	AutocompleteNamespace bool          `yaml:"autocompleteNamespace"`
	RefreshRate           string        `yaml:"refreshRate"`
	SpellCheck            bool          `yaml:"spellCheck"`
	RefreshRateDuration   time.Duration `yaml:"-"`
}

// NewView creates a new view configuration.
func NewAutocomplete() *Autocomplete {
	return &Autocomplete{
		AutocompleteNamespace: true,
		RefreshRate:           DefaultAutocompleteRefreshRate,
		SpellCheck:            false,
	}
}

// Validate a view configuration.
func (h *Autocomplete) Validate(client.Connection, KubeSettings) {
	if h.RefreshRate == "" {
		h.RefreshRate = DefaultAutocompleteRefreshRate
	}
	var err error
	h.RefreshRateDuration, err = time.ParseDuration(h.RefreshRate)
	if err != nil {
		log.Error().Err(err).Msgf("Unable to parse refresh rate %q", h.RefreshRate)
		h.RefreshRateDuration = 20 * time.Second
	}
}
