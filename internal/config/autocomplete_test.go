package config_test

import (
	"testing"

	"github.com/derailed/k9s/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewAutocomplete(t *testing.T) {
	a := config.NewAutocomplete()
	a.Validate(nil, nil)

	assert.Equal(t, true, a.AutocompleteNamespace)
	assert.Equal(t, config.DefaultAutocompleteRefreshRate, a.RefreshRate)
}
