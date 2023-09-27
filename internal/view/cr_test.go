package view_test

import (
	"testing"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/view"
	"github.com/stretchr/testify/assert"
)

func TestClusterRoleNew(t *testing.T) {
	v := view.NewClusterRole(client.NewGVR("rbac.authorization.k8s.io/v1/clusterroles"))

	assert.Nil(t, v.Init(makeCtx()))
	assert.Equal(t, "ClusterRoles", v.Name())
	assert.Equal(t, 6, len(v.Hints()))
}
