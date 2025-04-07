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
	// While this strategy works well for k9s for quick k9s sessions or mid-sized clusters,
	// it may not be optimal on larger clusters or long-running sessions,
	// since every informer will keep syncing until the context is closed even if there are no active watchers,
	// causing a higher bandwidth usage without any benefit.
	// The default idle time is set below the default resync time in order to
	// stop idle informers before the first resync kicks in.
	defaultIdleTime      = defaultResync / 100 * 70
	defaultMonitorTime   = 1 * time.Minute
	debugInformerMetrics = true
)

// Factory tracks various resource informers.
type Factory struct {
	factories  sync.Map // map[string]di.DynamicSharedInformerFactory
	client     client.Connection
	forwarders Forwarders
	stopChan   sync.Map // map[string]chan struct{}
	monitor    *factoryMonitor
	mx         sync.RWMutex
}

// NewFactory returns a new informers factory.
func NewFactory(clt client.Connection) *Factory {
	return &Factory{
		client:     clt,
		forwarders: NewForwarders(),
		monitor:    nil,
	}
}

// Start initializes the informers until caller cancels the context.
func (f *Factory) Start(ns string) {
	f.mx.Lock()
	defer f.mx.Unlock()

	if _, ok := f.stopChan.Load(ns); !ok {
		f.stopChan.Store(ns, make(chan struct{}))
	}
	if fact, ok := f.factories.Load(ns); ok {
		if stopCh, ok := f.stopChan.Load(ns); ok {
			fact.(di.DynamicSharedInformerFactory).Start(stopCh.(chan struct{}))
		}
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

	f.stopChan.Range(func(key, value interface{}) bool {
		if stopChan, ok := value.(chan struct{}); ok && stopChan != nil {
			close(stopChan)
		}
		f.stopChan.Delete(key)
		return true
	})

	f.factories.Range(func(key, value interface{}) bool {
		f.factories.Delete(key)
		return true
	})
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

	facVal, ok := f.factories.Load(ns)
	if !ok {
		return
	}
	fac := facVal.(di.DynamicSharedInformerFactory)

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
	f.factories.Range(func(key, value interface{}) bool {
		ns := key.(string)
		fac := value.(di.DynamicSharedInformerFactory)
		if stopChVal, ok := f.stopChan.Load(ns); ok {
			stopCh := stopChVal.(chan struct{})
			m := fac.WaitForCacheSync(stopCh)
			for k, v := range m {
				slog.Debug("CACHE `%q Loaded %t:%s",
					slogs.Namespace, ns,
					slogs.ResGrpVersion, v,
					slogs.ResKind, k,
				)
			}
		}
		return true
	})
}

// Client return the factory connection.
func (f *Factory) Client() client.Connection {
	return f.client
}

// FactoryFor returns a factory for a given namespace.
func (f *Factory) FactoryFor(ns string) di.DynamicSharedInformerFactory {
	if val, ok := f.factories.Load(ns); ok {
		return val.(di.DynamicSharedInformerFactory)
	}
	return nil
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
	for _, namespace := range []string{
		client.BlankNamespace,
		client.NamespaceAll,
		client.ClusterScope,
	} {
		if _, ok := f.factories.Load(namespace); ok {
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
	if stopChVal, ok := f.stopChan.Load(ns); ok {
		fact.Start(stopChVal.(chan struct{}))
	}

	f.monitor.Track(inf, gvr.AsResourceName(), ns)
	return inf, nil
}

func (f *Factory) ensureFactory(ns string) (di.DynamicSharedInformerFactory, error) {
	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}

	if facVal, ok := f.factories.Load(ns); ok {
		return facVal.(di.DynamicSharedInformerFactory), nil
	}

	f.mx.Lock()
	defer f.mx.Unlock()

	// Double-check after acquiring lock
	if facVal, ok := f.factories.Load(ns); ok {
		return facVal.(di.DynamicSharedInformerFactory), nil
	}

	dial, err := f.client.DynDial()
	if err != nil {
		return nil, err
	}
	actualNamespace := ns
	if client.IsClusterWide(ns) {
		actualNamespace = client.BlankNamespace
	}
	newFactory := di.NewFilteredDynamicSharedInformerFactory(
		dial,
		defaultResync,
		actualNamespace,
		nil,
	)
	f.factories.Store(ns, newFactory)
	if _, ok := f.stopChan.Load(ns); !ok {
		f.stopChan.Store(ns, make(chan struct{}))
	}
	return newFactory, nil
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
	if _, ok := f.factories.Load(ns); ok {
		if stopChVal, ok := f.stopChan.Load(ns); !ok {
			slog.Error("No stop channel for ns", slogs.Namespace, ns)
			return false
		} else {
			slog.Debug("Stopping factory for ns", slogs.Namespace, ns)
			close(stopChVal.(chan struct{}))
			f.stopChan.Delete(ns)
		}
		f.factories.Delete(ns)
		return true
	}
	return false
}

func (f *Factory) namespaces() []string {
	namespaces := make([]string, 0)
	f.factories.Range(func(key, value interface{}) bool {
		namespaces = append(namespaces, key.(string))
		return true
	})
	return namespaces
}
