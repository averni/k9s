// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/model1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// ClusterRole renders a K8s ClusterRole to screen.
type ClusterRole struct {
	Base
}

// Header returns a header rbw.
func (ClusterRole) Header(string) model1.Header {
	return model1.Header{
		model1.HeaderColumn{Name: "NAME"},
		model1.HeaderColumn{Name: "AGGR", Wide: true},
		model1.HeaderColumn{Name: "AGGR-TO", Wide: true},
		model1.HeaderColumn{Name: "LABELS", Wide: true},
		model1.HeaderColumn{Name: "AGE", Time: true},
	}
}

// Render renders a K8s resource to screen.
func (ClusterRole) Render(o interface{}, ns string, r *model1.Row) error {
	raw, ok := o.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("expecting clusterrole, but got %T", o)
	}
	var cr rbacv1.ClusterRole
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw.Object, &cr)
	if err != nil {
		return err
	}

	aggregated := ""
	if hasAggregation(&cr) {
		aggregated = "ⓨ"
	}

	r.ID = client.FQN("-", cr.ObjectMeta.Name)
	r.Fields = model1.Fields{
		cr.Name,
		aggregated,
		readAggregateTo(cr.Labels),
		mapToStr(cr.Labels),
		ToAge(cr.GetCreationTimestamp()),
	}

	return nil
}

// helpers
const aggregateToLabelPrefix = "/aggregate-to-"

func readAggregateTo(labels map[string]string) string {
	aggregateTo := make([]string, 0, 10)
	for label := range labels {
		aggregateToLabelIndex := strings.Index(label, aggregateToLabelPrefix)
		if aggregateToLabelIndex >= 0 && strings.HasSuffix(labels[label], "true") {
			aggregateTo = append(aggregateTo, label[aggregateToLabelIndex+len(aggregateToLabelPrefix):])
		}
	}
	if len(aggregateTo) > 0 {
		sort.Strings(aggregateTo)
	}
	return strings.Join(aggregateTo, ",")
}

func hasAggregation(cr *v1.ClusterRole) bool {
	return cr.AggregationRule != nil && len(cr.AggregationRule.ClusterRoleSelectors) > 0
}
