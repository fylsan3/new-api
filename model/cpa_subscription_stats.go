package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CpaUsageHourly aggregates the CPA usage queue per account, model and hour.
// CostUSD is an estimate at New API base prices (no group ratio); it is a
// display figure and never used for billing.
type CpaUsageHourly struct {
	ID                    int     `json:"-" gorm:"primaryKey"`
	InstanceID            int     `json:"instance_id" gorm:"uniqueIndex:idx_cpa_usage_hourly_key,priority:1;not null"`
	AuthIndex             string  `json:"auth_index" gorm:"type:varchar(64);uniqueIndex:idx_cpa_usage_hourly_key,priority:2;not null"`
	ModelName             string  `json:"model_name" gorm:"type:varchar(128);uniqueIndex:idx_cpa_usage_hourly_key,priority:3;not null"`
	BucketTs              int64   `json:"bucket_ts" gorm:"bigint;uniqueIndex:idx_cpa_usage_hourly_key,priority:4;index:idx_cpa_usage_hourly_ts;not null"`
	Requests              int64   `json:"requests" gorm:"bigint"`
	Failed                int64   `json:"failed" gorm:"bigint"`
	InputTokens           int64   `json:"input_tokens" gorm:"bigint"`
	OutputTokens          int64   `json:"output_tokens" gorm:"bigint"`
	CacheReadTokens       int64   `json:"cache_read_tokens" gorm:"bigint"`
	CacheCreationTokens   int64   `json:"cache_creation_tokens" gorm:"bigint"`
	CacheCreation1hTokens int64   `json:"cache_creation_1h_tokens" gorm:"column:cache_creation_1h_tokens;bigint"`
	LatencySumMs          int64   `json:"latency_sum_ms" gorm:"bigint"`
	CostUSD               float64 `json:"cost_usd"`
	UnpricedRequests      int64   `json:"unpriced_requests" gorm:"bigint"`
}

func (CpaUsageHourly) TableName() string { return "cpa_usage_hourly" }

// TotalTokens is every token the account processed in the bucket.
func (u *CpaUsageHourly) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
}

// CpaQuotaPoint is one observed utilization sample used for quota trends.
type CpaQuotaPoint struct {
	ID         int      `json:"-" gorm:"primaryKey"`
	InstanceID int      `json:"instance_id" gorm:"index:idx_cpa_quota_point_key,priority:1;not null"`
	Name       string   `json:"name" gorm:"type:varchar(255);index:idx_cpa_quota_point_key,priority:2;not null"`
	Ts         int64    `json:"ts" gorm:"bigint;index:idx_cpa_quota_point_key,priority:3;index:idx_cpa_quota_point_ts;not null"`
	FiveHour   *float64 `json:"five_hour"`
	SevenDay   *float64 `json:"seven_day"`
}

func (CpaQuotaPoint) TableName() string { return "cpa_quota_point" }

// CpaAlert stays visible until a root user acknowledges it.
type CpaAlert struct {
	ID             int    `json:"id" gorm:"primaryKey"`
	InstanceID     int    `json:"instance_id" gorm:"index;not null"`
	Name           string `json:"name" gorm:"type:varchar(255)"`
	Kind           string `json:"kind" gorm:"type:varchar(64);index"`
	Message        string `json:"message" gorm:"type:varchar(1024)"`
	Count          int    `json:"count"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint"`
	LastSeenAt     int64  `json:"last_seen_at" gorm:"bigint"`
	AcknowledgedAt int64  `json:"acknowledged_at" gorm:"bigint;index"`
	AcknowledgedBy int    `json:"acknowledged_by"`
}

func (CpaAlert) TableName() string { return "cpa_alert" }

// AddCpaUsageHourly adds one aggregated delta, creating the bucket if needed.
func AddCpaUsageHourly(row *CpaUsageHourly) error {
	if row == nil || row.Requests == 0 {
		return nil
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "instance_id"}, {Name: "auth_index"}, {Name: "model_name"}, {Name: "bucket_ts"}},
		DoUpdates: clause.Assignments(map[string]any{
			"requests":                 gorm.Expr("cpa_usage_hourly.requests + ?", row.Requests),
			"failed":                   gorm.Expr("cpa_usage_hourly.failed + ?", row.Failed),
			"input_tokens":             gorm.Expr("cpa_usage_hourly.input_tokens + ?", row.InputTokens),
			"output_tokens":            gorm.Expr("cpa_usage_hourly.output_tokens + ?", row.OutputTokens),
			"cache_read_tokens":        gorm.Expr("cpa_usage_hourly.cache_read_tokens + ?", row.CacheReadTokens),
			"cache_creation_tokens":    gorm.Expr("cpa_usage_hourly.cache_creation_tokens + ?", row.CacheCreationTokens),
			"cache_creation_1h_tokens": gorm.Expr("cpa_usage_hourly.cache_creation_1h_tokens + ?", row.CacheCreation1hTokens),
			"latency_sum_ms":           gorm.Expr("cpa_usage_hourly.latency_sum_ms + ?", row.LatencySumMs),
			"cost_usd":                 gorm.Expr("cpa_usage_hourly.cost_usd + ?", row.CostUSD),
			"unpriced_requests":        gorm.Expr("cpa_usage_hourly.unpriced_requests + ?", row.UnpricedRequests),
		}),
	}).Create(row).Error
}

// ListCpaUsageHourly returns buckets in [from, to).
func ListCpaUsageHourly(from, to int64) ([]CpaUsageHourly, error) {
	var rows []CpaUsageHourly
	err := DB.Where("bucket_ts >= ? AND bucket_ts < ?", from, to).Order("bucket_ts asc").Find(&rows).Error
	return rows, err
}

func AddCpaQuotaPoint(point *CpaQuotaPoint) error {
	return DB.Create(point).Error
}

// LastCpaQuotaPointTs returns the newest sample time of one account (0 if none).
func LastCpaQuotaPointTs(instanceID int, name string) (int64, error) {
	var point CpaQuotaPoint
	err := DB.Where("instance_id = ? AND name = ?", instanceID, name).Order("ts desc").Limit(1).Find(&point).Error
	return point.Ts, err
}

func ListCpaQuotaPoints(from int64) ([]CpaQuotaPoint, error) {
	var points []CpaQuotaPoint
	err := DB.Where("ts >= ?", from).Order("ts asc").Find(&points).Error
	return points, err
}

// PruneCpaStats deletes usage buckets and quota samples older than the cutoffs.
func PruneCpaStats(usageBefore, quotaBefore int64) error {
	if err := DB.Where("bucket_ts < ?", usageBefore).Delete(&CpaUsageHourly{}).Error; err != nil {
		return err
	}
	return DB.Where("ts < ?", quotaBefore).Delete(&CpaQuotaPoint{}).Error
}

// RaiseCpaAlert opens an alert, or bumps the open one of the same kind.
func RaiseCpaAlert(instanceID int, name, kind, message string) error {
	now := common.GetTimestamp()
	var open CpaAlert
	err := DB.Where("instance_id = ? AND name = ? AND kind = ? AND acknowledged_at = ?", instanceID, name, kind, 0).
		Order("id desc").Limit(1).Find(&open).Error
	if err != nil {
		return err
	}
	if open.ID == 0 {
		return DB.Create(&CpaAlert{
			InstanceID: instanceID, Name: name, Kind: kind, Message: message,
			Count: 1, CreatedAt: now, LastSeenAt: now,
		}).Error
	}
	return DB.Model(&CpaAlert{}).Where("id = ?", open.ID).Updates(map[string]any{
		"message": message, "count": open.Count + 1, "last_seen_at": now,
	}).Error
}

func ListOpenCpaAlerts() ([]CpaAlert, error) {
	var alerts []CpaAlert
	err := DB.Where("acknowledged_at = ?", 0).Order("last_seen_at desc").Limit(200).Find(&alerts).Error
	return alerts, err
}

// AcknowledgeCpaAlerts closes the given alerts, or every open one when ids is empty.
func AcknowledgeCpaAlerts(ids []int, userID int) (int64, error) {
	query := DB.Model(&CpaAlert{}).Where("acknowledged_at = ?", 0)
	if len(ids) > 0 {
		query = query.Where("id IN ?", ids)
	}
	result := query.Updates(map[string]any{"acknowledged_at": common.GetTimestamp(), "acknowledged_by": userID})
	return result.RowsAffected, result.Error
}

// UpdateCpaInstanceUsageState stores collector bookkeeping without touching
// user-edited fields.
func UpdateCpaInstanceUsageState(id int, fields map[string]any) error {
	return DB.Model(&CpaInstance{}).Where("id = ?", id).Updates(fields).Error
}

// CpaChannelRevenue is the hourly quota New API charged on one channel.
type CpaChannelRevenue struct {
	ChannelID int   `gorm:"column:channel_id"`
	CreatedAt int64 `gorm:"column:created_at"`
	Quota     int64 `gorm:"column:quota"`
	Count     int64 `gorm:"column:count"`
}

// ListCpaChannelRevenue reads the hourly quota_data rows of the given channels.
func ListCpaChannelRevenue(channelIDs []int, from, to int64) ([]CpaChannelRevenue, error) {
	rows := make([]CpaChannelRevenue, 0)
	if len(channelIDs) == 0 {
		return rows, nil
	}
	err := DB.Table("quota_data").
		Select("channel_id, created_at, sum(quota) as quota, sum(count) as count").
		Where("channel_id IN ? AND created_at >= ? AND created_at < ?", channelIDs, from, to).
		Group("channel_id, created_at").
		Find(&rows).Error
	return rows, err
}

// CpaChannelRef is the minimal channel view used to map channels to a CPA.
type CpaChannelRef struct {
	ID      int     `json:"id" gorm:"column:id"`
	Name    string  `json:"name" gorm:"column:name"`
	BaseURL *string `json:"-" gorm:"column:base_url"`
}

func ListCpaChannelRefs() ([]CpaChannelRef, error) {
	refs := make([]CpaChannelRef, 0)
	err := DB.Table("channels").Select("id, name, base_url").Find(&refs).Error
	return refs, err
}
