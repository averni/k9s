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
	informerStats  sync.Map // map[informers.GenericInformer]*informerStats
	namespaceStats sync.Map // map[string]*informerStats
	mx             sync.RWMutex
}

func newFactoryStats() *factoryStats {
	return &factoryStats{
		lastActive: time.Time{},
	}
}

// Reset resets the factory stats.
func (fs *factoryStats) Reset() {
	fs.mx.Lock()
	defer fs.mx.Unlock()
	fs.lastActive = time.Time{}
	fs.informerStats.Range(func(key, value interface{}) bool {
		fs.informerStats.Delete(key)
		return true
	})
	fs.namespaceStats.Range(func(key, value interface{}) bool {
		fs.namespaceStats.Delete(key)
		return true
	})
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

	if statVal, ok := fs.informerStats.Load(inf); ok {
		stat := statVal.(*informerStats)
		stat.lastActive = lastActive
	} else {
		newStat := &informerStats{
			synced:     false,
			lastActive: lastActive,
			namespace:  ns,
			gvr:        gvr,
			errors:     0,
		}
		fs.informerStats.Store(inf, newStat)
		inf.Informer().SetWatchErrorHandler(func(r *cache.Reflector, err error) {
			fs.mx.Lock()
			defer fs.mx.Unlock()
			slog.Warn("Informer watch error", slogs.GVR, gvr, slogs.Namespace, ns, slogs.Error, err)
			slog.Debug("Informer watch error", slogs.GVR, gvr, slogs.Namespace, ns, "informer", inf, "stats", newStat)

			if statVal, ok := fs.informerStats.Load(inf); ok {
				statVal.(*informerStats).errors++
			}
			if nsStatVal, ok := fs.namespaceStats.Load(ns); ok {
				nsStatVal.(*informerStats).errors++
			}
		})
	}
	if statVal, ok := fs.namespaceStats.Load(ns); ok {
		stat := statVal.(*informerStats)
		stat.lastActive = lastActive
	} else {
		fs.namespaceStats.Store(ns, &informerStats{
			synced:     false,
			lastActive: lastActive,
			namespace:  ns,
			gvr:        gvr,
			errors:     0,
		})
	}
}

// FactoryStopped stops and removes a namespaced factory.
func (fs *factoryStats) FactoryStopped(ns string) {
	fs.mx.Lock()
	defer fs.mx.Unlock()

	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}

	fs.informerStats.Range(func(key, value interface{}) bool {
		stat := value.(*informerStats)
		if stat.namespace == ns {
			fs.informerStats.Delete(key)
		}
		return true
	})
	fs.namespaceStats.Delete(ns)
}

func (fs *factoryStats) InformerStopped(inf informers.GenericInformer) {
	fs.mx.Lock()
	defer fs.mx.Unlock()

	if statVal, ok := fs.informerStats.Load(inf); ok {
		stat := statVal.(*informerStats)
		fs.informerStats.Delete(inf)
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
	if statVal, ok := fs.informerStats.Load(inf); ok {
		stat := statVal.(*informerStats)
		stat.synced = true
		if nsStatVal, ok := fs.namespaceStats.Load(stat.namespace); ok {
			nsStatVal.(*informerStats).synced = true
		}
	}
}

// HasSynced checks if a given informer has been synced.
func (fs *factoryStats) HasSynced(inf informers.GenericInformer) bool {
	fs.mx.RLock()
	defer fs.mx.RUnlock()

	hasSynced := false
	if statVal, ok := fs.informerStats.Load(inf); ok {
		hasSynced = statVal.(*informerStats).synced
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
	if statVal, ok := fs.informerStats.Load(inf); ok {
		stat := statVal.(*informerStats)
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
	isIdle := true
	fs.informerStats.Range(func(key, value interface{}) bool {
		stat := value.(*informerStats)
		if stat.namespace == ns {
			if time.Since(stat.lastActive) < idleTimeout {
				isIdle = false
				return false // stop iteration
			}
		}
		return true
	})
	return isIdle
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
		fm.stats.namespaceStats.Range(func(key, value interface{}) bool {
			ns := key.(string)
			stat := value.(*informerStats)
			slog.Debug("Factory stats", slogs.Namespace, ns, "lastActive", stat.lastActive.Truncate(time.Second).Format(time.RFC3339))
			return true
		})
		fm.stats.informerStats.Range(func(key, value interface{}) bool {
			inf := key.(informers.GenericInformer)
			stats := value.(*informerStats)
			if inf == nil || stats == nil {
				slog.Error("No stats for informer", slogs.GVR, inf)
				return true
			}
			slog.Debug("Informer stats", slogs.GVR, inf, "namespace", stats.namespace, "lastActive", stats.lastActive.Truncate(time.Second).Format(time.RFC3339), "synced", stats.synced, "errors", stats.errors)
			return true
		})
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
	metrics sync.Map // map[informers.GenericInformer]*informerMetrics
}

func newInformerMetricsMap() *informerMetricsMap {
	return &informerMetricsMap{}
}

// Instrument the informer by adding event handlers to it. If the informer
// is already instrumented, it will not be instrumented again to avoid
// double counting of events.
func (m *informerMetricsMap) Instrument(gvr, namespace string, inf informers.GenericInformer) {
	if _, ok := m.metrics.Load(inf); ok {
		return
	}

	m.mx.Lock()
	defer m.mx.Unlock()

	// Double-check after acquiring lock
	if _, ok := m.metrics.Load(inf); ok {
		return
	}

	metrics := newInformerMetrics(gvr, namespace)
	metrics.Instrument(inf)
	m.metrics.Store(inf, metrics)
}

func (m *informerMetricsMap) Reset() {
	m.mx.Lock()
	defer m.mx.Unlock()
	// Reset all metrics
	m.metrics.Range(func(key, value interface{}) bool {
		inf := key.(informers.GenericInformer)
		metrics := value.(*informerMetrics)
		if metrics.handlerReg != nil {
			inf.Informer().RemoveEventHandler(metrics.handlerReg)
			metrics.handlerReg = nil
		}
		if metrics != nil {
			metrics.Reset()
		}
		m.metrics.Delete(key)
		return true
	})
}

func (m *informerMetricsMap) InformerStopped(inf informers.GenericInformer) {
	m.mx.Lock()
	defer m.mx.Unlock()

	if metricsVal, ok := m.metrics.Load(inf); ok {
		metrics := metricsVal.(*informerMetrics)
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

	count := 0
	m.metrics.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	slog.Debug("----------- INFORMERS METRICS -------------")
	slog.Debug(fmt.Sprintf("Informers count: %d", count))
	m.metrics.Range(func(key, value interface{}) bool {
		inf := key.(informers.GenericInformer)
		metrics := value.(*informerMetrics)
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
		return true
	})
	slog.Debug("-------------------------------------------")
}

func (inf *informerMetricsMap) Remove(infToRemove informers.GenericInformer) {
	if _, ok := inf.metrics.Load(infToRemove); ok {
		inf.mx.Lock()
		defer inf.mx.Unlock()
		inf.metrics.Delete(infToRemove)
	}
}
