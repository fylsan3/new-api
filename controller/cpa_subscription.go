package controller

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Subscription accounts (root only). See service/cpa_subscription*.go.

func init() {
	auditContentTemplates["cpa_subscription.instance_create"] = "Added CPA ${name} (ID: ${id})"
	auditContentTemplates["cpa_subscription.instance_update"] = "Updated CPA ${name} (ID: ${id})"
	auditContentTemplates["cpa_subscription.instance_delete"] = "Deleted CPA ${name} (ID: ${id})"
	auditContentTemplates["cpa_subscription.account_enable"] = "Enabled subscription account ${name} (CPA ${instance_id})"
	auditContentTemplates["cpa_subscription.account_disable"] = "Disabled subscription account ${name} (CPA ${instance_id})"
	auditContentTemplates["cpa_subscription.token_refresh"] = "Refreshed token of subscription account ${name} (CPA ${instance_id})"
	auditContentTemplates["cpa_subscription.reset_grant_claim"] = "Claimed reset grant for subscription account ${name}: ${result}"
	auditContentTemplates["cpa_subscription.settings_update"] = "Updated subscription account refresh settings"
}

type cpaInstanceView struct {
	model.CpaInstance
	HasSecret bool   `json:"has_secret"`
	Secret    string `json:"secret"`
}

func toCpaInstanceView(instance model.CpaInstance) cpaInstanceView {
	view := cpaInstanceView{CpaInstance: instance, HasSecret: instance.SecretEnc != ""}
	if view.HasSecret {
		view.Secret = "****"
	}
	return view
}

type cpaInstanceRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Secret  string `json:"secret"`
}

func (r *cpaInstanceRequest) secretProvided() bool {
	s := strings.TrimSpace(r.Secret)
	return s != "" && s != "****"
}

func cpaRequestContext(c *gin.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), timeout)
}

func ListCpaSubscriptionOverview(c *gin.Context) {
	instances, err := model.ListCpaInstances()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]cpaInstanceView, 0, len(instances))
	for _, instance := range instances {
		views = append(views, toCpaInstanceView(instance))
	}
	accounts, err := service.ListCpaSnapshots()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"instances":           views,
		"accounts":            accounts,
		"settings":            setting,
		"refresh_all_running": service.IsCpaRefreshAllRunning(),
		"now":                 common.GetTimestamp(),
	})
}

func CreateCpaInstance(c *gin.Context) {
	var req cpaInstanceRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		common.ApiErrorMsg(c, "name is required")
		return
	}
	baseURL, err := service.NormalizeCpaBaseURL(req.BaseURL)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !req.secretProvided() {
		common.ApiErrorMsg(c, "management key is required")
		return
	}
	enc, err := service.EncryptCpaSecret(strings.TrimSpace(req.Secret))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	instance := model.CpaInstance{Name: name, BaseURL: baseURL, SecretEnc: enc}
	if err := model.CreateCpaInstance(&instance); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "cpa_subscription.instance_create", map[string]any{"id": instance.ID, "name": name, "base_url": baseURL})
	common.ApiSuccess(c, toCpaInstanceView(instance))
}

func UpdateCpaInstance(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	instance, err := model.GetCpaInstance(id)
	if err != nil {
		common.ApiErrorMsg(c, "CPA instance not found")
		return
	}
	var req cpaInstanceRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		instance.Name = name
	}
	if strings.TrimSpace(req.BaseURL) != "" {
		baseURL, err := service.NormalizeCpaBaseURL(req.BaseURL)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		instance.BaseURL = baseURL
	}
	secretChanged := req.secretProvided()
	if secretChanged {
		enc, err := service.EncryptCpaSecret(strings.TrimSpace(req.Secret))
		if err != nil {
			common.ApiError(c, err)
			return
		}
		instance.SecretEnc = enc
	}
	if err := model.SaveCpaInstance(instance); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "cpa_subscription.instance_update", map[string]any{
		"id": instance.ID, "name": instance.Name, "base_url": instance.BaseURL, "secret_changed": secretChanged,
	})
	common.ApiSuccess(c, toCpaInstanceView(*instance))
}

func DeleteCpaInstance(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	instance, err := model.GetCpaInstance(id)
	if err != nil {
		common.ApiErrorMsg(c, "CPA instance not found")
		return
	}
	if err := model.DeleteCpaInstance(id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "cpa_subscription.instance_delete", map[string]any{"id": id, "name": instance.Name})
	common.ApiSuccess(c, nil)
}

// TestCpaInstance tests either a saved instance (id>0, optional overrides) or
// unsaved form values. A blank or "****" secret reuses the saved one.
func TestCpaInstance(c *gin.Context) {
	var req struct {
		cpaInstanceRequest
		ID int `json:"id"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	var saved *model.CpaInstance
	if req.ID > 0 {
		instance, err := model.GetCpaInstance(req.ID)
		if err != nil {
			common.ApiErrorMsg(c, "CPA instance not found")
			return
		}
		saved = instance
	}
	baseURLRaw := req.BaseURL
	if strings.TrimSpace(baseURLRaw) == "" && saved != nil {
		baseURLRaw = saved.BaseURL
	}
	baseURL, err := service.NormalizeCpaBaseURL(baseURLRaw)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	secret := strings.TrimSpace(req.Secret)
	if !req.secretProvided() {
		if saved == nil || saved.SecretEnc == "" {
			common.ApiErrorMsg(c, "management key is required")
			return
		}
		secret, err = service.DecryptCpaSecret(saved.SecretEnc)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	ctx, cancel := cpaRequestContext(c, 20*time.Second)
	defer cancel()
	count, testErr := service.TestCpaConnection(ctx, baseURL, secret)
	if saved != nil {
		saved.LastTestAt = common.GetTimestamp()
		saved.LastTestOK = testErr == nil
		saved.LastTestMsg = ""
		if testErr != nil {
			saved.LastTestMsg = testErr.Error()
		}
		_ = model.SaveCpaInstance(saved)
	}
	if testErr != nil {
		common.ApiError(c, testErr)
		return
	}
	common.ApiSuccess(c, gin.H{"credential_count": count})
}

func SyncCpaInstances(c *gin.Context) {
	ctx, cancel := cpaRequestContext(c, 60*time.Second)
	defer cancel()
	errs := service.SyncAllCpaInstances(ctx)
	failures := map[string]string{}
	for id, err := range errs {
		failures[strconv.Itoa(id)] = err.Error()
	}
	common.ApiSuccess(c, gin.H{"failures": failures})
}

func RefreshAllCpaAccounts(c *gin.Context) {
	plan, err := service.StartCpaRefreshAll()
	if err != nil {
		var cooldown *service.CpaCooldownError
		if errors.As(err, &cooldown) {
			common.ApiErrorStatus(c, 200, err, gin.H{"cooldown_remaining": cooldown.RemainingSeconds})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, plan)
}

type cpaAccountRequest struct {
	InstanceID int    `json:"instance_id"`
	Name       string `json:"name"`
	Disabled   *bool  `json:"disabled"`
}

func bindCpaAccount(c *gin.Context) (*cpaAccountRequest, bool) {
	var req cpaAccountRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.InstanceID <= 0 || strings.TrimSpace(req.Name) == "" {
		common.ApiErrorMsg(c, "instance_id and name are required")
		return nil, false
	}
	req.Name = strings.TrimSpace(req.Name)
	return &req, true
}

func RefreshCpaAccount(c *gin.Context) {
	req, ok := bindCpaAccount(c)
	if !ok {
		return
	}
	ctx, cancel := cpaRequestContext(c, 60*time.Second)
	defer cancel()
	snap, err := service.RefreshCpaAccount(ctx, req.InstanceID, req.Name)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, snap)
}

func ListCpaAccountModels(c *gin.Context) {
	instanceID, _ := strconv.Atoi(c.Query("instance_id"))
	name := strings.TrimSpace(c.Query("name"))
	if instanceID <= 0 || name == "" {
		common.ApiErrorMsg(c, "instance_id and name are required")
		return
	}
	ctx, cancel := cpaRequestContext(c, 30*time.Second)
	defer cancel()
	models, err := service.ListCpaAccountModels(ctx, instanceID, name)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, models)
}

func SetCpaAccountStatus(c *gin.Context) {
	req, ok := bindCpaAccount(c)
	if !ok {
		return
	}
	if req.Disabled == nil {
		common.ApiErrorMsg(c, "disabled is required")
		return
	}
	ctx, cancel := cpaRequestContext(c, 30*time.Second)
	defer cancel()
	err := service.SetCpaAccountDisabled(ctx, req.InstanceID, req.Name, *req.Disabled)
	action := "cpa_subscription.account_enable"
	if *req.Disabled {
		action = "cpa_subscription.account_disable"
	}
	recordManageAudit(c, action, map[string]any{"instance_id": req.InstanceID, "name": req.Name, "success": err == nil})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func RefreshCpaAccountToken(c *gin.Context) {
	req, ok := bindCpaAccount(c)
	if !ok {
		return
	}
	ctx, cancel := cpaRequestContext(c, 60*time.Second)
	defer cancel()
	err := service.RefreshCpaAccountToken(ctx, req.InstanceID, req.Name)
	recordManageAudit(c, "cpa_subscription.token_refresh", map[string]any{"instance_id": req.InstanceID, "name": req.Name, "success": err == nil})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func ClaimCpaAccountResetGrant(c *gin.Context) {
	req, ok := bindCpaAccount(c)
	if !ok {
		return
	}
	ctx, cancel := cpaRequestContext(c, 60*time.Second)
	defer cancel()
	result, err := service.ClaimCpaResetGrant(ctx, req.InstanceID, req.Name)
	params := map[string]any{"instance_id": req.InstanceID, "name": req.Name, "success": err == nil}
	if result != nil {
		params["result"] = result.Result
		params["grant_id"] = result.GrantID
	}
	recordManageAudit(c, "cpa_subscription.reset_grant_claim", params)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func UpdateCpaSubscriptionSettings(c *gin.Context) {
	var req struct {
		AutoRefreshEnabled         *bool `json:"auto_refresh_enabled"`
		AutoRefreshIntervalMinutes *int  `json:"auto_refresh_interval_minutes"`
		RefreshAllCooldownSeconds  *int  `json:"refresh_all_cooldown_seconds"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if req.AutoRefreshEnabled != nil {
		setting.AutoRefreshEnabled = *req.AutoRefreshEnabled
	}
	if req.AutoRefreshIntervalMinutes != nil {
		setting.AutoRefreshIntervalMinutes = *req.AutoRefreshIntervalMinutes
	}
	if req.RefreshAllCooldownSeconds != nil {
		setting.RefreshAllCooldownSeconds = *req.RefreshAllCooldownSeconds
	}
	if err := model.SaveCpaSubscriptionSetting(setting); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "cpa_subscription.settings_update", map[string]any{
		"auto_refresh_enabled":          setting.AutoRefreshEnabled,
		"auto_refresh_interval_minutes": setting.AutoRefreshIntervalMinutes,
		"refresh_all_cooldown_seconds":  setting.RefreshAllCooldownSeconds,
	})
	common.ApiSuccess(c, setting)
}
