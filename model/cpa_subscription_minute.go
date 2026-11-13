package model

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CpaUsageMinute is a per-account, per-minute aggregate of the CPA usage
// queue (user decision 2026-11-12: RPM / TPM, 1-minute value plus 5-minute
// average, charts for 1 h / 12 h / 24 h / 7 d). Kept for about 7 days.
type CpaUsageMinute struct {
	ID         int    `json:"-" gorm:"primaryKey"`
	InstanceID int    `json:"instance_id" gorm:"uniqueIndex:idx_cpa_usage_minute_key,priority:1;not null"`
	AuthIndex  string `json:"auth_index" gorm:"type:varchar(64);uniqueIndex:idx_cpa_usage_minute_key,priority:2;not null"`
	BucketTs   int64  `json:"bucket_ts" gorm:"bigint;uniqueIndex:idx_cpa_usage_minute_key,priority:3;index:idx_cpa_usage_minute_ts;not null"`
	Requests   int64  `json:"requests" gorm:"bigint"`
	Tokens     int64  `json:"tokens" gorm:"bigint"`
	Failed     int64  `json:"failed" gorm:"bigint"`
	Status429  int64  `json:"status_429" gorm:"column:status_429;bigint"`
	Status401  int64  `json:"status_401" gorm:"column:status_401;bigint"`
	Status403  int64  `json:"status_403" gorm:"column:status_403;bigint"`
}

func (CpaUsageMinute) TableName() string { return "cpa_usage_minute" }

// AddCpaUsageMinute adds one aggregated delta, creating the bucket if needed.
func AddCpaUsageMinute(row *CpaUsageMinute) error {
	if row == nil || row.Requests == 0 {
		return nil
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "instance_id"}, {Name: "auth_index"}, {Name: "bucket_ts"}},
		DoUpdates: clause.Assignments(map[string]any{
			"requests":   gorm.Expr("cpa_usage_minute.requests + ?", row.Requests),
			"tokens":     gorm.Expr("cpa_usage_minute.tokens + ?", row.Tokens),
			"failed":     gorm.Expr("cpa_usage_minute.failed + ?", row.Failed),
			"status_429": gorm.Expr("cpa_usage_minute.status_429 + ?", row.Status429),
			"status_401": gorm.Expr("cpa_usage_minute.status_401 + ?", row.Status401),
			"status_403": gorm.Expr("cpa_usage_minute.status_403 + ?", row.Status403),
		}),
	}).Create(row).Error
}

// ListCpaUsageMinutes returns buckets in [from, to).
func ListCpaUsageMinutes(from, to int64) ([]CpaUsageMinute, error) {
	var rows []CpaUsageMinute
	err := DB.Where("bucket_ts >= ? AND bucket_ts < ?", from, to).Order("bucket_ts asc").Find(&rows).Error
	return rows, err
}

// ListCpaUsageMinutesOf returns the buckets of one account in [from, to).
func ListCpaUsageMinutesOf(instanceID int, authIndex string, from, to int64) ([]CpaUsageMinute, error) {
	var rows []CpaUsageMinute
	err := DB.Where("instance_id = ? AND auth_index = ? AND bucket_ts >= ? AND bucket_ts < ?", instanceID, authIndex, from, to).
		Order("bucket_ts asc").Find(&rows).Error
	return rows, err
}

// ListCpaUsageMinuteTotals returns one row per minute in [from, to): the sum
// of every account (InstanceID 0, empty AuthIndex).
func ListCpaUsageMinuteTotals(from, to int64) ([]CpaUsageMinute, error) {
	var rows []CpaUsageMinute
	err := DB.Model(&CpaUsageMinute{}).
		Select("bucket_ts, SUM(requests) AS requests, SUM(tokens) AS tokens, SUM(failed) AS failed, SUM(status_429) AS status_429, SUM(status_401) AS status_401, SUM(status_403) AS status_403").
		Where("bucket_ts >= ? AND bucket_ts < ?", from, to).
		Group("bucket_ts").Order("bucket_ts asc").Find(&rows).Error
	return rows, err
}

func PruneCpaUsageMinutes(before int64) error {
	return DB.Where("bucket_ts < ?", before).Delete(&CpaUsageMinute{}).Error
}
