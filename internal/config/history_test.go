package config_test

import (
	"testing"

	"github.com/derailed/k9s/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewHistory(t *testing.T) {
	l := config.NewHistory()
	l.Validate(nil, nil)

	assert.Equal(t, 20, l.MaxHistory)
}
