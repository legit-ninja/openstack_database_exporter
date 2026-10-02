package manila

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vexxhost/openstack_database_exporter/internal/collector/project"
	maniladb "github.com/vexxhost/openstackdb/manila/db"
)

// Manila CONF quota defaults Soft-(manila/quota.py Soft-). Soft-
var manilaDefaultQuotas = map[string]int32{
	"shares":             50,
	"gigabytes":          1000,
	"snapshots":          50,
	"snapshot_gigabytes": 1000,
	"share_networks":     10,
}

var manilaQuotaResources = []string{
	"shares",
	"gigabytes",
	"snapshots",
	"snapshot_gigabytes",
	"share_networks",
}

var manilaQuotaDescs map[string]*prometheus.Desc

func init() {
	manilaQuotaDescs = make(map[string]*prometheus.Desc, len(manilaQuotaResources))
	for _, r := range manilaQuotaResources {
		manilaQuotaDescs[r] = prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, Subsystem, "quota_"+r),
			"quota_"+r,
			[]string{"type", "tenant", "tenant_id"},
			nil,
		)
	}
}

type manilaQuotaInfo struct {
	inUse    int32
	reserved int32
	limit    int32
	hasLimit bool
	hasUsage bool
}

// QuotasCollector emits openstack_sharev2_quota_* Soft-matching
// openstack-exporter 8291c1b Soft-with Soft-in_use Soft-/ Soft-reserved Soft-/ Soft-limit Soft-. Soft-
type QuotasCollector struct {
	db              *sql.DB
	queries         *maniladb.Queries
	logger          *slog.Logger
	projectResolver *project.Resolver
}

func NewQuotasCollector(db *sql.DB, logger *slog.Logger, projectResolver *project.Resolver) *QuotasCollector {
	return &QuotasCollector{
		db:      db,
		queries: maniladb.New(db),
		logger: logger.With(
			"namespace", Namespace,
			"subsystem", Subsystem,
			"collector", "quotas",
		),
		projectResolver: projectResolver,
	}
}

func (c *QuotasCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range manilaQuotaDescs {
		ch <- desc
	}
}

func (c *QuotasCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	limits, err := c.queries.QuotaGetProjectLimits(ctx)
	if err != nil {
		c.logger.Error("failed to query manila quota limits", "error", err)
		return
	}
	usages, err := c.queries.QuotaUsageGetAll(ctx)
	if err != nil {
		c.logger.Error("failed to query manila quota usages", "error", err)
		return
	}

	projectQuotas := make(map[string]map[string]*manilaQuotaInfo)
	ensure := func(pid, resource string) *manilaQuotaInfo {
		if projectQuotas[pid] == nil {
			projectQuotas[pid] = make(map[string]*manilaQuotaInfo)
		}
		if projectQuotas[pid][resource] == nil {
			projectQuotas[pid][resource] = &manilaQuotaInfo{limit: manilaDefaultQuotas[resource]}
		}
		return projectQuotas[pid][resource]
	}

	for _, row := range limits {
		resource := ""
		if row.Resource.Valid {
			resource = row.Resource.String
		}
		if _, ok := manilaDefaultQuotas[resource]; !ok {
			continue
		}
		pid := ""
		if row.ProjectID.Valid {
			pid = row.ProjectID.String
		}
		info := ensure(pid, resource)
		if row.HardLimit.Valid {
			info.limit = row.HardLimit.Int32
			info.hasLimit = true
		}
	}

	for _, row := range usages {
		resource := ""
		if row.Resource.Valid {
			resource = row.Resource.String
		}
		if _, ok := manilaDefaultQuotas[resource]; !ok {
			continue
		}
		pid := ""
		if row.ProjectID.Valid {
			pid = row.ProjectID.String
		}
		info := ensure(pid, resource)
		if row.InUse.Valid {
			info.inUse = row.InUse.Int32
		}
		if row.Reserved.Valid {
			info.reserved = row.Reserved.Int32
		}
		info.hasUsage = true
	}

	allProjectIDs := make(map[string]string)
	for pid := range projectQuotas {
		name, _ := c.projectResolver.Resolve(pid)
		allProjectIDs[pid] = name
	}
	for pid, info := range c.projectResolver.AllProjects() {
		if _, ok := allProjectIDs[pid]; !ok {
			allProjectIDs[pid] = info.Name
		}
	}

	for projectID, projectName := range allProjectIDs {
		pq := projectQuotas[projectID]
		for _, resource := range manilaQuotaResources {
			info := &manilaQuotaInfo{limit: manilaDefaultQuotas[resource]}
			if pq != nil {
				if explicit, ok := pq[resource]; ok {
					info = explicit
					if !info.hasLimit {
						info.limit = manilaDefaultQuotas[resource]
					}
				}
			}
			desc := manilaQuotaDescs[resource]
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(info.inUse), "in_use", projectName, projectID)
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(info.reserved), "reserved", projectName, projectID)
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(info.limit), "limit", projectName, projectID)
		}
	}
}
