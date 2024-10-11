package view

import (
	"context"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/tcell/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterRole represents a PVC custom viewer.
type ClusterRole struct {
	ResourceViewer
}

// NewClusterRole returns a new viewer.
func NewClusterRole(gvr client.GVR) ResourceViewer {
	v := ClusterRole{
		ResourceViewer: NewBrowser(gvr),
	}
	v.AddBindKeysFn(v.bindKeys)
	return &v
}

func (c *ClusterRole) bindKeys(aa *ui.KeyActions) {
	if c.App().Config.K9s.IsReadOnly() {
		return
	}
	aa.Add(ui.KeyX, ui.NewKeyAction("Expand Aggregation", c.showAggregation(), true))
}

func showClusterRoles(app *App, path string, sel *metav1.LabelSelector) {
	l, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	v := NewClusterRole(client.NewGVR("rbac.authorization.k8s.io/v1/clusterroles"))

	v.SetContextFn(crCtx(path, l.String()))

	if err := app.inject(v, false); err != nil {
		app.Flash().Err(err)
	}
}

func (c *ClusterRole) showAggregation() func(evt *tcell.EventKey) *tcell.EventKey {
	return func(evt *tcell.EventKey) *tcell.EventKey {
		path := c.GetTable().GetSelectedItem()
		if path == "" {
			return nil
		}

		var crDao dao.Rbac
		cr, err := crDao.LoadClusterRole(c.App().factory, path)
		if err != nil {
			c.App().Flash().Err(err)
			return nil
		}
		if cr.AggregationRule == nil || len(cr.AggregationRule.ClusterRoleSelectors) == 0 {
			c.App().Flash().Errf("ClusterRole %s does not have any aggregation rules", path)
			return nil
		}
		// TODO: Support multiple selectors
		showClusterRoles(c.App(), path, &cr.AggregationRule.ClusterRoleSelectors[0])

		return nil
	}
}

func crCtx(path, ls string) ContextFunc {
	return func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, internal.KeyPath, "")
		ctx = context.WithValue(ctx, internal.KeySubjectKind, "ClusterRole")
		ctx = context.WithValue(ctx, internal.KeySubjectName, path)
		return context.WithValue(ctx, internal.KeyLabels, ls)
	}
}
