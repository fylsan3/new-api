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
	auditContentTemplates["cpa_subscription.usage_collect"] = "Set usage collection of CPA ${name} (ID: ${id}) to ${enabled}"
	auditContentTemplates["cpa_subscription.alert_ack"] = "Acknowledged ${count} subscription account alert(s)"
	auditContentTemplates["cpa_subscription.proxy_reveal"] = "Viewed full proxy address of subscription account ${name} (CPA ${instance_id})"
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
	Name                string  `json:"name"`
	BaseURL             string  `json:"base_url"`
	Secret              string  `json:"secret"`
	UsageCollectEnabled *bool   `json:"usage_collect_enabled"`
	ChannelIDs          *string `json:"channel_ids"`
}

// normalizeChannelIDs keeps positive integers only, deduplicated.
func normalizeChannelIDs(raw string) (string, error) {
	seen := map[int]bool{}
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return "", errors.New("channel_ids must be comma separated channel IDs")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, strconv.Itoa(id))
		}
	}
	joined := strings.Join(out, ",")
	if len(joined) > 1000 {
		return "", errors.New("too many channel IDs")
	}
	return joined, nil
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
	cardUsage, err := service.ListCpaCardUsage()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	alerts, err := model.ListOpenCpaAlerts()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"instances":           views,
		"accounts":            accounts,
		"usage":               cardUsage,
		"alerts":              alerts,
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
	if req.UsageCollectEnabled != nil {
		instance.UsageCollectEnabled = *req.UsageCollectEnabled
	}
	if req.ChannelIDs != nil {
		ids, idsErr := normalizeChannelIDs(*req.ChannelIDs)
		if idsErr != nil {
			common.ApiError(c, idsErr)
			return
		}
		instance.ChannelIDs = ids
	}
	if err := model.CreateCpaInstance(&instance); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "cpa_subscription.instance_create", map[string]any{
		"id": instance.ID, "name": name, "base_url": baseURL, "usage_collect_enabled": instance.UsageCollectEnabled,
	})
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
	if req.ChannelIDs != nil {
		ids, idsErr := normalizeChannelIDs(*req.ChannelIDs)
		if idsErr != nil {
			common.ApiError(c, idsErr)
			return
		}
		instance.ChannelIDs = ids
	}
	collectChanged := req.UsageCollectEnabled != nil && *req.UsageCollectEnabled != instance.UsageCollectEnabled
	if req.UsageCollectEnabled != nil {
		instance.UsageCollectEnabled = *req.UsageCollectEnabled
	}
	if err := model.SaveCpaInstance(instance); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "cpa_subscription.instance_update", map[string]any{
		"id": instance.ID, "name": instance.Name, "base_url": instance.BaseURL, "secret_changed": secretChanged,
		"channel_ids": instance.ChannelIDs,
	})
	if collectChanged {
		recordManageAudit(c, "cpa_subscription.usage_collect", map[string]any{
			"id": instance.ID, "name": instance.Name, "enabled": instance.UsageCollectEnabled,
		})
	}
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
	snap, err := service.RefreshCpaAccountWithCredential(ctx, req.InstanceID, req.Name)
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
		AutoRefreshEnabled         *bool    `json:"auto_refresh_enabled"`
		AutoRefreshIntervalMinutes *int     `json:"auto_refresh_interval_minutes"`
		RefreshAllCooldownSeconds  *int     `json:"refresh_all_cooldown_seconds"`
		ProxyMonthlyCostUSD        *float64 `json:"proxy_monthly_cost_usd"`
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
	if req.ProxyMonthlyCostUSD != nil {
		setting.ProxyMonthlyCostUSD = *req.ProxyMonthlyCostUSD
	}
	if err := model.SaveCpaSubscriptionSetting(setting); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "cpa_subscription.settings_update", map[string]any{
		"auto_refresh_enabled":          setting.AutoRefreshEnabled,
		"auto_refresh_interval_minutes": setting.AutoRefreshIntervalMinutes,
		"refresh_all_cooldown_seconds":  setting.RefreshAllCooldownSeconds,
		"proxy_monthly_cost_usd":        setting.ProxyMonthlyCostUSD,
	})
	common.ApiSuccess(c, setting)
}

func GetCpaSubscriptionStats(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	report, err := service.BuildCpaStats(days)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

// GetCpaSubscriptionCycles serves the cycle comparison tab.
func GetCpaSubscriptionCycles(c *gin.Context) {
	report, err := service.BuildCpaCycleReport()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

func ListCpaAlerts(c *gin.Context) {
	alerts, err := model.ListOpenCpaAlerts()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, alerts)
}

// AcknowledgeCpaAlerts closes the given alerts; an empty ids list closes all.
func AcknowledgeCpaAlerts(c *gin.Context) {
	var req struct {
		IDs []int `json:"ids"`
		All bool  `json:"all"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if len(req.IDs) == 0 && !req.All {
		common.ApiErrorMsg(c, "ids or all is required")
		return
	}
	count, err := model.AcknowledgeCpaAlerts(req.IDs, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "cpa_subscription.alert_ack", map[string]any{"count": count, "ids": req.IDs})
	common.ApiSuccess(c, gin.H{"acknowledged": count})
}

// RevealCpaAccountProxy returns the unmasked proxy_url of one account. Root
// only and audited; the value is read fresh from CPA and never cached.
func RevealCpaAccountProxy(c *gin.Context) {
	req, ok := bindCpaAccount(c)
	if !ok {
		return
	}
	ctx, cancel := cpaRequestContext(c, 20*time.Second)
	defer cancel()
	proxy, err := service.RevealCpaAccountProxy(ctx, req.InstanceID, req.Name)
	recordManageAudit(c, "cpa_subscription.proxy_reveal", map[string]any{"instance_id": req.InstanceID, "name": req.Name, "success": err == nil})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"proxy_url": proxy})
}
