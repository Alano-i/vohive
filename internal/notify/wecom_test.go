package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

func TestWeComChannelSendsNewsPayloadAndCachesToken(t *testing.T) {
	var tokenRequests int
	var sendRequests int
	var gotPayload wecomNewsPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokenRequests++
			if r.URL.Query().Get("corpid") != "corp-1" {
				t.Fatalf("corpid=%q", r.URL.Query().Get("corpid"))
			}
			if r.URL.Query().Get("corpsecret") != "secret-1" {
				t.Fatalf("corpsecret=%q", r.URL.Query().Get("corpsecret"))
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-1","expires_in":7200}`))
		case "/cgi-bin/message/send":
			sendRequests++
			if r.URL.Query().Get("access_token") != "token-1" {
				t.Fatalf("access_token=%q", r.URL.Query().Get("access_token"))
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotPayload); err != nil {
				t.Fatalf("payload json: %v", err)
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	ch, err := NewWeComChannel(config.WeComConfig{
		Enabled:                true,
		CorpID:                 "corp-1",
		CorpSecret:             "secret-1",
		AgentID:                100001,
		ToUser:                 "UserID1|UserID2",
		ArticleTitle:           "{{event}} - {{device_label}} - {{sms_sender}}",
		ArticleDescription:     "{{timestamp}} {{sms_receiver}} {{sms_text}}",
		ArticleURL:             "https://example.com/detail?device={{device_id}}",
		ArticlePicURL:          "https://example.com/pic.png",
		ArticleButtonText:      "查看",
		MiniProgramAppID:       "wx123",
		MiniProgramPagePath:    "pages/index",
		EnableDuplicateCheck:   true,
		DuplicateCheckInterval: 1800,
		APIBaseURL:             srv.URL,
	})
	if err != nil {
		t.Fatalf("NewWeComChannel() error = %v", err)
	}

	ctx := NotificationContext{
		Event:       "sms_received",
		Text:        "hello",
		DeviceID:    "dev-1",
		DeviceName:  "测试设备",
		SMSSender:   "+8613800138000",
		SMSReceiver: "+8613900139000",
		SMSText:     "短信原始内容",
		Timestamp:   time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC),
	}
	if err := ch.SendWithContext(ctx); err != nil {
		t.Fatalf("SendWithContext() error = %v", err)
	}
	if err := ch.SendWithContext(ctx); err != nil {
		t.Fatalf("second SendWithContext() error = %v", err)
	}

	if tokenRequests != 1 {
		t.Fatalf("tokenRequests=%d, want 1", tokenRequests)
	}
	if sendRequests != 2 {
		t.Fatalf("sendRequests=%d, want 2", sendRequests)
	}
	if gotPayload.MsgType != "news" {
		t.Fatalf("msgtype=%q, want news", gotPayload.MsgType)
	}
	if gotPayload.AgentID != 100001 {
		t.Fatalf("agentid=%d", gotPayload.AgentID)
	}
	if gotPayload.ToUser != "UserID1|UserID2" {
		t.Fatalf("touser=%q", gotPayload.ToUser)
	}
	if gotPayload.EnableDuplicateCheck != 1 || gotPayload.DuplicateCheckInterval != 1800 {
		t.Fatalf("duplicate fields=%d/%d", gotPayload.EnableDuplicateCheck, gotPayload.DuplicateCheckInterval)
	}
	if len(gotPayload.News.Articles) != 1 {
		t.Fatalf("articles=%d, want 1", len(gotPayload.News.Articles))
	}
	article := gotPayload.News.Articles[0]
	if article.Title != "sms_received - 测试设备 (dev-1) - +8613800138000" {
		t.Fatalf("title=%q", article.Title)
	}
	if article.Description != "2026-07-11 10:00:00 +8613900139000 短信原始内容" {
		t.Fatalf("description=%q", article.Description)
	}
	if article.URL != "https://example.com/detail?device=dev-1" {
		t.Fatalf("url=%q", article.URL)
	}
	if article.PicURL != "https://example.com/pic.png" || article.ButtonText != "查看" {
		t.Fatalf("article=%+v", article)
	}
	if article.AppID != "wx123" || article.PagePath != "pages/index" {
		t.Fatalf("mini program fields=%+v", article)
	}
}

func TestWeComChannelRequiresRecipient(t *testing.T) {
	_, err := NewWeComChannel(config.WeComConfig{
		Enabled:    true,
		CorpID:     "corp",
		CorpSecret: "secret",
		AgentID:    1,
	})
	if err == nil {
		t.Fatal("expected missing recipient error")
	}

	ch, err := NewWeComChannel(config.WeComConfig{
		Enabled:    true,
		CorpID:     "corp",
		CorpSecret: "secret",
		AgentID:    1,
		ToUser:     "@all",
	})
	if err != nil {
		t.Fatalf("NewWeComChannel() with empty article_url error = %v", err)
	}
	if ch == nil {
		t.Fatal("expected channel")
	}
	payload, err := ch.buildPayload(NotificationContext{
		Event:     "sms_received",
		Text:      "hello",
		Timestamp: time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("buildPayload() with empty article_url error = %v", err)
	}
	if len(payload.News.Articles) != 1 {
		t.Fatalf("articles=%d, want 1", len(payload.News.Articles))
	}
	if payload.News.Articles[0].URL != "" {
		t.Fatalf("url=%q, want empty", payload.News.Articles[0].URL)
	}
}

func TestWeComChannelNonSMSEventUsesEventText(t *testing.T) {
	ch := &WeComChannel{cfg: config.WeComConfig{
		ArticleTitle:       config.DefaultWeComArticleTitle,
		ArticleDescription: config.DefaultWeComArticleDescription,
		ArticlePicURL:      config.DefaultWeComArticlePicURL,
	}}

	payload, err := ch.buildPayload(NotificationContext{
		Event:     "raw",
		Text:      "设备 DJI Baiwang3 SIM 卡掉线: SIM 卡未插入或状态异常",
		Timestamp: time.Date(2026, 7, 13, 21, 57, 15, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("buildPayload() error = %v", err)
	}
	article := payload.News.Articles[0]
	if article.Title != "VoHive 通知" {
		t.Fatalf("title=%q, want VoHive 通知", article.Title)
	}
	if article.Description != "设备 DJI Baiwang3 SIM 卡掉线: SIM 卡未插入或状态异常" {
		t.Fatalf("description=%q", article.Description)
	}
}
