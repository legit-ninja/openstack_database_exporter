package octavia

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vexxhost/openstack_database_exporter/internal/collector/project"
	octaviadb "github.com/vexxhost/openstackdb/octavia/db/repositories"
)

// Octavia CONF.quotas defaults are QUOTA_UNLIMITED (-1).
const octaviaDefaultQuota = int32(-1)

// Metric resource name -> DB column Soft-mapping for limit overrides.
var octaviaQuotaResources = []struct {
	metric string
	column string
}{
	{"loadbalancer", "load_balancer"},
	{"listener", "listener"},
	{"pool", "pool"},
	{"member", "member"},
	{"healthmonitor", "health_monitor"},
	{"l7policy", "l7policy"},
	{"l7rule", "l7rule"},
}

var octaviaQuotaDescs map[string]*prometheus.Desc

func init() {
	octaviaQuotaDescs = make(map[string]*prometheus.Desc, len(octaviaQuotaResources))
	for _, r := range octaviaQuotaResources {
		octaviaQuotaDescs[r.metric] = prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, Subsystem, "quota_"+r.metric),
			"quota_"+r.metric,
			[]string{"type", "tenant", "tenant_id"},
			nil,
		)
	}
}

// QuotasCollector emits openstack_loadbalancer_quota_* Soft-matching
// openstack-exporter 786a63c. limit Soft-always; in_use for
// loadbalancer/listener/pool Soft-from object counts; no reserved.
type QuotasCollector struct {
	db              *sql.DB
	quotas          *octaviadb.QuotaRepository
	logger          *slog.Logger
	projectResolver *project.Resolver
}

func NewQuotasCollector(db *sql.DB, logger *slog.Logger, projectResolver *project.Resolver) *QuotasCollector {
	return &QuotasCollector{
		db:     db,
		quotas: octaviadb.NewQuotaRepository(db),
		logger: logger.With(
			"namespace", Namespace,
			"subsystem", Subsystem,
			"collector", "quotas",
		),
		projectResolver: projectResolver,
	}
}

func (c *QuotasCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range octaviaQuotaDescs {
		ch <- desc
	}
}

func (c *QuotasCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	quotaRows, err := c.quotas.GetAll(ctx)
	if err != nil {
		c.logger.Error("failed to query octavia quotas", "error", err)
		return
	}

	lbCounts, err := c.quotas.LoadBalancerCountByProject(ctx)
	if err != nil {
		c.logger.Error("failed to count load balancers by project", "error", err)
		return
	}
	listenerCounts, err := c.quotas.ListenerCountByProject(ctx)
	if err != nil {
		c.logger.Error("failed to count listeners by project", "error", err)
		return
	}
	poolCounts, err := c.quotas.PoolCountByProject(ctx)
	if err != nil {
		c.logger.Error("failed to count pools by project", "error", err)
		return
	}

	limitsByProject := make(map[string]map[string]int32)
	hasQuotaRow := make(map[string]bool, len(quotaRows))
	for _, row := range quotaRows {
		hasQuotaRow[row.ProjectID] = true
		limitsByProject[row.ProjectID] = map[string]int32{
			"loadbalancer":  nullIntOrDefault(row.LoadBalancer, octaviaDefaultQuota),
			"listener":      nullIntOrDefault(row.Listener, octaviaDefaultQuota),
			"pool":          nullIntOrDefault(row.Pool, octaviaDefaultQuota),
			"member":        nullIntOrDefault(row.Member, octaviaDefaultQuota),
			"healthmonitor": nullIntOrDefault(row.HealthMonitor, octaviaDefaultQuota),
			"l7policy":      nullIntOrDefault(row.L7policy, octaviaDefaultQuota),
			"l7rule":        nullIntOrDefault(row.L7rule, octaviaDefaultQuota),
		}
	}

	inUse := map[string]map[string]float64{
		"loadbalancer": {},
		"listener":     {},
		"pool":         {},
	}
	for _, row := range lbCounts {
		pid := ""
		if row.ProjectID.Valid {
			pid = row.ProjectID.String
		}
		inUse["loadbalancer"][pid] = float64(row.Count)
	}
	for _, row := range listenerCounts {
		pid := ""
		if row.ProjectID.Valid {
			pid = row.ProjectID.String
		}
		inUse["listener"][pid] = float64(row.Count)
	}
	for _, row := range poolCounts {
		pid := ""
		if row.ProjectID.Valid {
			pid = row.ProjectID.String
		}
		inUse["pool"][pid] = float64(row.Count)
	}

	allProjectIDs := make(map[string]string)
	for pid := range hasQuotaRow {
		name, _ := c.projectResolver.Resolve(pid)
		allProjectIDs[pid] = name
	}
	for _, m := range inUse {
		for pid := range m {
			if _, ok := allProjectIDs[pid]; !ok {
				name, _ := c.projectResolver.Resolve(pid)
				allProjectIDs[pid] = name
			}
		}
	}
	for pid, info := range c.projectResolver.AllProjects() {
		if _, ok := allProjectIDs[pid]; !ok {
			allProjectIDs[pid] = info.Name
		}
	}

	for projectID, projectName := range allProjectIDs {
		for _, r := range octaviaQuotaResources {
			limit := octaviaDefaultQuota
			if lims, ok := limitsByProject[projectID]; ok {
				limit = lims[r.metric]
			}
			desc := octaviaQuotaDescs[r.metric]
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(limit), "limit", projectName, projectID)
			if usage, ok := inUse[r.metric]; ok {
				ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, usage[projectID], "in_use", projectName, projectID)
			}
		}
	}
}

func nullIntOrDefault(v sql.NullInt32, def int32) int32 {
	if v.Valid {
		return v.Int32
	}
	return def
}
