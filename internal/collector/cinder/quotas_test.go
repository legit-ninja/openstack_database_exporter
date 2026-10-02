package cinder

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
	cinderdb "github.com/vexxhost/openstackdb/cinder/db"
)

func TestQuotasCollector(t *testing.T) {
	quotaCols := []string{"project_id", "resource", "hard_limit", "in_use"}

	type quotasTestCase struct {
		Name            string
		SetupMock       func(sqlmock.Sqlmock)
		ExpectedMetrics string
		ExpectError     bool
	}

	tests := []quotasTestCase{
		{
			Name: "successful collection with explicit count quotas",
			SetupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(quotaCols).
					AddRow("proj-abc", "volumes", 20, 3).
					AddRow("proj-abc", "snapshots", 15, 1).
					AddRow("proj-abc", "backups", 5, 2).
					AddRow("proj-abc", "gigabytes", 1000, 100) // ignored
				mock.ExpectQuery(regexp.QuoteMeta(cinderdb.QuotaGetAllWithUsage)).WillReturnRows(rows)
			},
			ExpectedMetrics: `# HELP openstack_cinder_quota_backups quota_backups
# TYPE openstack_cinder_quota_backups gauge
openstack_cinder_quota_backups{tenant="proj-abc",tenant_id="proj-abc",type="in_use"} 2
openstack_cinder_quota_backups{tenant="proj-abc",tenant_id="proj-abc",type="limit"} 5
openstack_cinder_quota_backups{tenant="proj-abc",tenant_id="proj-abc",type="reserved"} 0
# HELP openstack_cinder_quota_snapshots quota_snapshots
# TYPE openstack_cinder_quota_snapshots gauge
openstack_cinder_quota_snapshots{tenant="proj-abc",tenant_id="proj-abc",type="in_use"} 1
openstack_cinder_quota_snapshots{tenant="proj-abc",tenant_id="proj-abc",type="limit"} 15
openstack_cinder_quota_snapshots{tenant="proj-abc",tenant_id="proj-abc",type="reserved"} 0
# HELP openstack_cinder_quota_volumes quota_volumes
# TYPE openstack_cinder_quota_volumes gauge
openstack_cinder_quota_volumes{tenant="proj-abc",tenant_id="proj-abc",type="in_use"} 3
openstack_cinder_quota_volumes{tenant="proj-abc",tenant_id="proj-abc",type="limit"} 20
openstack_cinder_quota_volumes{tenant="proj-abc",tenant_id="proj-abc",type="reserved"} 0
`,
		},
		{
			Name: "defaults when only gigabytes quota row exists",
			SetupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(quotaCols).
					AddRow("proj-1", "gigabytes", 1000, 50)
				mock.ExpectQuery(regexp.QuoteMeta(cinderdb.QuotaGetAllWithUsage)).WillReturnRows(rows)
			},
			ExpectedMetrics: `# HELP openstack_cinder_quota_backups quota_backups
# TYPE openstack_cinder_quota_backups gauge
openstack_cinder_quota_backups{tenant="proj-1",tenant_id="proj-1",type="in_use"} 0
openstack_cinder_quota_backups{tenant="proj-1",tenant_id="proj-1",type="limit"} 10
openstack_cinder_quota_backups{tenant="proj-1",tenant_id="proj-1",type="reserved"} 0
# HELP openstack_cinder_quota_snapshots quota_snapshots
# TYPE openstack_cinder_quota_snapshots gauge
openstack_cinder_quota_snapshots{tenant="proj-1",tenant_id="proj-1",type="in_use"} 0
openstack_cinder_quota_snapshots{tenant="proj-1",tenant_id="proj-1",type="limit"} 10
openstack_cinder_quota_snapshots{tenant="proj-1",tenant_id="proj-1",type="reserved"} 0
# HELP openstack_cinder_quota_volumes quota_volumes
# TYPE openstack_cinder_quota_volumes gauge
openstack_cinder_quota_volumes{tenant="proj-1",tenant_id="proj-1",type="in_use"} 0
openstack_cinder_quota_volumes{tenant="proj-1",tenant_id="proj-1",type="limit"} 10
openstack_cinder_quota_volumes{tenant="proj-1",tenant_id="proj-1",type="reserved"} 0
`,
		},
		{
			Name: "unlimited limit (-1) and partial resources",
			SetupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(quotaCols).
					AddRow("proj-u", "volumes", -1, 7).
					AddRow("proj-u", "backups", 10, 0)
				mock.ExpectQuery(regexp.QuoteMeta(cinderdb.QuotaGetAllWithUsage)).WillReturnRows(rows)
			},
			ExpectedMetrics: `# HELP openstack_cinder_quota_backups quota_backups
# TYPE openstack_cinder_quota_backups gauge
openstack_cinder_quota_backups{tenant="proj-u",tenant_id="proj-u",type="in_use"} 0
openstack_cinder_quota_backups{tenant="proj-u",tenant_id="proj-u",type="limit"} 10
openstack_cinder_quota_backups{tenant="proj-u",tenant_id="proj-u",type="reserved"} 0
# HELP openstack_cinder_quota_snapshots quota_snapshots
# TYPE openstack_cinder_quota_snapshots gauge
openstack_cinder_quota_snapshots{tenant="proj-u",tenant_id="proj-u",type="in_use"} 0
openstack_cinder_quota_snapshots{tenant="proj-u",tenant_id="proj-u",type="limit"} 10
openstack_cinder_quota_snapshots{tenant="proj-u",tenant_id="proj-u",type="reserved"} 0
# HELP openstack_cinder_quota_volumes quota_volumes
# TYPE openstack_cinder_quota_volumes gauge
openstack_cinder_quota_volumes{tenant="proj-u",tenant_id="proj-u",type="in_use"} 7
openstack_cinder_quota_volumes{tenant="proj-u",tenant_id="proj-u",type="limit"} -1
openstack_cinder_quota_volumes{tenant="proj-u",tenant_id="proj-u",type="reserved"} 0
`,
		},
		{
			Name: "empty results",
			SetupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(cinderdb.QuotaGetAllWithUsage)).WillReturnRows(sqlmock.NewRows(quotaCols))
			},
			ExpectedMetrics: "",
		},
		{
			Name: "null project_id",
			SetupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(quotaCols).
					AddRow(nil, "volumes", 10, 1)
				mock.ExpectQuery(regexp.QuoteMeta(cinderdb.QuotaGetAllWithUsage)).WillReturnRows(rows)
			},
			ExpectedMetrics: `# HELP openstack_cinder_quota_backups quota_backups
# TYPE openstack_cinder_quota_backups gauge
openstack_cinder_quota_backups{tenant="",tenant_id="",type="in_use"} 0
openstack_cinder_quota_backups{tenant="",tenant_id="",type="limit"} 10
openstack_cinder_quota_backups{tenant="",tenant_id="",type="reserved"} 0
# HELP openstack_cinder_quota_snapshots quota_snapshots
# TYPE openstack_cinder_quota_snapshots gauge
openstack_cinder_quota_snapshots{tenant="",tenant_id="",type="in_use"} 0
openstack_cinder_quota_snapshots{tenant="",tenant_id="",type="limit"} 10
openstack_cinder_quota_snapshots{tenant="",tenant_id="",type="reserved"} 0
# HELP openstack_cinder_quota_volumes quota_volumes
# TYPE openstack_cinder_quota_volumes gauge
openstack_cinder_quota_volumes{tenant="",tenant_id="",type="in_use"} 1
openstack_cinder_quota_volumes{tenant="",tenant_id="",type="limit"} 10
openstack_cinder_quota_volumes{tenant="",tenant_id="",type="reserved"} 0
`,
		},
		{
			Name: "query error",
			SetupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(cinderdb.QuotaGetAllWithUsage)).WillReturnError(sql.ErrConnDone)
			},
			ExpectedMetrics: "",
			ExpectError:     true,
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
				if tt.ExpectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			} else {
				problems, err := testutil.CollectAndLint(collector)
				assert.Len(t, problems, 0)
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
