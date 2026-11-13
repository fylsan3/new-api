package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// Alert push to WeCom / DingTalk group robots (user decision 2026-11-12:
// both channels, four event types, each with its own switch). Webhook URLs
// and the DingTalk sign secret are stored encrypted like CPA keys and never
// sent back to the browser.

type CpaNotifyKind string

const (
	CpaNotifySocks    CpaNotifyKind = "socks"
	CpaNotifyAuth     CpaNotifyKind = "auth"
	CpaNotifyCpa      CpaNotifyKind = "cpa"
	CpaNotifyRotation CpaNotifyKind = "rotation"

	cpaNotifyTimeout = 10 * time.Second
	// The same event of the same account is pushed at most once per window.
	cpaNotifyDedupWindow = 5 * time.Minute
)

var (
	cpaNotifyMu   sync.Mutex
	cpaNotifySent = map[string]int64{}
	// cpaNotifyPost is replaced in tests.
	cpaNotifyPost = postCpaWebhook
)

var cpaNotifyTitles = map[CpaNotifyKind]string{
	CpaNotifySocks:    "SOCKS 不通，账号已自动停用",
	CpaNotifyAuth:     "上游返回 401/403，账号已自动停用",
	CpaNotifyCpa:      "CPA 连接 / 用量采集状态变化",
	CpaNotifyRotation: "账号轮休 / 排班上下线",
}

func cpaBackgroundContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func cpaNotifyKindOn(s *model.CpaSubscriptionSetting, kind CpaNotifyKind) bool {
	switch kind {
	case CpaNotifySocks:
		return s.AlertOnSocks
	case CpaNotifyAuth:
		return s.AlertOnAuth
	case CpaNotifyCpa:
		return s.AlertOnCpa
	case CpaNotifyRotation:
		return s.AlertOnRotation
	}
	return false
}

// NotifyCpaEvent pushes one event to every enabled channel. It never
// blocks the caller and never fails it: push errors only go to the log.
func NotifyCpaEvent(kind CpaNotifyKind, instanceID int, name, message string) {
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil || !cpaNotifyKindOn(setting, kind) {
		return
	}
	if !(setting.AlertWecomEnabled && setting.AlertWecomWebhookEnc != "") &&
		!(setting.AlertDingtalkEnabled && setting.AlertDingtalkWebhookEnc != "") {
		return
	}
	now := time.Now().Unix()
	dedupKey := fmt.Sprintf("%s|%d|%s|%s", kind, instanceID, name, message)
	cpaNotifyMu.Lock()
	if last, ok := cpaNotifySent[dedupKey]; ok && now-last < int64(cpaNotifyDedupWindow/time.Second) {
		cpaNotifyMu.Unlock()
		return
	}
	cpaNotifySent[dedupKey] = now
	for k, ts := range cpaNotifySent {
		if now-ts >= int64(cpaNotifyDedupWindow/time.Second) {
			delete(cpaNotifySent, k)
		}
	}
	cpaNotifyMu.Unlock()

	title, text := formatCpaNotify(kind, instanceID, name, message, now)
	gopool.Go(func() {
		for _, err := range sendCpaNotify(setting, title, text, false) {
			common.SysError("cpa alert push failed: " + err.Error())
		}
	})
}

func formatCpaNotify(kind CpaNotifyKind, instanceID int, name, message string, now int64) (string, string) {
	title := "New API 告警：" + cpaNotifyTitles[kind]
	lines := []string{"### " + title}
	if instance, err := model.GetCpaInstance(instanceID); err == nil && instance != nil {
		lines = append(lines, "- CPA："+instance.Name)
	}
	if name != "" {
		lines = append(lines, "- 账号："+name)
	}
	lines = append(lines,
		"- 详情："+message,
		"- 时间："+time.Unix(now, 0).Format("2006-01-02 15:04:05"),
	)
	return title, strings.Join(lines, "\n")
}

// sendCpaNotify posts to the enabled channels (force: every saved channel,
// used by the test button). It returns one error per failed channel.
func sendCpaNotify(s *model.CpaSubscriptionSetting, title, text string, force bool) []error {
	var errs []error
	if (s.AlertWecomEnabled || force) && s.AlertWecomWebhookEnc != "" {
		if hook, err := DecryptCpaSecret(s.AlertWecomWebhookEnc); err != nil {
			errs = append(errs, fmt.Errorf("WeCom: %w", err))
		} else if err := cpaNotifyPost(hook, map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"content": text},
		}); err != nil {
			errs = append(errs, fmt.Errorf("WeCom: %w", err))
		}
	}
	if (s.AlertDingtalkEnabled || force) && s.AlertDingtalkWebhookEnc != "" {
		hook, err := DecryptCpaSecret(s.AlertDingtalkWebhookEnc)
		if err == nil && s.AlertDingtalkSecretEnc != "" {
			var secret string
			if secret, err = DecryptCpaSecret(s.AlertDingtalkSecretEnc); err == nil {
				hook, err = signDingtalkWebhook(hook, secret, time.Now().UnixMilli())
			}
		}
		if err == nil {
			err = cpaNotifyPost(hook, map[string]any{
				"msgtype":  "markdown",
				"markdown": map[string]any{"title": title, "text": text},
			})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("DingTalk: %w", err))
		}
	}
	return errs
}

// signDingtalkWebhook appends timestamp + sign ("加签" security setting).
func signDingtalkWebhook(hook, secret string, tsMillis int64) (string, error) {
	u, err := url.Parse(hook)
	if err != nil {
		return "", err
	}
	ts := strconv.FormatInt(tsMillis, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ValidateCpaWebhookURL accepts http(s) URLs only.
func ValidateCpaWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("webhook 地址格式不正确，应以 https:// 开头")
	}
	return nil
}

func postCpaWebhook(hook string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cpaNotifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: cpaNotifyTimeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateText(string(raw), 200))
	}
	// Both robots answer 200 with {"errcode":0,"errmsg":"ok"} on success.
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if json.Unmarshal(raw, &result) == nil && result.ErrCode != 0 {
		return fmt.Errorf("errcode %d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

// SendCpaTestNotify pushes a test message to one saved channel
// ("wecom" | "dingtalk") regardless of the switches.
func SendCpaTestNotify(channel string) error {
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		return err
	}
	probe := *setting
	probe.AlertWecomEnabled, probe.AlertDingtalkEnabled = false, false
	switch channel {
	case "wecom":
		if probe.AlertDingtalkWebhookEnc = ""; probe.AlertWecomWebhookEnc == "" {
			return errors.New("还没有保存企业微信 webhook 地址")
		}
	case "dingtalk":
		if probe.AlertWecomWebhookEnc = ""; probe.AlertDingtalkWebhookEnc == "" {
			return errors.New("还没有保存钉钉 webhook 地址")
		}
	default:
		return errors.New("unknown channel")
	}
	title := "New API 告警：测试消息"
	text := "### " + title + "\n- 这是一条测试消息，收到说明告警推送配置正确。\n- 时间：" + time.Now().Format("2006-01-02 15:04:05")
	if errs := sendCpaNotify(&probe, title, text, true); len(errs) > 0 {
		return errs[0]
	}
	return nil
}
