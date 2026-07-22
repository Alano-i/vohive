package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

var wecomPlaceholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

const maxWeComResponseBody = 1 << 20

func readWeComResponse(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxWeComResponseBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxWeComResponseBody {
		return nil, errors.New("企业微信响应体过大")
	}
	return data, nil
}

type WeComChannel struct {
	cfg      config.WeComConfig
	client   *http.Client
	handlers map[string]CommandHandler

	tokenMu           sync.Mutex
	cachedAccessToken string
	tokenExpiry       time.Time
}

type wecomAccessTokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type wecomNewsArticle struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	PicURL      string `json:"picurl,omitempty"`
	ButtonText  string `json:"btntxt,omitempty"`
	AppID       string `json:"appid,omitempty"`
	PagePath    string `json:"pagepath,omitempty"`
}

type wecomNewsPayload struct {
	ToUser  string `json:"touser,omitempty"`
	ToParty string `json:"toparty,omitempty"`
	ToTag   string `json:"totag,omitempty"`
	MsgType string `json:"msgtype"`
	AgentID int64  `json:"agentid"`
	News    struct {
		Articles []wecomNewsArticle `json:"articles"`
	} `json:"news"`
	EnableDuplicateCheck   int `json:"enable_duplicate_check,omitempty"`
	DuplicateCheckInterval int `json:"duplicate_check_interval,omitempty"`
}

type wecomAPIResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func NewWeComChannel(cfg config.WeComConfig) (*WeComChannel, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	normalizeWeComConfig(&cfg)
	if cfg.CorpID == "" || cfg.CorpSecret == "" {
		return nil, errors.New("企业微信应用通知缺少 corp_id 或 corp_secret")
	}
	if cfg.AgentID == 0 {
		return nil, errors.New("企业微信应用通知缺少 agent_id")
	}
	if cfg.ToUser == "" && cfg.ToParty == "" && cfg.ToTag == "" {
		return nil, errors.New("企业微信应用通知至少需要配置 touser、toparty 或 totag")
	}

	return &WeComChannel{
		cfg:      cfg,
		handlers: make(map[string]CommandHandler),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func normalizeWeComConfig(cfg *config.WeComConfig) {
	cfg.CorpID = strings.TrimSpace(cfg.CorpID)
	cfg.CorpSecret = strings.TrimSpace(cfg.CorpSecret)
	cfg.ToUser = strings.TrimSpace(cfg.ToUser)
	cfg.ToParty = strings.TrimSpace(cfg.ToParty)
	cfg.ToTag = strings.TrimSpace(cfg.ToTag)
	cfg.ArticleTitle = strings.TrimSpace(cfg.ArticleTitle)
	cfg.ArticleDescription = strings.TrimSpace(cfg.ArticleDescription)
	cfg.ArticleURL = strings.TrimSpace(cfg.ArticleURL)
	cfg.ArticlePicURL = strings.TrimSpace(cfg.ArticlePicURL)
	cfg.ArticleButtonText = strings.TrimSpace(cfg.ArticleButtonText)
	cfg.MiniProgramAppID = strings.TrimSpace(cfg.MiniProgramAppID)
	cfg.MiniProgramPagePath = strings.TrimSpace(cfg.MiniProgramPagePath)
	cfg.APIBaseURL = strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if cfg.ArticleTitle == "" {
		cfg.ArticleTitle = config.DefaultWeComArticleTitle
	}
	if cfg.ArticleDescription == "" {
		cfg.ArticleDescription = config.DefaultWeComArticleDescription
	}
	if cfg.ArticlePicURL == "" {
		cfg.ArticlePicURL = config.DefaultWeComArticlePicURL
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = config.DefaultWeComAPIBaseURL
	}
	if cfg.DuplicateCheckInterval <= 0 {
		cfg.DuplicateCheckInterval = config.DefaultWeComDuplicateCheckSeconds
	}
}

func (w *WeComChannel) Name() string { return "wecom" }

func (w *WeComChannel) Send(text string) error {
	return w.SendWithContext(NotificationContext{
		Event:     "notification",
		Text:      text,
		Timestamp: time.Now(),
	})
}

func (w *WeComChannel) SendWithContext(ctx NotificationContext) error {
	_, err := w.SendWithContextDetailed(ctx)
	return err
}

type SendWeComResult struct {
	ErrCode int
	ErrMsg  string
}

func (w *WeComChannel) SendWithContextDetailed(ctx NotificationContext) (SendWeComResult, error) {
	result := SendWeComResult{}
	if w == nil || w.client == nil {
		return result, nil
	}
	if strings.TrimSpace(ctx.Text) == "" {
		return result, nil
	}
	if ctx.Timestamp.IsZero() {
		ctx.Timestamp = time.Now()
	}
	if strings.TrimSpace(ctx.Event) == "" {
		ctx.Event = "notification"
	}

	token, err := w.accessToken()
	if err != nil {
		return result, err
	}

	payload, err := w.buildPayload(ctx)
	if err != nil {
		return result, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return result, fmt.Errorf("序列化企业微信图文消息失败: %w", err)
	}

	sendURL, err := url.Parse(w.cfg.APIBaseURL + "/cgi-bin/message/send")
	if err != nil {
		return result, fmt.Errorf("解析企业微信发送地址失败: %w", err)
	}
	sendQuery := sendURL.Query()
	sendQuery.Set("access_token", token)
	sendURL.RawQuery = sendQuery.Encode()

	req, err := http.NewRequest(http.MethodPost, sendURL.String(), bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("创建企业微信发送请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "Vohive-WeCom/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return result, fmt.Errorf("企业微信发送请求失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := readWeComResponse(resp.Body)
	if err != nil {
		return result, fmt.Errorf("读取企业微信发送响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("企业微信发送 HTTP 状态码错误: %d", resp.StatusCode)
	}

	var apiResp wecomAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return result, fmt.Errorf("解析企业微信发送响应失败: %w", err)
	}
	result.ErrCode = apiResp.ErrCode
	result.ErrMsg = apiResp.ErrMsg
	if apiResp.ErrCode != 0 {
		return result, fmt.Errorf("企业微信发送失败 %d: %s", apiResp.ErrCode, apiResp.ErrMsg)
	}
	return result, nil
}

func (w *WeComChannel) accessToken() (string, error) {
	w.tokenMu.Lock()
	defer w.tokenMu.Unlock()

	if w.cachedAccessToken != "" && time.Now().Before(w.tokenExpiry.Add(-2*time.Minute)) {
		return w.cachedAccessToken, nil
	}

	tokenURL, err := url.Parse(w.cfg.APIBaseURL + "/cgi-bin/gettoken")
	if err != nil {
		return "", fmt.Errorf("解析企业微信 access_token 地址失败: %w", err)
	}
	tokenQuery := tokenURL.Query()
	tokenQuery.Set("corpid", w.cfg.CorpID)
	tokenQuery.Set("corpsecret", w.cfg.CorpSecret)
	tokenURL.RawQuery = tokenQuery.Encode()

	req, err := http.NewRequest(http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("创建企业微信 access_token 请求失败: %w", err)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求企业微信 access_token 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := readWeComResponse(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取企业微信 access_token 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("企业微信 access_token HTTP 状态码错误: %d", resp.StatusCode)
	}

	var tokenResp wecomAccessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("解析企业微信 access_token 响应失败: %w", err)
	}
	if tokenResp.ErrCode != 0 {
		return "", fmt.Errorf("企业微信 access_token 获取失败 %d: %s", tokenResp.ErrCode, tokenResp.ErrMsg)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return "", errors.New("企业微信 access_token 响应为空")
	}
	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
	}
	w.cachedAccessToken = tokenResp.AccessToken
	w.tokenExpiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return w.cachedAccessToken, nil
}

func (w *WeComChannel) buildPayload(ctx NotificationContext) (wecomNewsPayload, error) {
	title, description := w.articleText(ctx)
	url := strings.TrimSpace(w.render(w.cfg.ArticleURL, ctx))

	payload := wecomNewsPayload{
		ToUser:  w.cfg.ToUser,
		ToParty: w.cfg.ToParty,
		ToTag:   w.cfg.ToTag,
		MsgType: "news",
		AgentID: w.cfg.AgentID,
	}
	payload.News.Articles = []wecomNewsArticle{{
		Title:       clampRunes(title, 128),
		Description: clampRunes(description, 512),
		URL:         url,
		PicURL:      w.render(w.cfg.ArticlePicURL, ctx),
		ButtonText:  w.render(w.cfg.ArticleButtonText, ctx),
		AppID:       w.cfg.MiniProgramAppID,
		PagePath:    w.render(w.cfg.MiniProgramPagePath, ctx),
	}}
	if w.cfg.EnableDuplicateCheck {
		payload.EnableDuplicateCheck = 1
		payload.DuplicateCheckInterval = w.cfg.DuplicateCheckInterval
	}
	return payload, nil
}

func (w *WeComChannel) articleText(ctx NotificationContext) (string, string) {
	if strings.TrimSpace(ctx.Event) != "sms_received" {
		return w.titleForContext(ctx), strings.TrimSpace(ctx.Text)
	}

	title := w.render(w.cfg.ArticleTitle, ctx)
	if strings.TrimSpace(title) == "" {
		title = w.titleForContext(ctx)
	}
	description := w.render(w.cfg.ArticleDescription, ctx)
	if strings.TrimSpace(description) == "" {
		description = ctx.Text
	}
	return title, description
}

func (w *WeComChannel) titleForContext(ctx NotificationContext) string {
	label := ctx.DeviceLabel()
	switch ctx.Event {
	case "sms_received":
		return "新短信 - " + label
	case "incoming_call":
		return "来电通知 - " + label
	case "ip_rotated":
		return "公网 IP 已切换 - " + label
	default:
		if label == "未知设备" {
			return "VoHive 通知"
		}
		return "VoHive 通知 - " + label
	}
}

func (w *WeComChannel) render(template string, ctx NotificationContext) string {
	values := map[string]string{
		"text":            strings.TrimSpace(ctx.Text),
		"event":           strings.TrimSpace(ctx.Event),
		"timestamp":       ctx.Timestamp.Format("2006-01-02 15:04:05"),
		"device_id":       strings.TrimSpace(ctx.DeviceID),
		"device_name":     strings.TrimSpace(ctx.DeviceName),
		"device_label":    ctx.DeviceLabel(),
		"sms_sender":      strings.TrimSpace(ctx.SMSSender),
		"sms_receiver":    strings.TrimSpace(ctx.SMSReceiver),
		"sms_text":        strings.TrimSpace(ctx.SMSText),
		"sms_source":      strings.TrimSpace(ctx.SMSSource),
		"sender_number":   strings.TrimSpace(ctx.SMSSender),
		"receiver_number": strings.TrimSpace(ctx.SMSReceiver),
	}
	return wecomPlaceholderPattern.ReplaceAllStringFunc(template, func(token string) string {
		matches := wecomPlaceholderPattern.FindStringSubmatch(token)
		if len(matches) != 2 {
			return token
		}
		if v, ok := values[matches[1]]; ok {
			return v
		}
		return token
	})
}

func clampRunes(s string, max int) string {
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= max {
		return string(rs)
	}
	return string(rs[:max])
}

func (w *WeComChannel) RegisterCommand(_ string, _ CommandHandler) {}
func (w *WeComChannel) Start() error                               { return nil }
func (w *WeComChannel) Close() error {
	if w != nil && w.client != nil {
		w.client.CloseIdleConnections()
	}
	return nil
}
