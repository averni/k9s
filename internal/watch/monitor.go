package watch

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/slogs"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// informerStats tracks basic informer stats like
// last active time and whether it has been synced.
type informerStats struct {
	synced     bool
	errors     int
	startedAt  time.Time
	lastActive time.Time
	namespace  string
	gvr        string
}

// factoryStats tracks the number of informers
// and the last time it was active.
type factoryStats struct {
	lastActive     time.Time
	informerStats  map[informers.GenericInformer]*informerStats
	namespaceStats map[string]*informerStats
	mx             sync.RWMutex
}

func newFactoryStats() *factoryStats {
	return &factoryStats{
		lastActive:     time.Time{},
		informerStats:  make(map[informers.GenericInformer]*informerStats),
		namespaceStats: make(map[string]*informerStats),
	}
}

// Reset resets the factory stats.
func (fs *factoryStats) Reset() {
	fs.mx.Lock()
	defer fs.mx.Unlock()
	fs.lastActive = time.Time{}
	fs.informerStats = make(map[informers.GenericInformer]*informerStats)
	fs.namespaceStats = make(map[string]*informerStats)
}

// Track tracks the last time a given informer was active.
func (fs *factoryStats) Track(inf informers.GenericInformer, gvr string, ns string) {
	fs.mx.Lock()
	defer fs.mx.Unlock()

	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}

	lastActive := time.Now()
	fs.lastActive = lastActive

	if stat, ok := fs.informerStats[inf]; ok {
		stat.lastActive = lastActive
	} else {
		fs.informerStats[inf] = &informerStats{
			synced:     false,
			lastActive: lastActive,
			namespace:  ns,
			gvr:        gvr,
			errors:     0,
		}
		inf.Informer().SetWatchErrorHandler(func(r *cache.Reflector, err error) {
			fs.mx.Lock()
			defer fs.mx.Unlock()
			slog.Warn("Informer watch error", slogs.GVR, gvr, slogs.Namespace, ns, slogs.Error, err)
			slog.Debug("Informer watch error", slogs.GVR, gvr, slogs.Namespace, ns, "informer", inf, "stats", fs.informerStats[inf])

			fs.informerStats[inf].errors++
			fs.namespaceStats[ns].errors++
		})
	}
	if stat, ok := fs.namespaceStats[ns]; ok {
		stat.lastActive = lastActive
	} else {
		fs.namespaceStats[ns] = &informerStats{
			synced:     false,
			lastActive: lastActive,
			namespace:  ns,
			gvr:        gvr,
			errors:     0,
		}
	}
}

// FactoryStopped stops and removes a namespaced factory.
func (fs *factoryStats) FactoryStopped(ns string) {
	fs.mx.Lock()
	defer fs.mx.Unlock()

	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}

	for k, stat := range fs.informerStats {
		if stat.namespace == ns {
			delete(fs.informerStats, k)
			break
		}
	}
	delete(fs.namespaceStats, ns)
}

func (fs *factoryStats) InformerStopped(inf informers.GenericInformer) {
	fs.mx.Lock()
	defer fs.mx.Unlock()

	if stat, ok := fs.informerStats[inf]; ok {
		delete(fs.informerStats, inf)
		slog.Debug("Stopped informer", slogs.GVR, stat.gvr, slogs.Namespace, stat.namespace)
	}
}

// Synced records that a given informer has been synced.
func (fs *factoryStats) Synced(inf informers.GenericInformer) {
	fs.mx.Lock()
	defer fs.mx.Unlock()

	if inf == nil {
		slog.Warn("Informer is nil")
		return
	}
	if stat, ok := fs.informerStats[inf]; ok {
		stat.synced = true
	}
	if stat, ok := fs.namespaceStats[fs.informerStats[inf].namespace]; ok {
		stat.synced = true
	}
}

// HasSynced checks if a given informer has been synced.
func (fs *factoryStats) HasSynced(inf informers.GenericInformer) bool {
	fs.mx.RLock()
	defer fs.mx.RUnlock()

	hasSynced := false
	if stat, ok := fs.informerStats[inf]; ok {
		hasSynced = stat.synced
	}
	// slog.Debug("Checking if informer has synced: ", slogs.GVR, inf, "informer.hasSynced", inf.Informer().HasSynced(), "controller.hasSynced", inf.Informer().GetController().HasSynced(), "stats.hasSynced", hasSynced)

	return hasSynced
}

func (fs *factoryStats) InformerIdleSince(inf informers.GenericInformer, idleTimeout time.Duration) bool {
	fs.mx.RLock()
	defer fs.mx.RUnlock()

	if fs.lastActive.IsZero() {
		return false
	}
	if stat, ok := fs.informerStats[inf]; ok {
		if !stat.lastActive.IsZero() && time.Since(stat.lastActive) < idleTimeout {
			return false
		}
	}
	return true
}

func (fs *factoryStats) IdleSince(ns string, idleTimeout time.Duration) bool {
	fs.mx.RLock()
	defer fs.mx.RUnlock()
	if fs.lastActive.IsZero() {
		return true
	}
	for _, stat := range fs.informerStats {
		if stat.namespace == ns {
			if time.Since(stat.lastActive) < idleTimeout {
				return false
			}
		}
	}
	return true
}

// factoryMonitor monitors the factory for idle factories.
type factoryMonitor struct {
	factory       *Factory
	stats         *factoryStats
	metrics       *informerMetricsMap
	idleTimeout   time.Duration
	checkInterval time.Duration
	stopChan      chan struct{}
	wg            sync.WaitGroup
	mx            sync.RWMutex
}

func newFactoryMonitor(factory *Factory, idleTimeout, checkInterval time.Duration) *factoryMonitor {
	return &factoryMonitor{
		factory:       factory,
		stats:         newFactoryStats(),
		metrics:       newInformerMetricsMap(),
		idleTimeout:   idleTimeout,
		checkInterval: checkInterval,
		stopChan:      make(chan struct{}),
	}
}

// Start starts the factory monitor loop.
func (fm *factoryMonitor) Start() {
	fm.mx.Lock()
	defer fm.mx.Unlock()

	slog.Debug("factoryMonitor started")
	go fm.monitorLoop()
}

// Synced records that a given informer has been synced.
func (fs *factoryMonitor) Synced(inf informers.GenericInformer) {
	fs.stats.Synced(inf)
}

// HasSynced checks if a given informer has been synced.
func (fs *factoryMonitor) HasSynced(inf informers.GenericInformer) bool {
	return fs.stats.HasSynced(inf)
}

// Stop stops the factory monitor loop.
func (fm *factoryMonitor) Stop() {
	fm.mx.Lock()
	defer fm.mx.Unlock()

	if fm.stopChan != nil {
		slog.Debug("Stopping factoryMonitor")
		fm.stats.Reset()
		fm.metrics.Reset()
		close(fm.stopChan)
		fm.wg.Wait()
		fm.stopChan = nil
	}
}

func (fm *factoryMonitor) Track(inf informers.GenericInformer, gvr string, ns string) {
	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}

	if debugInformerMetrics {
		fm.metrics.Instrument(gvr, ns, inf)
	}

	fm.stats.Track(inf, gvr, ns)
}

// monitorLoop goroutine to periodically check for idle factories.
func (fm *factoryMonitor) monitorLoop() {
	ticker := time.NewTicker(fm.checkInterval)
	fm.wg.Add(1)
	defer fm.wg.Done()
	defer ticker.Stop()

	for {
		select {
		case <-fm.stopChan:
			return
		case <-ticker.C:
			fm.monitor()
		}
	}
}

// monitor checks if any factories are idle and stops them.
func (fm *factoryMonitor) monitor() {
	// evicted := []informers.GenericInformer{}
	// for inf, stats := range fm.stats.informerStats {
	// 	if stats == nil {
	// 		slog.Error("No stats for informer", slogs.GVR, inf)
	// 		continue
	// 	}
	// 	if fm.stats.InformerIdleSince(inf, fm.idleTimeout) {
	// 		slog.Info("Stopping idle informer", slogs.GVR, stats.gvr, slogs.Namespace, stats.namespace, "informer", inf, "idleTimeout", fm.idleTimeout)
	// 		if stopped := fm.factory.stopInformer(inf); stopped {
	// 			evicted = append(evicted, inf)
	// 		}
	// 	}
	// }

	// // Remove evicted informers from the metrics map
	// for _, inf := range evicted {
	// 	fm.stats.InformerStopped(inf)
	// 	fm.metrics.InformerStopped(inf)
	// }

	for _, ns := range fm.factory.namespaces() {
		if fm.stats.IdleSince(ns, fm.idleTimeout) {
			slog.Debug("Stopping idle factory", slogs.Namespace, ns, "idleTimeout", fm.idleTimeout)
			stopped := fm.factory.stopFactory(ns)
			if stopped {
				fm.stats.FactoryStopped(ns)
			}
		}
	}

	if debugInformerMetrics {
		for _, ns := range fm.factory.namespaces() {
			slog.Debug("Factory ns", slogs.Namespace, ns, "lastActive", fm.stats.lastActive.Truncate(time.Second).Format(time.RFC3339))
		}
		for ns, _ := range fm.stats.namespaceStats {
			slog.Debug("Factory stats", slogs.Namespace, ns, "lastActive", fm.stats.namespaceStats[ns].lastActive.Truncate(time.Second).Format(time.RFC3339))
		}
		for inf, stats := range fm.stats.informerStats {
			if inf == nil || stats == nil {
				slog.Error("No stats for informer", slogs.GVR, inf)
				continue
			}
			slog.Debug("Informer stats", slogs.GVR, inf, "namespace", stats.namespace, "lastActive", stats.lastActive.Truncate(time.Second).Format(time.RFC3339), "synced", stats.synced, "errors", stats.errors)
		}
		fm.metrics.Debug()
	}
}

// Basic metrics for informers
// Simple way to know which informers are active and how many
// events they are processing for debugging and performance tuning purposes
// without having to install and activate the prometheus client library
// that should be used for a more complete solution.

// informerMetrics tracks the number of events received by an informer

type informerMetrics struct {
	gvr        string
	namespace  string
	added      int
	updated    int
	deleted    int
	errors     int
	startedAt  time.Time
	lastUpdate time.Time
	stoppedAt  time.Time
	handlerReg cache.ResourceEventHandlerRegistration
	mx         sync.RWMutex
}

func newInformerMetrics(gvr, namespace string) *informerMetrics {
	return &informerMetrics{
		gvr:        gvr,
		namespace:  namespace,
		startedAt:  time.Time{},
		lastUpdate: time.Time{},
		stoppedAt:  time.Time{},
		added:      0,
		updated:    0,
		deleted:    0,
		errors:     0,
		handlerReg: nil,
	}
}

// Instrument the informer by adding event handlers to it.
func (m *informerMetrics) Instrument(inf informers.GenericInformer) {
	m.mx.Lock()
	defer m.mx.Unlock()

	m.startedAt = time.Now()
	handlerReg, err := inf.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.mx.Lock()
			defer m.mx.Unlock()
			m.lastUpdate = time.Now()
			m.added++
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.mx.Lock()
			defer m.mx.Unlock()
			m.lastUpdate = time.Now()
			m.updated++
		},
		DeleteFunc: func(obj interface{}) {
			m.mx.Lock()
			defer m.mx.Unlock()
			m.lastUpdate = time.Now()
			m.deleted++
		},
	})
	if err != nil {
		slog.Error("Failed to add event handler", slogs.GVR, m.gvr, slogs.Namespace, m.namespace, slogs.Error, err)
		return
	}
	m.handlerReg = handlerReg
	inf.Informer().SetWatchErrorHandler(func(r *cache.Reflector, err error) {
		slog.Warn("Informer watch error", slogs.GVR, m.gvr, slogs.Namespace, m.namespace, slogs.Error, err)
		m.mx.Lock()
		defer m.mx.Unlock()
		m.errors++
		m.lastUpdate = time.Now()
	})
}

// Reset the metrics to zero.
func (m *informerMetrics) Reset() {
	m.mx.Lock()
	defer m.mx.Unlock()
	m.added = 0
	m.updated = 0
	m.deleted = 0
	m.errors = 0
	m.stoppedAt = time.Time{}
	m.startedAt = time.Time{}
	m.lastUpdate = time.Time{}
}

// informerMetricsMap holds informers and their metrics.
type informerMetricsMap struct {
	mx      sync.RWMutex
	metrics map[informers.GenericInformer]*informerMetrics // should be a weak map/sync.Map
}

func newInformerMetricsMap() *informerMetricsMap {
	return &informerMetricsMap{
		metrics: make(map[informers.GenericInformer]*informerMetrics),
	}
}

// Instrument the informer by adding event handlers to it. If the informer
// is already instrumented, it will not be instrumented again to avoid
// double counting of events.
func (m *informerMetricsMap) Instrument(gvr, namespace string, inf informers.GenericInformer) {
	m.mx.Lock()
	defer m.mx.Unlock()

	if _, ok := m.metrics[inf]; ok {
		return
	}

	metrics := newInformerMetrics(gvr, namespace)
	metrics.Instrument(inf)
	m.metrics[inf] = metrics
}

func (m *informerMetricsMap) Reset() {
	m.mx.Lock()
	defer m.mx.Unlock()
	// Reset all metrics
	for inf, metrics := range m.metrics {
		if metrics.handlerReg != nil {
			inf.Informer().RemoveEventHandler(metrics.handlerReg)
			metrics.handlerReg = nil
		}
		if metrics != nil {
			metrics.Reset()
		}
	}
	m.metrics = make(map[informers.GenericInformer]*informerMetrics)
}

func (m *informerMetricsMap) InformerStopped(inf informers.GenericInformer) {
	m.mx.Lock()
	defer m.mx.Unlock()

	if metrics, ok := m.metrics[inf]; ok {
		metrics.stoppedAt = time.Now()
		if metrics.handlerReg != nil {
			inf.Informer().RemoveEventHandler(metrics.handlerReg)
			metrics.handlerReg = nil
		}
	}
}

func (m *informerMetricsMap) Debug() {
	m.mx.RLock()
	defer m.mx.RUnlock()

	slog.Debug("----------- INFORMERS METRICS -------------")
	slog.Debug(fmt.Sprintf("Informers count: %d", len(m.metrics)))
	for inf, metrics := range m.metrics {
		slog.Debug(
			fmt.Sprintf("%20v %s", inf, metrics.namespace+"/"+metrics.gvr),
			slog.Int("added", metrics.added),
			slog.Int("updated", metrics.updated),
			slog.Int("deleted", metrics.deleted),
			slog.Int("errors", metrics.errors),
			slog.String("startedAt", metrics.startedAt.Truncate(time.Second).Format(time.RFC3339)),
			slog.String("lastUpdate", metrics.lastUpdate.Truncate(time.Second).Format(time.RFC3339)),
			slog.String("stoppedAt", metrics.stoppedAt.Truncate(time.Second).Format(time.RFC3339)),
		)
	}
	slog.Debug("-------------------------------------------")
}

func (inf *informerMetricsMap) Remove(infToRemove informers.GenericInformer) {
	if _, ok := inf.metrics[infToRemove]; ok {
		inf.mx.Lock()
		defer inf.mx.Unlock()
		delete(inf.metrics, infToRemove)
	}
}
