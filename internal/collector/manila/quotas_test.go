package manila

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
	maniladb "github.com/vexxhost/openstackdb/manila/db"
)

func TestQuotasCollector(t *testing.T) {
	limitsCols := []string{"project_id", "resource", "hard_limit"}
	usageCols := []string{"project_id", "resource", "in_use", "reserved"}

	tests := []struct {
		Name            string
		SetupMock       func(sqlmock.Sqlmock)
		ExpectedMetrics string
	}{
		{
			Name: "full in_use/reserved/limit for tracked resources",
			SetupMock: func(mock sqlmock.Sqlmock) {
				limits := sqlmock.NewRows(limitsCols).
					AddRow("proj-1", "shares", 40).
					AddRow("proj-1", "gigabytes", 500).
					AddRow("proj-1", "snapshots", 20).
					AddRow("proj-1", "snapshot_gigabytes", 200).
					AddRow("proj-1", "share_networks", 5).
					AddRow("proj-1", "share_groups", 10) // ignored
				usages := sqlmock.NewRows(usageCols).
					AddRow("proj-1", "shares", 3, 1).
					AddRow("proj-1", "gigabytes", 100, 0).
					AddRow("proj-1", "snapshots", 2, 0).
					AddRow("proj-1", "snapshot_gigabytes", 50, 0).
					AddRow("proj-1", "share_networks", 1, 0)
				mock.ExpectQuery(regexp.QuoteMeta(maniladb.QuotaGetProjectLimits)).WillReturnRows(limits)
				mock.ExpectQuery(regexp.QuoteMeta(maniladb.QuotaUsageGetAll)).WillReturnRows(usages)
			},
			ExpectedMetrics: `# HELP openstack_sharev2_quota_gigabytes quota_gigabytes
# TYPE openstack_sharev2_quota_gigabytes gauge
openstack_sharev2_quota_gigabytes{tenant="proj-1",tenant_id="proj-1",type="in_use"} 100
openstack_sharev2_quota_gigabytes{tenant="proj-1",tenant_id="proj-1",type="limit"} 500
openstack_sharev2_quota_gigabytes{tenant="proj-1",tenant_id="proj-1",type="reserved"} 0
# HELP openstack_sharev2_quota_share_networks quota_share_networks
# TYPE openstack_sharev2_quota_share_networks gauge
openstack_sharev2_quota_share_networks{tenant="proj-1",tenant_id="proj-1",type="in_use"} 1
openstack_sharev2_quota_share_networks{tenant="proj-1",tenant_id="proj-1",type="limit"} 5
openstack_sharev2_quota_share_networks{tenant="proj-1",tenant_id="proj-1",type="reserved"} 0
# HELP openstack_sharev2_quota_shares quota_shares
# TYPE openstack_sharev2_quota_shares gauge
openstack_sharev2_quota_shares{tenant="proj-1",tenant_id="proj-1",type="in_use"} 3
openstack_sharev2_quota_shares{tenant="proj-1",tenant_id="proj-1",type="limit"} 40
openstack_sharev2_quota_shares{tenant="proj-1",tenant_id="proj-1",type="reserved"} 1
# HELP openstack_sharev2_quota_snapshot_gigabytes quota_snapshot_gigabytes
# TYPE openstack_sharev2_quota_snapshot_gigabytes gauge
openstack_sharev2_quota_snapshot_gigabytes{tenant="proj-1",tenant_id="proj-1",type="in_use"} 50
openstack_sharev2_quota_snapshot_gigabytes{tenant="proj-1",tenant_id="proj-1",type="limit"} 200
openstack_sharev2_quota_snapshot_gigabytes{tenant="proj-1",tenant_id="proj-1",type="reserved"} 0
# HELP openstack_sharev2_quota_snapshots quota_snapshots
# TYPE openstack_sharev2_quota_snapshots gauge
openstack_sharev2_quota_snapshots{tenant="proj-1",tenant_id="proj-1",type="in_use"} 2
openstack_sharev2_quota_snapshots{tenant="proj-1",tenant_id="proj-1",type="limit"} 20
openstack_sharev2_quota_snapshots{tenant="proj-1",tenant_id="proj-1",type="reserved"} 0
`,
		},
		{
			Name: "defaults when usage Soft-only Soft-",
			SetupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(maniladb.QuotaGetProjectLimits)).WillReturnRows(sqlmock.NewRows(limitsCols))
				usages := sqlmock.NewRows(usageCols).
					AddRow("proj-2", "shares", 4, 0)
				mock.ExpectQuery(regexp.QuoteMeta(maniladb.QuotaUsageGetAll)).WillReturnRows(usages)
			},
			ExpectedMetrics: `# HELP openstack_sharev2_quota_gigabytes quota_gigabytes
# TYPE openstack_sharev2_quota_gigabytes gauge
openstack_sharev2_quota_gigabytes{tenant="proj-2",tenant_id="proj-2",type="in_use"} 0
openstack_sharev2_quota_gigabytes{tenant="proj-2",tenant_id="proj-2",type="limit"} 1000
openstack_sharev2_quota_gigabytes{tenant="proj-2",tenant_id="proj-2",type="reserved"} 0
# HELP openstack_sharev2_quota_share_networks quota_share_networks
# TYPE openstack_sharev2_quota_share_networks gauge
openstack_sharev2_quota_share_networks{tenant="proj-2",tenant_id="proj-2",type="in_use"} 0
openstack_sharev2_quota_share_networks{tenant="proj-2",tenant_id="proj-2",type="limit"} 10
openstack_sharev2_quota_share_networks{tenant="proj-2",tenant_id="proj-2",type="reserved"} 0
# HELP openstack_sharev2_quota_shares quota_shares
# TYPE openstack_sharev2_quota_shares gauge
openstack_sharev2_quota_shares{tenant="proj-2",tenant_id="proj-2",type="in_use"} 4
openstack_sharev2_quota_shares{tenant="proj-2",tenant_id="proj-2",type="limit"} 50
openstack_sharev2_quota_shares{tenant="proj-2",tenant_id="proj-2",type="reserved"} 0
# HELP openstack_sharev2_quota_snapshot_gigabytes quota_snapshot_gigabytes
# TYPE openstack_sharev2_quota_snapshot_gigabytes gauge
openstack_sharev2_quota_snapshot_gigabytes{tenant="proj-2",tenant_id="proj-2",type="in_use"} 0
openstack_sharev2_quota_snapshot_gigabytes{tenant="proj-2",tenant_id="proj-2",type="limit"} 1000
openstack_sharev2_quota_snapshot_gigabytes{tenant="proj-2",tenant_id="proj-2",type="reserved"} 0
# HELP openstack_sharev2_quota_snapshots quota_snapshots
# TYPE openstack_sharev2_quota_snapshots gauge
openstack_sharev2_quota_snapshots{tenant="proj-2",tenant_id="proj-2",type="in_use"} 0
openstack_sharev2_quota_snapshots{tenant="proj-2",tenant_id="proj-2",type="limit"} 50
openstack_sharev2_quota_snapshots{tenant="proj-2",tenant_id="proj-2",type="reserved"} 0
`,
		},
		{
			Name: "empty",
			SetupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(maniladb.QuotaGetProjectLimits)).WillReturnRows(sqlmock.NewRows(limitsCols))
				mock.ExpectQuery(regexp.QuoteMeta(maniladb.QuotaUsageGetAll)).WillReturnRows(sqlmock.NewRows(usageCols))
			},
			ExpectedMetrics: "",
		},
		{
			Name: "query error",
			SetupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(maniladb.QuotaGetProjectLimits)).WillReturnError(sql.ErrConnDone)
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
