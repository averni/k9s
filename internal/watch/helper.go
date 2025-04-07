// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package watch

import (
	"fmt"
	"log/slog"
	"path"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	di "k8s.io/client-go/dynamic/dynamicinformer"
)

func toGVR(gvr string) schema.GroupVersionResource {
	tokens := strings.Split(gvr, "/")
	if len(tokens) < 3 {
		tokens = append([]string{""}, tokens...)
	}

	return schema.GroupVersionResource{
		Group:    tokens[0],
		Version:  tokens[1],
		Resource: tokens[2],
	}
}

func namespaced(n string) (ns, res string) {
	ns, res = path.Split(n)

	return strings.Trim(ns, "/"), res
}

// DumpFactory for debug.
func DumpFactory(f *Factory) {
	slog.Debug("----------- FACTORIES -------------")
	f.factories.Range(func(key, value interface{}) bool {
		ns := key.(string)
		slog.Debug(fmt.Sprintf("  Factory for NS %q", ns))
		return true
	})
	slog.Debug("-----------------------------------")
}

// DebugFactory for debug.
func DebugFactory(f *Factory, ns, gvr string) {
	slog.Debug(fmt.Sprintf("----------- DEBUG FACTORY (%s) -------------", gvr))
	facVal, ok := f.factories.Load(ns)
	if !ok {
		return
	}
	fac := facVal.(di.DynamicSharedInformerFactory)
	inf := fac.ForResource(toGVR(gvr))
	for i, k := range inf.Informer().GetStore().ListKeys() {
		slog.Debug(fmt.Sprintf("%d -- %s", i, k))
	}
}
