// migration_metrics.go provides Prometheus-style metrics collection for
// MPT-to-binary trie migration, including counters, gauges, and a
// JSON-serializable status endpoint.
package trie

import (
	"encoding/json"
	"sync"
	"time"
)

// Counter is a monotonically increasing metric.
type Counter struct {
	mu    sync.Mutex
	value uint64
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.mu.Lock()
	c.value++
	c.mu.Unlock()
}

// Add increments the counter by n.
func (c *Counter) Add(n uint64) {
	c.mu.Lock()
	c.value += n
	c.mu.Unlock()
}

// Get returns the current counter value.
func (c *Counter) Get() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// Gauge is a metric that can go up and down.
type Gauge struct {
	mu    sync.Mutex
	value float64
}

// Set sets the gauge to a specific value.
func (g *Gauge) Set(v float64) {
	g.mu.Lock()
	g.value = v
	g.mu.Unlock()
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	g.mu.Lock()
	g.value++
	g.mu.Unlock()
}

// Get returns the current gauge value.
func (g *Gauge) Get() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.value
}

// MigrationMetrics holds migration-related counters and gauges.
type MigrationMetrics struct {
	AccountsMigrated        Counter
	StorageMigrated         Counter
	MigrationPercentage     Gauge
	PhaseDuration           time.Duration
	EstimatedBlocksRemaining int
}

// MigrationMetricsCollector wraps an IncrementalMigration and collects
// metrics each time Collect() is called.
type MigrationMetricsCollector struct {
	mu        sync.Mutex
	migration *IncrementalMigration
	metrics   MigrationMetrics
	startTime time.Time
}

// NewMigrationMetricsCollector creates a collector for the given migration.
func NewMigrationMetricsCollector(m *IncrementalMigration) *MigrationMetricsCollector {
	return &MigrationMetricsCollector{
		migration: m,
		startTime: time.Now(),
	}
}

// Collect updates the metrics from the migration state.
func (c *MigrationMetricsCollector) Collect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	migrated, total, pct := c.migration.Progress()
	c.metrics.AccountsMigrated = Counter{value: uint64(migrated)}
	c.metrics.StorageMigrated = Counter{value: uint64(migrated)} // 1:1 for account migration
	c.metrics.MigrationPercentage.Set(pct)
	c.metrics.PhaseDuration = time.Since(c.startTime)

	batchSize := c.migration.config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	remaining := total - migrated
	if remaining > 0 {
		c.metrics.EstimatedBlocksRemaining = (remaining + batchSize - 1) / batchSize
	} else {
		c.metrics.EstimatedBlocksRemaining = 0
	}
}

// Metrics returns a copy of the current metrics.
func (c *MigrationMetricsCollector) Metrics() MigrationMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.metrics
}

// migrationStatusJSON is the JSON representation of migration status.
type migrationStatusJSON struct {
	AccountsMigrated         uint64  `json:"accounts_migrated"`
	StorageMigrated          uint64  `json:"storage_migrated"`
	MigrationPercentage      float64 `json:"migration_percentage"`
	PhaseDurationMs          int64   `json:"phase_duration_ms"`
	EstimatedBlocksRemaining int     `json:"estimated_blocks_remaining"`
	IsDone                   bool    `json:"is_done"`
}

// MigrationStatusJSON returns a JSON-encoded status report of the migration.
func (c *MigrationMetricsCollector) MigrationStatusJSON() ([]byte, error) {
	c.Collect()
	c.mu.Lock()
	defer c.mu.Unlock()

	status := migrationStatusJSON{
		AccountsMigrated:         c.metrics.AccountsMigrated.Get(),
		StorageMigrated:          c.metrics.StorageMigrated.Get(),
		MigrationPercentage:      c.metrics.MigrationPercentage.Get(),
		PhaseDurationMs:          c.metrics.PhaseDuration.Milliseconds(),
		EstimatedBlocksRemaining: c.metrics.EstimatedBlocksRemaining,
		IsDone:                   c.migration.IsDone(),
	}
	return json.Marshal(status)
}
