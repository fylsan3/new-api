package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCpaStatsDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			initCol()
			tables := []any{&CpaUsageHourly{}, &CpaQuotaPoint{}, &CpaAlert{}}
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(tables...))
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				initCol()
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.Migrator().DropTable(tables...))
			require.NoError(t, db.AutoMigrate(tables...))

			delta := func() *CpaUsageHourly {
				return &CpaUsageHourly{
					InstanceID: 1, AuthIndex: "a1", ModelName: "claude-x", BucketTs: 3600,
					Requests: 2, Failed: 1, InputTokens: 10, OutputTokens: 20,
					CacheReadTokens: 30, CacheCreationTokens: 40, CacheCreation1hTokens: 5,
					LatencySumMs: 100, CostUSD: 0.25, UnpricedRequests: 1,
				}
			}
			require.NoError(t, AddCpaUsageHourly(delta()))
			require.NoError(t, AddCpaUsageHourly(delta()))
			other := delta()
			other.BucketTs = 7200
			require.NoError(t, AddCpaUsageHourly(other))

			rows, err := ListCpaUsageHourly(0, 7200)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			got := rows[0]
			assert.Equal(t, int64(4), got.Requests)
			assert.Equal(t, int64(2), got.Failed)
			assert.Equal(t, int64(20), got.InputTokens)
			assert.Equal(t, int64(40), got.OutputTokens)
			assert.Equal(t, int64(60), got.CacheReadTokens)
			assert.Equal(t, int64(80), got.CacheCreationTokens)
			assert.Equal(t, int64(10), got.CacheCreation1hTokens)
			assert.Equal(t, int64(200), got.LatencySumMs)
			assert.InDelta(t, 0.5, got.CostUSD, 1e-9)
			assert.Equal(t, int64(2), got.UnpricedRequests)
			assert.Equal(t, int64(200), got.TotalTokens())

			require.NoError(t, PruneCpaStats(7200, 0))
			rows, err = ListCpaUsageHourly(0, 1<<40)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.Equal(t, int64(7200), rows[0].BucketTs)

			require.NoError(t, RaiseCpaAlert(1, "acc", "socks_auto_disabled", "first"))
			require.NoError(t, RaiseCpaAlert(1, "acc", "socks_auto_disabled", "second"))
			require.NoError(t, RaiseCpaAlert(1, "acc", "usage_statistics_off", "off"))
			alerts, err := ListOpenCpaAlerts()
			require.NoError(t, err)
			require.Len(t, alerts, 2)
			for _, alert := range alerts {
				if alert.Kind == "socks_auto_disabled" {
					assert.Equal(t, 2, alert.Count)
					assert.Equal(t, "second", alert.Message)
				}
			}

			affected, err := AcknowledgeCpaAlerts([]int{alerts[0].ID}, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(1), affected)
			affected, err = AcknowledgeCpaAlerts(nil, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(1), affected)
			alerts, err = ListOpenCpaAlerts()
			require.NoError(t, err)
			assert.Empty(t, alerts)

			require.NoError(t, RaiseCpaAlert(1, "acc", "socks_auto_disabled", "again"))
			alerts, err = ListOpenCpaAlerts()
			require.NoError(t, err)
			require.Len(t, alerts, 1)
			assert.Equal(t, 1, alerts[0].Count)

			five := 42.5
			require.NoError(t, AddCpaQuotaPoint(&CpaQuotaPoint{InstanceID: 1, Name: "acc", Ts: 100, FiveHour: &five}))
			require.NoError(t, AddCpaQuotaPoint(&CpaQuotaPoint{InstanceID: 1, Name: "acc", Ts: 200}))
			last, err := LastCpaQuotaPointTs(1, "acc")
			require.NoError(t, err)
			assert.Equal(t, int64(200), last)
			points, err := ListCpaQuotaPoints(0)
			require.NoError(t, err)
			require.Len(t, points, 2)
			require.NotNil(t, points[0].FiveHour)
			assert.InDelta(t, 42.5, *points[0].FiveHour, 1e-9)
			assert.Nil(t, points[1].SevenDay)
		})
	}
}
