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
	auditContentTemplates["cpa_subscription.settings_update"] = "Updated subscription account settings"
	auditContentTemplates["cpa_subscription.usage_collect"] = "Set usage collection of CPA ${name} (ID: ${id}) to ${enabled}"
	auditContentTemplates["cpa_subscription.alert_ack"] = "Acknowledged ${count} subscription account alert(s)"
	auditContentTemplates["cpa_subscription.alert_test"] = "Sent a test alert to ${channel}"
	auditContentTemplates["cpa_subscription.account_shift"] = "Set shift of subscription account ${name} (CPA ${instance_id}) to ${shift}"
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
	rates, rateTotal, err := service.ListCpaRates(common.GetTimestamp())
	if err != nil {
		// Side feature: the page still opens, just without RPM / TPM.
		common.SysError("cpa subscription rates: " + err.Error())
		rates, rateTotal = map[string]service.CpaRate{}, service.CpaRate{}
	}
	common.ApiSuccess(c, gin.H{
		"instances":           views,
		"rates":               rates,
		"rate_total":          rateTotal,
		"accounts":            accounts,
		"usage":               cardUsage,
		"alerts":              alerts,
		"settings":            setting,
		"refresh_all_running": service.IsCpaRefreshAllRunning(),
		"rotation":            service.GetCpaRotationSummary(),
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
	// A manual switch always wins over rotation (design draft, section 5).
	service.ClearCpaRotationOnManualSwitch(req.InstanceID, req.Name, *req.Disabled)
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
		params["direct"] = result.Direct
	}
	recordManageAudit(c, "cpa_subscription.reset_grant_claim", params)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// cpaSettingPatch lists every switch / value the page may change (rotation
// extensions and monitoring, user decisions 2026-11-12). nil = unchanged.
// Range checks live in setting.Normalize.
type cpaSettingPatch struct {
	AutoRefreshEnabled         *bool    `json:"auto_refresh_enabled"`
	AutoRefreshIntervalMinutes *int     `json:"auto_refresh_interval_minutes"`
	RefreshAllCooldownSeconds  *int     `json:"refresh_all_cooldown_seconds"`
	ProxyMonthlyCostUSD        *float64 `json:"proxy_monthly_cost_usd"`
	RotationEnabled            *bool    `json:"rotation_enabled"`
	RotationWorkHours          *int     `json:"rotation_work_hours"`
	RotationRestMinutes        *int     `json:"rotation_rest_minutes"`
	RotationJitterMinutes      *int     `json:"rotation_jitter_minutes"`
	RotationMaxRestPercent     *int     `json:"rotation_max_rest_percent"`
	RotationMinOnline          *int     `json:"rotation_min_online"`
	RotationExcluded           *string  `json:"rotation_excluded"`

	RotationShowBadges          *bool   `json:"rotation_show_badges"`
	RotationWake429Enabled      *bool   `json:"rotation_wake_429_enabled"`
	RotationWake429Minutes      *int    `json:"rotation_wake_429_minutes"`
	RotationWake429Count        *int    `json:"rotation_wake_429_count"`
	RotationWakeRpmEnabled      *bool   `json:"rotation_wake_rpm_enabled"`
	RotationWakeRpm             *int    `json:"rotation_wake_rpm"`
	RotationWakeBatch           *int    `json:"rotation_wake_batch"`
	RotationWakeCooldownMinutes *int    `json:"rotation_wake_cooldown_minutes"`
	RotationWakeHoldMinutes     *int    `json:"rotation_wake_hold_minutes"`
	RotationWakeRest            *bool   `json:"rotation_wake_rest"`
	RotationWakeShift           *bool   `json:"rotation_wake_shift"`
	RotationNightEnabled        *bool   `json:"rotation_night_enabled"`
	RotationNightStart          *string `json:"rotation_night_start"`
	RotationNightEnd            *string `json:"rotation_night_end"`
	RotationNightMaxRestPercent *int    `json:"rotation_night_max_rest_percent"`
	RotationShiftEnabled        *bool   `json:"rotation_shift_enabled"`
	RotationDayShiftStart       *string `json:"rotation_day_shift_start"`
	RotationDayShiftEnd         *string `json:"rotation_day_shift_end"`
	RotationNightShiftStart     *string `json:"rotation_night_shift_start"`
	RotationNightShiftEnd       *string `json:"rotation_night_shift_end"`
	RotationShiftJitterMinutes  *int    `json:"rotation_shift_jitter_minutes"`
	RotationLimitEnabled        *bool   `json:"rotation_limit_enabled"`
	RotationLimitMinutes        *int    `json:"rotation_limit_minutes"`
	RotationLimitCount          *int    `json:"rotation_limit_count"`
	RotationLimitRestMinutes    *int    `json:"rotation_limit_rest_minutes"`
	RotationQuotaEnabled        *bool   `json:"rotation_quota_enabled"`
	RotationQuotaPercent        *int    `json:"rotation_quota_percent"`
	RotationDailyEnabled        *bool   `json:"rotation_daily_enabled"`
	RotationDailyHours          *int    `json:"rotation_daily_hours"`

	// Safety gates (user decision 2026-11-12).
	RotationBatchSize          *int  `json:"rotation_batch_size"`
	RotationBatchGapMinutes    *int  `json:"rotation_batch_gap_minutes"`
	RotationCapacityEnabled    *bool `json:"rotation_capacity_enabled"`
	RotationCapacityRpm        *int  `json:"rotation_capacity_rpm"`
	RotationCapacityTpm        *int  `json:"rotation_capacity_tpm"`
	RotationCapacity429Minutes *int  `json:"rotation_capacity_429_minutes"`
	RotationDryRun             *bool `json:"rotation_dry_run"`

	AuthStopEnabled      *bool `json:"auth_stop_enabled"`
	AlertWecomEnabled    *bool `json:"alert_wecom_enabled"`
	AlertDingtalkEnabled *bool `json:"alert_dingtalk_enabled"`
	AlertOnSocks         *bool `json:"alert_on_socks"`
	AlertOnAuth          *bool `json:"alert_on_auth"`
	AlertOnCpa           *bool `json:"alert_on_cpa"`
	AlertOnRotation      *bool `json:"alert_on_rotation"`
	// Secrets: "" keeps the saved value, "-" clears it, anything else
	// replaces it. They are never echoed back.
	AlertWecomWebhook    *string `json:"alert_wecom_webhook"`
	AlertDingtalkWebhook *string `json:"alert_dingtalk_webhook"`
	AlertDingtalkSecret  *string `json:"alert_dingtalk_secret"`
}

func UpdateCpaSubscriptionSettings(c *gin.Context) {
	var req cpaSettingPatch
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
	rotationTouched := applyCpaRotationPatch(setting, &req, common.GetTimestamp())
	changedSecrets, err := applyCpaAlertSecrets(setting, &req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SaveCpaSubscriptionSetting(setting); err != nil {
		common.ApiError(c, err)
		return
	}
	params := map[string]any{}
	if raw, err := common.Marshal(setting); err == nil {
		_ = common.Unmarshal(raw, &params)
	}
	delete(params, "rotation_shifts")
	if len(changedSecrets) > 0 {
		params["changed_secrets"] = changedSecrets
	}
	recordManageAudit(c, "cpa_subscription.settings_update", params)
	if rotationTouched {
		service.TriggerCpaRotation()
	}
	common.ApiSuccess(c, setting)
}

// applyCpaRotationPatch copies the rotation fields of the request. Turning
// the switch on stamps RotationEnabledAt, so work hours count from that
// moment and nobody goes resting for what it did before.
func applyCpaRotationPatch(setting *model.CpaSubscriptionSetting, req *cpaSettingPatch, now int64) bool {
	wasEnabled := setting.RotationEnabled
	touched := applyRotationSettings(setting, req.RotationEnabled, req.RotationWorkHours, req.RotationRestMinutes,
		req.RotationJitterMinutes, req.RotationMaxRestPercent, req.RotationMinOnline, req.RotationExcluded)
	if applyRotationExtSettings(setting, req) {
		touched = true
	}
	if setting.RotationEnabled && !wasEnabled {
		setting.RotationEnabledAt = now
	}
	return touched
}

// PreviewCpaRotation answers "what would happen if I saved this": the
// request is applied to a copy of the settings and planned, nothing is
// saved and no account is touched.
func PreviewCpaRotation(c *gin.Context) {
	var req cpaSettingPatch
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	draft := *setting
	applyCpaRotationPatch(&draft, &req, common.GetTimestamp())
	summary, err := service.PreviewCpaRotation(&draft)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

// StopCpaRotation is the emergency button: switch rotation off and bring
// every resting account back right away.
func StopCpaRotation(c *gin.Context) {
	woken, failed, err := service.StopCpaRotationNow()
	recordManageAudit(c, "cpa_subscription.rotation_stop", map[string]any{"woken": woken, "failed": failed, "success": err == nil})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"woken": woken, "failed": failed})
}

func applyRotationExtSettings(s *model.CpaSubscriptionSetting, r *cpaSettingPatch) bool {
	touched := false
	b := func(dst *bool, v *bool) {
		if v != nil {
			*dst, touched = *v, true
		}
	}
	n := func(dst *int, v *int) {
		if v != nil {
			*dst, touched = *v, true
		}
	}
	t := func(dst *string, v *string) {
		if v != nil {
			*dst, touched = strings.TrimSpace(*v), true
		}
	}
	b(&s.RotationShowBadges, r.RotationShowBadges)
	b(&s.RotationWake429Enabled, r.RotationWake429Enabled)
	n(&s.RotationWake429Minutes, r.RotationWake429Minutes)
	n(&s.RotationWake429Count, r.RotationWake429Count)
	b(&s.RotationWakeRpmEnabled, r.RotationWakeRpmEnabled)
	n(&s.RotationWakeRpm, r.RotationWakeRpm)
	n(&s.RotationWakeBatch, r.RotationWakeBatch)
	n(&s.RotationWakeCooldownMinutes, r.RotationWakeCooldownMinutes)
	n(&s.RotationWakeHoldMinutes, r.RotationWakeHoldMinutes)
	b(&s.RotationWakeRest, r.RotationWakeRest)
	b(&s.RotationWakeShift, r.RotationWakeShift)
	b(&s.RotationNightEnabled, r.RotationNightEnabled)
	t(&s.RotationNightStart, r.RotationNightStart)
	t(&s.RotationNightEnd, r.RotationNightEnd)
	n(&s.RotationNightMaxRestPercent, r.RotationNightMaxRestPercent)
	b(&s.RotationShiftEnabled, r.RotationShiftEnabled)
	t(&s.RotationDayShiftStart, r.RotationDayShiftStart)
	t(&s.RotationDayShiftEnd, r.RotationDayShiftEnd)
	t(&s.RotationNightShiftStart, r.RotationNightShiftStart)
	t(&s.RotationNightShiftEnd, r.RotationNightShiftEnd)
	n(&s.RotationShiftJitterMinutes, r.RotationShiftJitterMinutes)
	b(&s.RotationLimitEnabled, r.RotationLimitEnabled)
	n(&s.RotationLimitMinutes, r.RotationLimitMinutes)
	n(&s.RotationLimitCount, r.RotationLimitCount)
	n(&s.RotationLimitRestMinutes, r.RotationLimitRestMinutes)
	b(&s.RotationQuotaEnabled, r.RotationQuotaEnabled)
	n(&s.RotationQuotaPercent, r.RotationQuotaPercent)
	b(&s.RotationDailyEnabled, r.RotationDailyEnabled)
	n(&s.RotationDailyHours, r.RotationDailyHours)
	n(&s.RotationBatchSize, r.RotationBatchSize)
	n(&s.RotationBatchGapMinutes, r.RotationBatchGapMinutes)
	b(&s.RotationCapacityEnabled, r.RotationCapacityEnabled)
	n(&s.RotationCapacityRpm, r.RotationCapacityRpm)
	n(&s.RotationCapacityTpm, r.RotationCapacityTpm)
	n(&s.RotationCapacity429Minutes, r.RotationCapacity429Minutes)
	b(&s.RotationDryRun, r.RotationDryRun)
	// Monitoring switches do not need a rotation run.
	b2 := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	b2(&s.AuthStopEnabled, r.AuthStopEnabled)
	b2(&s.AlertWecomEnabled, r.AlertWecomEnabled)
	b2(&s.AlertDingtalkEnabled, r.AlertDingtalkEnabled)
	b2(&s.AlertOnSocks, r.AlertOnSocks)
	b2(&s.AlertOnAuth, r.AlertOnAuth)
	b2(&s.AlertOnCpa, r.AlertOnCpa)
	b2(&s.AlertOnRotation, r.AlertOnRotation)
	return touched
}

// applyCpaAlertSecrets encrypts new webhook URLs / the DingTalk secret.
func applyCpaAlertSecrets(s *model.CpaSubscriptionSetting, r *cpaSettingPatch) ([]string, error) {
	var changed []string
	set := func(name string, dst *string, v *string, isURL bool) error {
		if v == nil || strings.TrimSpace(*v) == "" {
			return nil
		}
		plain := strings.TrimSpace(*v)
		changed = append(changed, name)
		if plain == "-" {
			*dst = ""
			return nil
		}
		if isURL {
			if err := service.ValidateCpaWebhookURL(plain); err != nil {
				return err
			}
		}
		enc, err := service.EncryptCpaSecret(plain)
		if err != nil {
			return err
		}
		*dst = enc
		return nil
	}
	if err := set("wecom_webhook", &s.AlertWecomWebhookEnc, r.AlertWecomWebhook, true); err != nil {
		return nil, err
	}
	if err := set("dingtalk_webhook", &s.AlertDingtalkWebhookEnc, r.AlertDingtalkWebhook, true); err != nil {
		return nil, err
	}
	if err := set("dingtalk_secret", &s.AlertDingtalkSecretEnc, r.AlertDingtalkSecret, false); err != nil {
		return nil, err
	}
	return changed, nil
}

// TestCpaAlertChannel sends a test message to one saved channel.
func TestCpaAlertChannel(c *gin.Context) {
	var req struct {
		Channel string `json:"channel"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	err := service.SendCpaTestNotify(req.Channel)
	recordManageAudit(c, "cpa_subscription.alert_test", map[string]any{"channel": req.Channel, "ok": err == nil})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// GetCpaRateSeries serves the RPM / TPM chart (one account or the pool).
func GetCpaRateSeries(c *gin.Context) {
	instanceID, _ := strconv.Atoi(c.Query("instance_id"))
	series, err := service.BuildCpaRateSeries(instanceID, c.Query("name"), c.DefaultQuery("range", "1h"), common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, series)
}

// SetCpaAccountShiftHandler changes one account's shift.
func SetCpaAccountShiftHandler(c *gin.Context) {
	var req struct {
		InstanceID int    `json:"instance_id"`
		Name       string `json:"name"`
		Shift      string `json:"shift"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.InstanceID <= 0 || strings.TrimSpace(req.Name) == "" {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	err := service.SetCpaAccountShift(req.InstanceID, req.Name, req.Shift)
	recordManageAudit(c, "cpa_subscription.account_shift", map[string]any{"instance_id": req.InstanceID, "name": req.Name, "shift": req.Shift})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// applyRotationSettings copies the provided rotation fields; it reports
// whether any was present. Range checks live in setting.Normalize.
func applyRotationSettings(setting *model.CpaSubscriptionSetting, enabled *bool, work, rest, jitter, percent, minOnline *int, excluded *string) bool {
	touched := false
	setBool := func(dst *bool, v *bool) {
		if v != nil {
			*dst, touched = *v, true
		}
	}
	setInt := func(dst *int, v *int) {
		if v != nil {
			*dst, touched = *v, true
		}
	}
	setBool(&setting.RotationEnabled, enabled)
	setInt(&setting.RotationWorkHours, work)
	setInt(&setting.RotationRestMinutes, rest)
	setInt(&setting.RotationJitterMinutes, jitter)
	setInt(&setting.RotationMaxRestPercent, percent)
	setInt(&setting.RotationMinOnline, minOnline)
	if excluded != nil {
		lines := make([]string, 0)
		for _, line := range strings.Split(*excluded, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				lines = append(lines, line)
			}
		}
		setting.RotationExcluded, touched = strings.Join(lines, "\n"), true
	}
	return touched
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
