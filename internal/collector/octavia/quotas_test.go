package octavia

import (
	"database/sql"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vexxhost/openstack_database_exporter/internal/collector/project"
	octaviadb "github.com/vexxhost/openstackdb/octavia/db/repositories"
)

func TestQuotasCollector(t *testing.T) {
	quotaCols := []string{
		"project_id", "health_monitor", "listener", "load_balancer",
		"member", "pool", "l7policy", "l7rule",
	}
	countCols := []string{"project_id", "count"}

	tests := []struct {
		Name            string
		SetupMock       func(sqlmock.Sqlmock)
		ExpectedMetrics string
	}{
		{
			Name: "limits and in_use for lb/listener/pool",
			SetupMock: func(mock sqlmock.Sqlmock) {
				quotaRows := sqlmock.NewRows(quotaCols).
					AddRow("proj-1", -1, 20, 5, -1, 10, -1, -1)
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.QuotaGetAll)).WillReturnRows(quotaRows)
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.LoadBalancerCountByProject)).
					WillReturnRows(sqlmock.NewRows(countCols).AddRow("proj-1", 2))
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.ListenerCountByProject)).
					WillReturnRows(sqlmock.NewRows(countCols).AddRow("proj-1", 3))
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.PoolCountByProject)).
					WillReturnRows(sqlmock.NewRows(countCols).AddRow("proj-1", 1))
			},
			ExpectedMetrics: `# HELP openstack_loadbalancer_quota_healthmonitor quota_healthmonitor
# TYPE openstack_loadbalancer_quota_healthmonitor gauge
openstack_loadbalancer_quota_healthmonitor{tenant="proj-1",tenant_id="proj-1",type="limit"} -1
# HELP openstack_loadbalancer_quota_l7policy quota_l7policy
# TYPE openstack_loadbalancer_quota_l7policy gauge
openstack_loadbalancer_quota_l7policy{tenant="proj-1",tenant_id="proj-1",type="limit"} -1
# HELP openstack_loadbalancer_quota_l7rule quota_l7rule
# TYPE openstack_loadbalancer_quota_l7rule gauge
openstack_loadbalancer_quota_l7rule{tenant="proj-1",tenant_id="proj-1",type="limit"} -1
# HELP openstack_loadbalancer_quota_listener quota_listener
# TYPE openstack_loadbalancer_quota_listener gauge
openstack_loadbalancer_quota_listener{tenant="proj-1",tenant_id="proj-1",type="in_use"} 3
openstack_loadbalancer_quota_listener{tenant="proj-1",tenant_id="proj-1",type="limit"} 20
# HELP openstack_loadbalancer_quota_loadbalancer quota_loadbalancer
# TYPE openstack_loadbalancer_quota_loadbalancer gauge
openstack_loadbalancer_quota_loadbalancer{tenant="proj-1",tenant_id="proj-1",type="in_use"} 2
openstack_loadbalancer_quota_loadbalancer{tenant="proj-1",tenant_id="proj-1",type="limit"} 5
# HELP openstack_loadbalancer_quota_member quota_member
# TYPE openstack_loadbalancer_quota_member gauge
openstack_loadbalancer_quota_member{tenant="proj-1",tenant_id="proj-1",type="limit"} -1
# HELP openstack_loadbalancer_quota_pool quota_pool
# TYPE openstack_loadbalancer_quota_pool gauge
openstack_loadbalancer_quota_pool{tenant="proj-1",tenant_id="proj-1",type="in_use"} 1
openstack_loadbalancer_quota_pool{tenant="proj-1",tenant_id="proj-1",type="limit"} 10
`,
		},
		{
			Name: "empty quotas and counts",
			SetupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.QuotaGetAll)).WillReturnRows(sqlmock.NewRows(quotaCols))
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.LoadBalancerCountByProject)).WillReturnRows(sqlmock.NewRows(countCols))
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.ListenerCountByProject)).WillReturnRows(sqlmock.NewRows(countCols))
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.PoolCountByProject)).WillReturnRows(sqlmock.NewRows(countCols))
			},
			ExpectedMetrics: "",
		},
		{
			Name: "query error on quotas",
			SetupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(octaviadb.QuotaGetAll)).WillReturnError(sql.ErrConnDone)
			},
			ExpectedMetrics: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			tt.SetupMock(mock)

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			resolver := project.NewResolver(logger, nil, 0)
			collector := NewQuotasCollector(db, logger, resolver)

			if tt.ExpectedMetrics != "" {
				err = testutil.CollectAndCompare(collector, strings.NewReader(tt.ExpectedMetrics))
				assert.NoError(t, err)
			} else {
				problems, err := testutil.CollectAndLint(collector)
				assert.Len(t, problems, 0)
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
