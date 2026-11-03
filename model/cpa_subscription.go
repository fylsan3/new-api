package model

import (
	"errors"

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
}

const (
	CpaAutoRefreshMinIntervalMinutes = 10
	CpaRefreshAllMinCooldownSeconds  = 60
)

func (s *CpaSubscriptionSetting) Normalize() {
	if s.AutoRefreshIntervalMinutes < CpaAutoRefreshMinIntervalMinutes {
		s.AutoRefreshIntervalMinutes = CpaAutoRefreshMinIntervalMinutes
	}
	if s.RefreshAllCooldownSeconds < CpaRefreshAllMinCooldownSeconds {
		s.RefreshAllCooldownSeconds = CpaRefreshAllMinCooldownSeconds
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
		if err := tx.Where("instance_id = ?", id).Delete(&CpaAccountCache{}).Error; err != nil {
			return err
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
