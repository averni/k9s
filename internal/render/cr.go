// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/derailed/k9s/internal/client"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// ClusterRole renders a K8s ClusterRole to screen.
type ClusterRole struct {
	Base
}

var colors = []string{
	"green",
	"yellow",
	"blue",
	"magenta",
	"cyan",
	"white",
	"red",
}

// Header returns a header rbw.
func (cr ClusterRole) Header(string) Header {
	return Header{
		HeaderColumn{Name: "NAME"},
		HeaderColumn{Name: "AGGREGATED"},
		HeaderColumn{Name: "AGGREGATE-TO", Wide: true},
		HeaderColumn{Name: "LABELS", Wide: true},
		HeaderColumn{Name: "AGE", Time: true},
	}
}

// Render renders a K8s resource to screen.
func (c ClusterRole) Render(o interface{}, ns string, r *Row) error {
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
		color := colors[0]
		aggregated = "[" + color + "::b]Ⓐ"
	}

	r.ID = client.FQN("-", cr.ObjectMeta.Name)
	r.Fields = Fields{
		cr.Name,
		aggregated,
		readAggregateTo(cr.Labels),
		mapToStr(cr.Labels),
		toAge(cr.GetCreationTimestamp()),
	}

	return nil
}

// helpers
const aggregateToPrefix = "rbac.authorization.k8s.io/aggregate-to-"

func readAggregateTo(labels map[string]string) string {
	aggregateTo := make([]string, 0, 10)
	for label := range labels {
		if strings.HasPrefix(label, aggregateToPrefix) && strings.HasSuffix(labels[label], "true") {
			aggregateTo = append(aggregateTo, label[len(aggregateToPrefix):])
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
