// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package watch

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/slogs"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	di "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
)

const (
	defaultResync   = 10 * time.Minute
	defaultWaitTime = 700 * time.Millisecond
	// defaultIdleTime: Maximum time before we stop a namespaced factory if it has not been accessed.
	// This is to prevent keeping informers alive when the user isn't interacting with any resources they are watching.
	// Under normal circumstances (like k8s operators) keeping an informer alive
	// forever is what we want.
	// On k9s this strategy is not optimal since we deal with a lot of resources and every informer
	// will keep syncing until the context is closed even if there are no active watchers,
	// causing a higher resource usage, mostly network/memory, without any benefit.
	// The default idle time is set below the default resync time in order to
	// stop idle informers before the first resync kicks in.
	defaultIdleTime      = defaultResync / 100 * 70
	defaultMonitorTime   = 1 * time.Minute
	debugInformerMetrics = true
)

// Factory tracks various resource informers.
type Factory struct {
	factories  map[string]di.DynamicSharedInformerFactory
	client     client.Connection
	forwarders Forwarders
	stopChan   map[string]chan struct{}
	monitor    *factoryMonitor
	mx         sync.RWMutex
}

// NewFactory returns a new informers factory.
func NewFactory(clt client.Connection) *Factory {
	return &Factory{
		client:     clt,
		factories:  make(map[string]di.DynamicSharedInformerFactory),
		forwarders: NewForwarders(),
		monitor:    nil,
		stopChan:   make(map[string]chan struct{}),
	}
}

// Start initializes the informers until caller cancels the context.
func (f *Factory) Start(ns string) {
	f.mx.Lock()
	defer f.mx.Unlock()

	if _, ok := f.stopChan[ns]; !ok {
		f.stopChan[ns] = make(chan struct{})
	}
	if _, ok := f.factories[ns]; ok {
		f.factories[ns].Start(f.stopChan[ns])
	}

	if f.monitor == nil {
		f.monitor = newFactoryMonitor(f, defaultIdleTime, defaultMonitorTime)
		f.monitor.Start()
	}
}

// Terminate terminates all watchers and forwards.
func (f *Factory) Terminate() {
	f.mx.Lock()
	defer f.mx.Unlock()

	if f.monitor != nil {
		f.monitor.Stop()
		f.monitor = nil
	}

	if f.stopChan != nil {
		for ns, stopChan := range f.stopChan {
			if stopChan != nil {
				close(stopChan)
			}
			delete(f.stopChan, ns)
		}
	}

	for k := range f.factories {
		delete(f.factories, k)
	}
	f.forwarders.DeleteAll()
}

// List returns a resource collection.
func (f *Factory) List(gvr *client.GVR, ns string, wait bool, lbls labels.Selector) ([]runtime.Object, error) {
	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}
	inf, err := f.CanForResource(ns, gvr, client.ListAccess)
	if err != nil {
		return nil, err
	}

	var oo []runtime.Object
	if client.IsClusterScoped(ns) {
		oo, err = inf.Lister().List(lbls)
	} else {
		oo, err = inf.Lister().ByNamespace(ns).List(lbls)
	}
	if err == nil {
		if !f.monitor.HasSynced(inf) {
			wait = true
		}
	}

	if !wait || (wait && inf.Informer().HasSynced()) {
		return oo, err
	}

	f.waitForCacheSync(ns)
	f.monitor.Synced(inf)
	if client.IsClusterScoped(ns) {
		return inf.Lister().List(lbls)
	}
	return inf.Lister().ByNamespace(ns).List(lbls)
}

// Get retrieves a given resource.
func (f *Factory) Get(gvr *client.GVR, fqn string, wait bool, _ labels.Selector) (runtime.Object, error) {
	ns, n := namespaced(fqn)
	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}

	inf, err := f.CanForResource(ns, gvr, []string{client.GetVerb})
	if err != nil {
		return nil, err
	}
	var o runtime.Object
	if client.IsClusterScoped(ns) {
		o, err = inf.Lister().Get(n)
	} else {
		o, err = inf.Lister().ByNamespace(ns).Get(n)
	}
	if err == nil {
		if !f.monitor.HasSynced(inf) {
			wait = true
		}
	}
	if !wait || (wait && inf.Informer().HasSynced()) {
		return o, err
	}

	f.waitForCacheSync(ns)
	f.monitor.Synced(inf)
	if client.IsClusterScoped(ns) {
		return inf.Lister().Get(n)
	}

	return inf.Lister().ByNamespace(ns).Get(n)
}

func (f *Factory) waitForCacheSync(ns string) {
	if client.IsClusterWide(ns) {
		ns = client.BlankNamespace
	}

	f.mx.RLock()
	defer f.mx.RUnlock()
	fac, ok := f.factories[ns]
	if !ok {
		return
	}

	// Hang for a sec for the cache to refresh if still not done bail out!
	c := make(chan struct{})
	go func(c chan struct{}) {
		<-time.After(defaultWaitTime)
		close(c)
	}(c)
	_ = fac.WaitForCacheSync(c)
}

// WaitForCacheSync waits for all factories to update their cache.
func (f *Factory) WaitForCacheSync() {
	for ns, fac := range f.factories {
		m := fac.WaitForCacheSync(f.stopChan[ns])
		for k, v := range m {
			slog.Debug("CACHE `%q Loaded %t:%s",
				slogs.Namespace, ns,
				slogs.ResGrpVersion, v,
				slogs.ResKind, k,
			)
		}
	}
}

// Client return the factory connection.
func (f *Factory) Client() client.Connection {
	return f.client
}

// FactoryFor returns a factory for a given namespace.
func (f *Factory) FactoryFor(ns string) di.DynamicSharedInformerFactory {
	return f.factories[ns]
}

// SetActiveNS sets the active namespace.
func (f *Factory) SetActiveNS(ns string) error {
	if f.isClusterWide() {
		return nil
	}
	_, err := f.ensureFactory(ns)
	return err
}

func (f *Factory) isClusterWide() bool {
	f.mx.RLock()
	defer f.mx.RUnlock()
	for _, namespace := range []string{
		client.BlankNamespace,
		client.NamespaceAll,
		client.ClusterScope,
	} {
		if _, ok := f.factories[namespace]; ok {
			return ok
		}
	}

	return false
}

// CanForResource return an informer is user has access.
func (f *Factory) CanForResource(ns string, gvr *client.GVR, verbs []string) (informers.GenericInformer, error) {
	auth, err := f.Client().CanI(ns, gvr, "", verbs)
	if err != nil {
		return nil, err
	}
	if !auth {
		return nil, fmt.Errorf("%v access denied on resource %q:%q", verbs, ns, gvr)
	}

	return f.ForResource(ns, gvr)
}

// ForResource returns an informer for a given resource.
func (f *Factory) ForResource(ns string, gvr *client.GVR) (informers.GenericInformer, error) {
	fact, err := f.ensureFactory(ns)
	if err != nil {
		return nil, err
	}
	inf := fact.ForResource(gvr.GVR())
	if inf == nil {
		slog.Error("No informer found",
			slogs.GVR, gvr,
			slogs.Namespace, ns,
		)
		return inf, nil
	}

	slog.Debug("Starting informer factory", slogs.GVR, gvr, slogs.Namespace, ns)
	f.Start(ns)
	fact.Start(f.stopChan[ns])

	f.monitor.Track(inf, gvr.AsResourceName(), ns)
	return inf, nil
}

func (f *Factory) ensureFactory(ns string) (di.DynamicSharedInformerFactory, error) {
	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}
	f.mx.Lock()
	defer f.mx.Unlock()
	if fac, ok := f.factories[ns]; ok {
		return fac, nil
	}

	dial, err := f.client.DynDial()
	if err != nil {
		return nil, err
	}
	actualNamespace := ns
	if client.IsClusterWide(ns) {
		actualNamespace = client.BlankNamespace
	}
	f.factories[ns] = di.NewFilteredDynamicSharedInformerFactory(
		dial,
		defaultResync,
		actualNamespace,
		nil,
	)
	if _, ok := f.stopChan[ns]; !ok {
		f.stopChan[ns] = make(chan struct{})
	}
	return f.factories[ns], nil
}

// AddForwarder registers a new portforward for a given container.
func (f *Factory) AddForwarder(pf Forwarder) {
	f.mx.Lock()
	defer f.mx.Unlock()

	f.forwarders[pf.ID()] = pf
}

// DeleteForwarder deletes portforward for a given container.
func (f *Factory) DeleteForwarder(path string) {
	count := f.forwarders.Kill(path)
	slog.Warn("Deleted portforward",
		slogs.Count, count,
		slogs.GVR, path,
	)
}

// Forwarders returns all portforwards.
func (f *Factory) Forwarders() Forwarders {
	f.mx.RLock()
	defer f.mx.RUnlock()

	return f.forwarders
}

// ForwarderFor returns a portforward for a given container or nil if none exists.
func (f *Factory) ForwarderFor(path string) (Forwarder, bool) {
	f.mx.RLock()
	defer f.mx.RUnlock()

	fwd, ok := f.forwarders[path]

	return fwd, ok
}

// ValidatePortForwards check if pods are still around for portforwards.
// BOZO!! Review!!!
func (f *Factory) ValidatePortForwards() {
	for k, fwd := range f.forwarders {
		tokens := strings.Split(k, ":")
		if len(tokens) != 2 {
			slog.Error("Invalid port-forward key", slogs.Key, k)
			return
		}
		paths := strings.Split(tokens[0], "|")
		if len(paths) < 1 {
			slog.Error("Invalid port-forward path", slogs.Path, tokens[0])
		}
		o, err := f.Get(client.PodGVR, paths[0], false, labels.Everything())
		if err != nil {
			fwd.Stop()
			delete(f.forwarders, k)
			continue
		}
		var pod v1.Pod
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.(*unstructured.Unstructured).Object, &pod); err != nil {
			continue
		}
		if pod.GetCreationTimestamp().Unix() > fwd.Age().Unix() {
			fwd.Stop()
			delete(f.forwarders, k)
		}
	}
}

// StopFactory stops a factory for a given namespace.
// It closes the stop channel and deletes the factory.
func (f *Factory) stopFactory(ns string) bool {
	f.mx.Lock()
	defer f.mx.Unlock()
	if _, ok := f.factories[ns]; ok {
		if _, ok := f.stopChan[ns]; !ok {
			slog.Error("No stop channel for ns", slogs.Namespace, ns)
			return false
		}
		slog.Debug("Stopping factory for ns", slogs.Namespace, ns)
		close(f.stopChan[ns])
		delete(f.stopChan, ns)
		delete(f.factories, ns)
		return true
	}
	return false
}

func (f *Factory) namespaces() []string {
	f.mx.RLock()
	defer f.mx.RUnlock()
	namespaces := make([]string, 0, len(f.factories))
	for ns := range f.factories {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}
