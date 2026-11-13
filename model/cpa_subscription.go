package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// CpaInstance is one CLIProxyAPI deployment whose management API is read by
// the root-only subscription accounts page. SecretEnc never leaves the server.
type CpaInstance struct {
	ID          int    `json:"id" gorm:"primaryKey"`
	Name        string `json:"name" gorm:"type:varchar(128);not null"`
	BaseURL     string `json:"base_url" gorm:"type:varchar(512);not null"`
	SecretEnc   string `json:"-" gorm:"type:text"`
	LastTestAt  int64  `json:"last_test_at" gorm:"bigint"`
	LastTestOK  bool   `json:"last_test_ok"`
	LastTestMsg string `json:"last_test_msg" gorm:"type:varchar(512)"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`

	// UsageCollectEnabled makes New API drain this CPA's usage queue. Off by
	// default: popping is destructive, so it must be an explicit choice.
	UsageCollectEnabled bool `json:"usage_collect_enabled"`
	// ChannelIDs lists the New API channels that forward to this CPA
	// (comma separated). Empty means "match channels by host".
	ChannelIDs        string `json:"channel_ids" gorm:"type:varchar(1024)"`
	UsageLastPollAt   int64  `json:"usage_last_poll_at" gorm:"bigint"`
	UsageLastRecordAt int64  `json:"usage_last_record_at" gorm:"bigint"`
	UsageLastError    string `json:"usage_last_error" gorm:"type:varchar(512)"`
	// UsageRemoteState mirrors CPA usage-statistics-enabled: 0 unknown, 1 on, 2 off.
	UsageRemoteState int `json:"usage_remote_state"`
}

// CpaAccountCache keeps the last known snapshot of one credential so the page
// renders without calling CPA. Data is a JSON document owned by the service.
type CpaAccountCache struct {
	ID         int    `json:"id" gorm:"primaryKey"`
	InstanceID int    `json:"instance_id" gorm:"index:idx_cpa_account_cache_key,unique;not null"`
	Name       string `json:"name" gorm:"type:varchar(255);index:idx_cpa_account_cache_key,unique;not null"`
	Data       string `json:"-" gorm:"type:text"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint"`
}

// CpaSubscriptionSetting is a single-row table (ID=1) for page-level settings.
type CpaSubscriptionSetting struct {
	ID                         int   `json:"-" gorm:"primaryKey"`
	AutoRefreshEnabled         bool  `json:"auto_refresh_enabled"`
	AutoRefreshIntervalMinutes int   `json:"auto_refresh_interval_minutes"`
	RefreshAllCooldownSeconds  int   `json:"refresh_all_cooldown_seconds"`
	LastRefreshAllAt           int64 `json:"last_refresh_all_at" gorm:"bigint"`
	// ProxyMonthlyCostUSD is the SOCKS proxy cost of one account per month.
	ProxyMonthlyCostUSD float64 `json:"proxy_monthly_cost_usd"`

	// Account rotation (one global set, user decision 2026-11-11). An
	// account that has served requests for RotationWorkHours consecutive
	// hours is disabled in CPA for RotationRestMinutes ± jitter, then
	// re-enabled. RotationExcluded holds "instanceID/name" keys, one per line.
	RotationInitialized    bool   `json:"-"`
	RotationEnabled        bool   `json:"rotation_enabled"`
	RotationWorkHours      int    `json:"rotation_work_hours"`
	RotationRestMinutes    int    `json:"rotation_rest_minutes"`
	RotationJitterMinutes  int    `json:"rotation_jitter_minutes"`
	RotationMaxRestPercent int    `json:"rotation_max_rest_percent"`
	RotationMinOnline      int    `json:"rotation_min_online"`
	RotationExcluded       string `json:"rotation_excluded" gorm:"type:text"`

	// Rotation extensions (user decisions 2026-11-12, every rule has its own
	// switch and values). Defaults are set once via RotationExtInitialized.
	RotationExtInitialized bool `json:"-"`
	// RotationShowBadges hides every rotation tag / notice on the page.
	RotationShowBadges bool `json:"rotation_show_badges"`
	// Busy wake-up: wake resting accounts early when the pool is busy.
	RotationWake429Enabled      bool `json:"rotation_wake_429_enabled"`
	RotationWake429Minutes      int  `json:"rotation_wake_429_minutes"`
	RotationWake429Count        int  `json:"rotation_wake_429_count"`
	RotationWakeRpmEnabled      bool `json:"rotation_wake_rpm_enabled"`
	RotationWakeRpm             int  `json:"rotation_wake_rpm"`
	RotationWakeBatch           int  `json:"rotation_wake_batch"`
	RotationWakeCooldownMinutes int  `json:"rotation_wake_cooldown_minutes"`
	RotationWakeHoldMinutes     int  `json:"rotation_wake_hold_minutes"`
	RotationWakeRest            bool `json:"rotation_wake_rest"`
	RotationWakeShift           bool `json:"rotation_wake_shift"`
	// Night: a fixed window with a higher max-rest percentage.
	RotationNightEnabled        bool   `json:"rotation_night_enabled"`
	RotationNightStart          string `json:"rotation_night_start" gorm:"type:varchar(8)"`
	RotationNightEnd            string `json:"rotation_night_end" gorm:"type:varchar(8)"`
	RotationNightMaxRestPercent int    `json:"rotation_night_max_rest_percent"`
	// Shifts: day / night / all. RotationShifts holds "instanceID/name=day"
	// lines; accounts without a line are assigned alternately.
	RotationShiftEnabled       bool   `json:"rotation_shift_enabled"`
	RotationDayShiftStart      string `json:"rotation_day_shift_start" gorm:"type:varchar(8)"`
	RotationDayShiftEnd        string `json:"rotation_day_shift_end" gorm:"type:varchar(8)"`
	RotationNightShiftStart    string `json:"rotation_night_shift_start" gorm:"type:varchar(8)"`
	RotationNightShiftEnd      string `json:"rotation_night_shift_end" gorm:"type:varchar(8)"`
	RotationShiftJitterMinutes int    `json:"rotation_shift_jitter_minutes"`
	RotationShifts             string `json:"rotation_shifts" gorm:"type:text"`
	// Per-account protection.
	RotationLimitEnabled     bool `json:"rotation_limit_enabled"`
	RotationLimitMinutes     int  `json:"rotation_limit_minutes"`
	RotationLimitCount       int  `json:"rotation_limit_count"`
	RotationLimitRestMinutes int  `json:"rotation_limit_rest_minutes"`
	RotationQuotaEnabled     bool `json:"rotation_quota_enabled"`
	RotationQuotaPercent     int  `json:"rotation_quota_percent"`
	RotationDailyEnabled     bool `json:"rotation_daily_enabled"`
	RotationDailyHours       int  `json:"rotation_daily_hours"`

	// Safety gates (user decision 2026-11-12: turning rotation on must
	// never take many accounts offline at once). Defaults are set once via
	// RotationSafeInitialized.
	RotationSafeInitialized bool `json:"-"`
	// RotationEnabledAt: when the switch was last turned on. Work hours
	// before it are not counted.
	RotationEnabledAt int64 `json:"rotation_enabled_at" gorm:"bigint"`
	// At most RotationBatchSize accounts per CPA go resting in one step,
	// and two steps are at least RotationBatchGapMinutes apart.
	RotationBatchSize       int `json:"rotation_batch_size"`
	RotationBatchGapMinutes int `json:"rotation_batch_gap_minutes"`
	// Capacity check: an account only rests when the accounts left online
	// can carry the busiest minute of the last hour (per-account RPM / TPM
	// below the limits, 0 = not checked) and the pool had no 429 in the
	// last RotationCapacity429Minutes.
	RotationCapacityEnabled    bool `json:"rotation_capacity_enabled"`
	RotationCapacityRpm        int  `json:"rotation_capacity_rpm"`
	RotationCapacityTpm        int  `json:"rotation_capacity_tpm"`
	RotationCapacity429Minutes int  `json:"rotation_capacity_429_minutes"`
	// RotationDryRun: plan and show who would rest, never disable anyone.
	RotationDryRun bool `json:"rotation_dry_run"`

	// Monitoring: 401/403 stop + WeCom / DingTalk webhooks (encrypted).
	AuthStopEnabled         bool   `json:"auth_stop_enabled"`
	AlertWecomEnabled       bool   `json:"alert_wecom_enabled"`
	AlertWecomWebhookEnc    string `json:"-" gorm:"type:text"`
	AlertDingtalkEnabled    bool   `json:"alert_dingtalk_enabled"`
	AlertDingtalkWebhookEnc string `json:"-" gorm:"type:text"`
	AlertDingtalkSecretEnc  string `json:"-" gorm:"type:text"`
	AlertOnSocks            bool   `json:"alert_on_socks"`
	AlertOnAuth             bool   `json:"alert_on_auth"`
	AlertOnCpa              bool   `json:"alert_on_cpa"`
	AlertOnRotation         bool   `json:"alert_on_rotation"`
	// Read-only flags for the page; secrets never leave the server.
	HasWecomWebhook    bool `json:"has_wecom_webhook" gorm:"-"`
	HasDingtalkWebhook bool `json:"has_dingtalk_webhook" gorm:"-"`
	HasDingtalkSecret  bool `json:"has_dingtalk_secret" gorm:"-"`
}

const (
	CpaAutoRefreshMinIntervalMinutes = 10
	CpaRefreshAllMinCooldownSeconds  = 60

	// Suggested rotation values from the design draft (2026-11-11).
	CpaRotationDefaultWorkHours      = 6
	CpaRotationDefaultRestMinutes    = 120
	CpaRotationDefaultJitterMinutes  = 20
	CpaRotationDefaultMaxRestPercent = 30
	CpaRotationDefaultMinOnline      = 2
)

func (s *CpaSubscriptionSetting) normalizeRotation() {
	if !s.RotationInitialized {
		s.RotationInitialized = true
		s.RotationWorkHours = CpaRotationDefaultWorkHours
		s.RotationRestMinutes = CpaRotationDefaultRestMinutes
		s.RotationJitterMinutes = CpaRotationDefaultJitterMinutes
		s.RotationMaxRestPercent = CpaRotationDefaultMaxRestPercent
		s.RotationMinOnline = CpaRotationDefaultMinOnline
	}
	s.RotationWorkHours = min(max(s.RotationWorkHours, 1), 72)
	s.RotationRestMinutes = min(max(s.RotationRestMinutes, 10), 24*60)
	s.RotationJitterMinutes = min(max(s.RotationJitterMinutes, 0), s.RotationRestMinutes/2)
	s.RotationMaxRestPercent = min(max(s.RotationMaxRestPercent, 1), 90)
	s.RotationMinOnline = min(max(s.RotationMinOnline, 1), 1000)
	s.normalizeRotationExt()
}

// normalizeRotationExt sets the suggested defaults once (user confirmed
// 2026-11-12) and clamps every value afterwards.
func (s *CpaSubscriptionSetting) normalizeRotationExt() {
	if !s.RotationExtInitialized {
		s.RotationExtInitialized = true
		s.RotationShowBadges = true
		s.RotationWake429Minutes, s.RotationWake429Count = 10, 20
		s.RotationWakeRpm = 10
		s.RotationWakeBatch, s.RotationWakeCooldownMinutes, s.RotationWakeHoldMinutes = 1, 5, 30
		s.RotationWakeRest, s.RotationWakeShift = true, false
		s.RotationNightStart, s.RotationNightEnd, s.RotationNightMaxRestPercent = "01:00", "07:00", 60
		s.RotationDayShiftStart, s.RotationDayShiftEnd = "08:00", "02:00"
		s.RotationNightShiftStart, s.RotationNightShiftEnd = "20:00", "14:00"
		s.RotationShiftJitterMinutes = 30
		s.RotationLimitMinutes, s.RotationLimitCount, s.RotationLimitRestMinutes = 10, 10, 30
		s.RotationQuotaPercent = 90
		s.RotationDailyHours = 16
		s.AlertOnSocks, s.AlertOnAuth, s.AlertOnCpa, s.AlertOnRotation = true, true, true, true
	}
	clamp := func(v *int, lo, hi int) { *v = min(max(*v, lo), hi) }
	if !s.RotationSafeInitialized {
		s.RotationSafeInitialized = true
		s.RotationBatchSize, s.RotationBatchGapMinutes = 1, 5
		s.RotationCapacityEnabled = true
		s.RotationCapacityRpm, s.RotationCapacityTpm, s.RotationCapacity429Minutes = 8, 0, 5
	}
	clamp(&s.RotationBatchSize, 1, 100)
	clamp(&s.RotationBatchGapMinutes, 0, 240)
	clamp(&s.RotationCapacityRpm, 0, 100000)
	clamp(&s.RotationCapacityTpm, 0, 1000000000)
	clamp(&s.RotationCapacity429Minutes, 0, 60)
	clamp(&s.RotationWake429Minutes, 1, 120)
	clamp(&s.RotationWake429Count, 1, 100000)
	clamp(&s.RotationWakeRpm, 1, 10000)
	clamp(&s.RotationWakeBatch, 1, 100)
	clamp(&s.RotationWakeCooldownMinutes, 1, 240)
	clamp(&s.RotationWakeHoldMinutes, 5, 1440)
	clamp(&s.RotationNightMaxRestPercent, 1, 90)
	clamp(&s.RotationShiftJitterMinutes, 0, 180)
	clamp(&s.RotationLimitMinutes, 1, 120)
	clamp(&s.RotationLimitCount, 1, 100000)
	clamp(&s.RotationLimitRestMinutes, 5, 1440)
	clamp(&s.RotationQuotaPercent, 50, 100)
	clamp(&s.RotationDailyHours, 1, 24)
	s.RotationNightStart = normalizeClock(s.RotationNightStart, "01:00")
	s.RotationNightEnd = normalizeClock(s.RotationNightEnd, "07:00")
	s.RotationDayShiftStart = normalizeClock(s.RotationDayShiftStart, "08:00")
	s.RotationDayShiftEnd = normalizeClock(s.RotationDayShiftEnd, "02:00")
	s.RotationNightShiftStart = normalizeClock(s.RotationNightShiftStart, "20:00")
	s.RotationNightShiftEnd = normalizeClock(s.RotationNightShiftEnd, "14:00")
	s.HasWecomWebhook = s.AlertWecomWebhookEnc != ""
	s.HasDingtalkWebhook = s.AlertDingtalkWebhookEnc != ""
	s.HasDingtalkSecret = s.AlertDingtalkSecretEnc != ""
}

// ClockMinutes parses "HH:MM" into minutes after midnight.
func ClockMinutes(v string) (int, bool) {
	var h, m int
	if n, err := fmt.Sscanf(strings.TrimSpace(v), "%d:%d", &h, &m); err != nil || n != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func normalizeClock(v, fallback string) string {
	minutes, ok := ClockMinutes(v)
	if !ok {
		return fallback
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// RotationShiftMap returns "instanceID/name" -> day | night | all.
func (s *CpaSubscriptionSetting) RotationShiftMap() map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s.RotationShifts, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		value = strings.TrimSpace(value)
		if ok && (value == "day" || value == "night" || value == "all") {
			out[strings.TrimSpace(key)] = value
		}
	}
	return out
}

// RotationExcludedSet returns the excluded "instanceID/name" keys.
func (s *CpaSubscriptionSetting) RotationExcludedSet() map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(s.RotationExcluded, "\n") {
		if key := strings.TrimSpace(line); key != "" {
			out[key] = true
		}
	}
	return out
}

func (s *CpaSubscriptionSetting) Normalize() {
	s.normalizeRotation()
	if s.AutoRefreshIntervalMinutes < CpaAutoRefreshMinIntervalMinutes {
		s.AutoRefreshIntervalMinutes = CpaAutoRefreshMinIntervalMinutes
	}
	if s.RefreshAllCooldownSeconds < CpaRefreshAllMinCooldownSeconds {
		s.RefreshAllCooldownSeconds = CpaRefreshAllMinCooldownSeconds
	}
	if s.ProxyMonthlyCostUSD < 0 {
		s.ProxyMonthlyCostUSD = 0
	}
}

func ListCpaInstances() ([]CpaInstance, error) {
	var instances []CpaInstance
	err := DB.Order("id asc").Find(&instances).Error
	return instances, err
}

func GetCpaInstance(id int) (*CpaInstance, error) {
	var instance CpaInstance
	if err := DB.First(&instance, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &instance, nil
}

func CreateCpaInstance(instance *CpaInstance) error {
	now := common.GetTimestamp()
	instance.CreatedAt = now
	instance.UpdatedAt = now
	return DB.Create(instance).Error
}

func SaveCpaInstance(instance *CpaInstance) error {
	instance.UpdatedAt = common.GetTimestamp()
	return DB.Save(instance).Error
}

func DeleteCpaInstance(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, table := range []any{&CpaAccountCache{}, &CpaUsageHourly{}, &CpaQuotaPoint{}, &CpaAlert{}, &CpaUsageFailHourly{}, &CpaQuotaCycle{}, &CpaUsageMinute{}} {
			if err := tx.Where("instance_id = ?", id).Delete(table).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&CpaInstance{}, "id = ?", id).Error
	})
}

func ListCpaAccountCaches() ([]CpaAccountCache, error) {
	var caches []CpaAccountCache
	err := DB.Order("instance_id asc, name asc").Find(&caches).Error
	return caches, err
}

func GetCpaAccountCache(instanceID int, name string) (*CpaAccountCache, error) {
	var cache CpaAccountCache
	err := DB.Where("instance_id = ? AND name = ?", instanceID, name).First(&cache).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cache, nil
}

func UpsertCpaAccountCache(instanceID int, name string, data string) error {
	existing, err := GetCpaAccountCache(instanceID, name)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	if existing == nil {
		return DB.Create(&CpaAccountCache{InstanceID: instanceID, Name: name, Data: data, UpdatedAt: now}).Error
	}
	return DB.Model(&CpaAccountCache{}).Where("id = ?", existing.ID).
		Updates(map[string]any{"data": data, "updated_at": now}).Error
}

// PruneCpaAccountCaches removes cached accounts that CPA no longer lists.
func PruneCpaAccountCaches(instanceID int, keep []string) error {
	query := DB.Where("instance_id = ?", instanceID)
	if len(keep) > 0 {
		query = query.Where("name NOT IN ?", keep)
	}
	return query.Delete(&CpaAccountCache{}).Error
}

func GetCpaSubscriptionSetting() (*CpaSubscriptionSetting, error) {
	var setting CpaSubscriptionSetting
	err := DB.First(&setting, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		setting = CpaSubscriptionSetting{
			ID:                         1,
			AutoRefreshIntervalMinutes: CpaAutoRefreshMinIntervalMinutes,
			RefreshAllCooldownSeconds:  CpaRefreshAllMinCooldownSeconds,
		}
		setting.Normalize()
		if createErr := DB.Create(&setting).Error; createErr != nil {
			return nil, createErr
		}
		return &setting, nil
	}
	if err != nil {
		return nil, err
	}
	setting.Normalize()
	return &setting, nil
}

func SaveCpaSubscriptionSetting(setting *CpaSubscriptionSetting) error {
	setting.ID = 1
	setting.Normalize()
	return DB.Save(setting).Error
}

func MarkCpaRefreshAllAt(ts int64) error {
	return DB.Model(&CpaSubscriptionSetting{}).Where("id = ?", 1).Update("last_refresh_all_at", ts).Error
}
