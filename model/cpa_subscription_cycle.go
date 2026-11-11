package model

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CpaUsageFailHourly counts failed relay requests per account, hour and
// upstream status code (design draft 2026-11-11, "failure reasons").
// Data comes from the fail block of the CPA usage queue records.
type CpaUsageFailHourly struct {
	ID         int    `json:"-" gorm:"primaryKey"`
	InstanceID int    `json:"instance_id" gorm:"uniqueIndex:idx_cpa_fail_hourly_key,priority:1;not null"`
	AuthIndex  string `json:"auth_index" gorm:"type:varchar(64);uniqueIndex:idx_cpa_fail_hourly_key,priority:2;not null"`
	BucketTs   int64  `json:"bucket_ts" gorm:"bigint;uniqueIndex:idx_cpa_fail_hourly_key,priority:3;index:idx_cpa_fail_hourly_ts;not null"`
	StatusCode int    `json:"status_code" gorm:"uniqueIndex:idx_cpa_fail_hourly_key,priority:4;not null"`
	Count      int64  `json:"count" gorm:"column:fail_count;bigint"`
	LastCode   string `json:"last_code" gorm:"type:varchar(64)"`
	LastAt     int64  `json:"last_at" gorm:"bigint"`
}

func (CpaUsageFailHourly) TableName() string { return "cpa_usage_fail_hourly" }

// AddCpaUsageFailHourly adds one aggregated delta, creating the bucket if needed.
func AddCpaUsageFailHourly(row *CpaUsageFailHourly) error {
	if row == nil || row.Count == 0 {
		return nil
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "instance_id"}, {Name: "auth_index"}, {Name: "bucket_ts"}, {Name: "status_code"}},
		DoUpdates: clause.Assignments(map[string]any{
			"fail_count": gorm.Expr("cpa_usage_fail_hourly.fail_count + ?", row.Count),
			"last_code":  row.LastCode,
			"last_at":    row.LastAt,
		}),
	}).Create(row).Error
}

// ListCpaUsageFailHourly returns buckets in [from, to).
func ListCpaUsageFailHourly(from, to int64) ([]CpaUsageFailHourly, error) {
	var rows []CpaUsageFailHourly
	err := DB.Where("bucket_ts >= ? AND bucket_ts < ?", from, to).Order("bucket_ts asc").Find(&rows).Error
	return rows, err
}

// CpaQuotaCycle is one reset period of one weekly quota window of one
// account (design draft 2026-11-11, "cycle comparison"). Kept forever.
// EndReason is empty while the cycle is open.
type CpaQuotaCycle struct {
	ID         int    `json:"id" gorm:"primaryKey"`
	InstanceID int    `json:"instance_id" gorm:"index:idx_cpa_quota_cycle_key,priority:1;not null"`
	Name       string `json:"name" gorm:"type:varchar(255);index:idx_cpa_quota_cycle_key,priority:2;not null"`
	Window     string `json:"window" gorm:"column:quota_window;type:varchar(64);index:idx_cpa_quota_cycle_key,priority:3;not null"`
	AuthIndex  string `json:"auth_index" gorm:"type:varchar(64)"`
	StartTs    int64  `json:"start_ts" gorm:"bigint;index:idx_cpa_quota_cycle_key,priority:4"`
	EndTs      int64  `json:"end_ts" gorm:"bigint"`
	PlanType   string `json:"plan_type" gorm:"type:varchar(64)"`
	// LastPercent is the newest used percent seen inside the cycle.
	LastPercent       *float64 `json:"last_percent"`
	LastPercentAt     int64    `json:"last_percent_at" gorm:"bigint"`
	LastPercentSource string   `json:"last_percent_source" gorm:"type:varchar(16)"`
	// HitLimit: the window was seen exhausted (100% used or rejected).
	HitLimit  bool   `json:"hit_limit"`
	EndReason string `json:"end_reason" gorm:"type:varchar(32);index"`
	// Token totals are frozen when the cycle closes (hourly usage is pruned
	// after 90 days); open cycles are computed live.
	Tokens          int64 `json:"tokens" gorm:"bigint"`
	CacheReadTokens int64 `json:"cache_read_tokens" gorm:"bigint"`
	Backfilled      bool  `json:"backfilled"`
	ClosedAt        int64 `json:"closed_at" gorm:"bigint"`
}

func (CpaQuotaCycle) TableName() string { return "cpa_quota_cycle" }

func SaveCpaQuotaCycle(cycle *CpaQuotaCycle) error {
	return DB.Save(cycle).Error
}

func ListCpaQuotaCycles() ([]CpaQuotaCycle, error) {
	var cycles []CpaQuotaCycle
	err := DB.Order("instance_id asc, name asc, quota_window asc, start_ts desc").Find(&cycles).Error
	return cycles, err
}

// ListOpenCpaQuotaCyclesEndedBefore returns open cycles whose end has passed.
func ListOpenCpaQuotaCyclesEndedBefore(ts int64) ([]CpaQuotaCycle, error) {
	var cycles []CpaQuotaCycle
	err := DB.Where("end_reason = ? AND end_ts < ?", "", ts).Find(&cycles).Error
	return cycles, err
}

// PruneCpaUsageFail deletes failure buckets older than the cutoff.
func PruneCpaUsageFail(before int64) error {
	return DB.Where("bucket_ts < ?", before).Delete(&CpaUsageFailHourly{}).Error
}

// GetLatestCpaQuotaCycle returns the newest cycle (open or closed) of one
// account window, or nil.
func GetLatestCpaQuotaCycle(instanceID int, name, window string) (*CpaQuotaCycle, error) {
	var cycle CpaQuotaCycle
	err := DB.Where("instance_id = ? AND name = ? AND quota_window = ?", instanceID, name, window).
		Order("start_ts desc, id desc").Limit(1).Find(&cycle).Error
	if err != nil || cycle.ID == 0 {
		return nil, err
	}
	return &cycle, nil
}

// ListCpaUsageHourlyOfAccount returns one account's buckets in [from, to).
func ListCpaUsageHourlyOfAccount(instanceID int, authIndex string, from, to int64) ([]CpaUsageHourly, error) {
	var rows []CpaUsageHourly
	err := DB.Where("instance_id = ? AND auth_index = ? AND bucket_ts >= ? AND bucket_ts < ?", instanceID, authIndex, from, to).
		Find(&rows).Error
	return rows, err
}

// HasCpaQuotaCycleBefore tells whether an account window has a cycle that
// started before ts.
func HasCpaQuotaCycleBefore(instanceID int, name, window string, ts int64) (bool, error) {
	var n int64
	err := DB.Model(&CpaQuotaCycle{}).
		Where("instance_id = ? AND name = ? AND quota_window = ? AND start_ts < ?", instanceID, name, window, ts).
		Count(&n).Error
	return n > 0, err
}
