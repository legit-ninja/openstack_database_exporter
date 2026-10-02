package cinder

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vexxhost/openstack_database_exporter/internal/collector/project"
	cinderdb "github.com/vexxhost/openstackdb/cinder/db"
)

// Cinder default count quotas (CONF.quota_*). Matches OpenStack Cinder defaults
// and the API exporter fixtures (limit 10 when unset).
const (
	cinderDefaultVolumesQuota   = 10
	cinderDefaultSnapshotsQuota = 10
	cinderDefaultBackupsQuota   = 10
)

// countQuotaResources are the per-project count quotas ported from
// openstack-exporter feat/tenant-quota-collectors (11c0dc6).
var countQuotaResources = []string{"volumes", "snapshots", "backups"}

var countQuotaDefaults = map[string]int32{
	"volumes":   cinderDefaultVolumesQuota,
	"snapshots": cinderDefaultSnapshotsQuota,
	"backups":   cinderDefaultBackupsQuota,
}

var countQuotaDescs map[string]*prometheus.Desc

func init() {
	countQuotaDescs = make(map[string]*prometheus.Desc, len(countQuotaResources))
	for _, r := range countQuotaResources {
		countQuotaDescs[r] = prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, Subsystem, "quota_"+r),
			"quota_"+r,
			[]string{"type", "tenant", "tenant_id"},
			nil,
		)
	}
}

type countQuotaInfo struct {
	inUse int32
	limit int32
	has   bool
}

// QuotasCollector emits openstack_cinder_quota_{volumes,snapshots,backups}
// with type in_use|reserved|limit, matching the API exporter layout.
//
// Soft-glance notes:
//   - Uses openstackdb QuotaGetAllWithUsage (unfiltered join of quotas +
//     quota_usages). reserved is not selected by that query → always 0,
//     same Soft-parity as the Nova DB quotas collector.
//   - Projects without an explicit quotas row get Cinder defaults and
//     in_use=0 (usage-only rows are Soft-invisible until openstackdb exposes
//     an unfiltered quota_usages query).
//   - Source commit 296f2f3 (skip failing projects in API loops) is N/A:
//     this collector is bulk SQL; on query failure we log+return like other
//     collectors.
type QuotasCollector struct {
	db              *sql.DB
	queries         *cinderdb.Queries
	logger          *slog.Logger
	projectResolver *project.Resolver
}

func NewQuotasCollector(db *sql.DB, logger *slog.Logger, projectResolver *project.Resolver) *QuotasCollector {
	return &QuotasCollector{
		db:      db,
		queries: cinderdb.New(db),
		logger: logger.With(
			"namespace", Namespace,
			"subsystem", Subsystem,
			"collector", "quotas",
		),
		projectResolver: projectResolver,
	}
}

func (c *QuotasCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range countQuotaDescs {
		ch <- desc
	}
}

func (c *QuotasCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	rows, err := c.queries.QuotaGetAllWithUsage(ctx)
	if err != nil {
		c.logger.Error("failed to query quotas with usage", "error", err)
		return
	}

	// projectID -> resource -> info
	projectQuotas := make(map[string]map[string]*countQuotaInfo)
	for _, row := range rows {
		pid := ""
		if row.ProjectID.Valid {
			pid = row.ProjectID.String
		}
		// Track every project that appears in quotas (even gigabytes-only)
		// so Soft-defaults still emit for count resources.
		if projectQuotas[pid] == nil {
			projectQuotas[pid] = make(map[string]*countQuotaInfo)
		}
		resource := row.Resource
		if _, ok := countQuotaDefaults[resource]; !ok {
			continue
		}
		limit := countQuotaDefaults[resource]
		if row.HardLimit.Valid {
			limit = row.HardLimit.Int32
		}
		projectQuotas[pid][resource] = &countQuotaInfo{
			inUse: row.InUse,
			limit: limit,
			has:   true,
		}
	}

	allProjectIDs := make(map[string]string) // projectID -> projectName
	for pid := range projectQuotas {
		name, _ := c.projectResolver.Resolve(pid)
		allProjectIDs[pid] = name
	}
	for pid, info := range c.projectResolver.AllProjects() {
		if _, exists := allProjectIDs[pid]; !exists {
			allProjectIDs[pid] = info.Name
		}
	}

	for projectID, projectName := range allProjectIDs {
		pq := projectQuotas[projectID]
		for _, resource := range countQuotaResources {
			info := &countQuotaInfo{limit: countQuotaDefaults[resource]}
			if pq != nil {
				if explicit, ok := pq[resource]; ok && explicit.has {
					info = explicit
				}
			}
			desc := countQuotaDescs[resource]
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(info.inUse), "in_use", projectName, projectID)
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(0), "reserved", projectName, projectID)
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(info.limit), "limit", projectName, projectID)
		}
	}
}
