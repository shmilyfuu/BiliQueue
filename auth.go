package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type BiliAuth struct {
	UID      int64  `json:"uid"`
	Username string `json:"username"`
	Cookie   string `json:"cookie"`
}

type QRLoginPollResult struct {
	Status  string
	Message string
	Auth    BiliAuth
}

func (b *BiliClient) StartQRLogin(ctx context.Context) (string, string, error) {
	endpoint := "https://passport.bilibili.com/x/passport-login/web/qrcode/generate?source=main-fe-header"
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			URL       string `json:"url"`
			QRCodeKey string `json:"qrcode_key"`
		} `json:"data"`
	}
	if err := b.getJSON(ctx, endpoint, "", &resp); err != nil {
		return "", "", err
	}
	if resp.Code != 0 || resp.Data.URL == "" || resp.Data.QRCodeKey == "" {
		return "", "", fmt.Errorf("二维码获取失败：%s", resp.Message)
	}
	return resp.Data.URL, resp.Data.QRCodeKey, nil
}

func (b *BiliClient) PollQRLogin(ctx context.Context, key string) (QRLoginPollResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return QRLoginPollResult{}, errors.New("二维码密钥为空")
	}
	params := url.Values{"qrcode_key": {key}, "source": {"main-fe-header"}}
	endpoint := "https://passport.bilibili.com/x/passport-login/web/qrcode/poll?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return QRLoginPollResult{}, err
	}
	setBiliHeaders(req, "")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return QRLoginPollResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return QRLoginPollResult{}, fmt.Errorf("登录轮询 HTTP %d", resp.StatusCode)
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body); err != nil {
		return QRLoginPollResult{}, err
	}
	if body.Code != 0 {
		return QRLoginPollResult{}, fmt.Errorf("登录轮询失败：%s", body.Message)
	}
	switch body.Data.Code {
	case 86101:
		return QRLoginPollResult{Status: "waiting", Message: "等待扫码"}, nil
	case 86090:
		return QRLoginPollResult{Status: "scanned", Message: "已扫码，请在手机上确认"}, nil
	case 86038:
		return QRLoginPollResult{Status: "expired", Message: "二维码已过期"}, nil
	case 0:
		cookie := cookiesToHeader(resp.Cookies())
		if cookie == "" {
			return QRLoginPollResult{}, errors.New("登录成功，但没有取得登录 Cookie")
		}
		uid := cookieInt64(cookie, "DedeUserID")
		username := ""
		if navUID, navName, err := b.fetchLoginProfile(ctx, cookie); err == nil {
			if navUID > 0 {
				uid = navUID
			}
			username = navName
		}
		if uid <= 0 {
			return QRLoginPollResult{}, errors.New("登录成功，但没有取得用户 UID")
		}
		return QRLoginPollResult{
			Status:  "success",
			Message: "登录成功",
			Auth: BiliAuth{
				UID:      uid,
				Username: username,
				Cookie:   cookie,
			},
		}, nil
	default:
		message := body.Data.Message
		if message == "" {
			message = fmt.Sprintf("登录状态码 %d", body.Data.Code)
		}
		return QRLoginPollResult{Status: "error", Message: message}, nil
	}
}

func (b *BiliClient) fetchLoginProfile(ctx context.Context, cookie string) (int64, string, error) {
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Mid   int64  `json:"mid"`
			Uname string `json:"uname"`
		} `json:"data"`
	}
	if err := b.getJSON(ctx, "https://api.bilibili.com/x/web-interface/nav", cookie, &resp); err != nil {
		return 0, "", err
	}
	if resp.Code != 0 || resp.Data.Mid <= 0 {
		return 0, "", fmt.Errorf("登录状态校验失败：%s", resp.Message)
	}
	return resp.Data.Mid, resp.Data.Uname, nil
}

func cookiesToHeader(cookies []*http.Cookie) string {
	values := make(map[string]string)
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		values[cookie.Name] = cookie.Value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, "; ")
}

func cookieMap(cookie string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(cookie, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || name == "" {
			continue
		}
		out[name] = value
	}
	return out
}

func cookieInt64(cookie, key string) int64 {
	value := cookieMap(cookie)[key]
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}

func mergeCookie(cookie string, extra map[string]string) string {
	values := cookieMap(cookie)
	for key, value := range extra {
		if value != "" {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, "; ")
}
