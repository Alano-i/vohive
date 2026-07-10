package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/notify"
)

type testWeComRequest struct {
	Enabled                bool   `json:"enabled"`
	CorpID                 string `json:"corp_id"`
	CorpSecret             string `json:"corp_secret"`
	AgentID                int64  `json:"agent_id"`
	ToUser                 string `json:"touser"`
	ToParty                string `json:"toparty"`
	ToTag                  string `json:"totag"`
	ArticleTitle           string `json:"article_title"`
	ArticleDescription     string `json:"article_description"`
	ArticleURL             string `json:"article_url"`
	ArticlePicURL          string `json:"article_picurl"`
	ArticleButtonText      string `json:"article_button_text"`
	MiniProgramAppID       string `json:"mini_program_appid"`
	MiniProgramPagePath    string `json:"mini_program_pagepath"`
	EnableDuplicateCheck   bool   `json:"enable_duplicate_check"`
	DuplicateCheckInterval int    `json:"duplicate_check_interval"`
	APIBaseURL             string `json:"api_base_url"`
}

func (s *Server) handleTestWeComNotification(c *gin.Context) {
	var req testWeComRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	if !req.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先启用企业微信应用通知后再测试"})
		return
	}

	cfg := config.WeComConfig{
		Enabled:                true,
		CorpID:                 strings.TrimSpace(req.CorpID),
		CorpSecret:             strings.TrimSpace(req.CorpSecret),
		AgentID:                req.AgentID,
		ToUser:                 strings.TrimSpace(req.ToUser),
		ToParty:                strings.TrimSpace(req.ToParty),
		ToTag:                  strings.TrimSpace(req.ToTag),
		ArticleTitle:           strings.TrimSpace(req.ArticleTitle),
		ArticleDescription:     strings.TrimSpace(req.ArticleDescription),
		ArticleURL:             strings.TrimSpace(req.ArticleURL),
		ArticlePicURL:          strings.TrimSpace(req.ArticlePicURL),
		ArticleButtonText:      strings.TrimSpace(req.ArticleButtonText),
		MiniProgramAppID:       strings.TrimSpace(req.MiniProgramAppID),
		MiniProgramPagePath:    strings.TrimSpace(req.MiniProgramPagePath),
		EnableDuplicateCheck:   req.EnableDuplicateCheck,
		DuplicateCheckInterval: req.DuplicateCheckInterval,
		APIBaseURL:             strings.TrimSpace(req.APIBaseURL),
	}

	ch, err := notify.NewWeComChannel(cfg)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "企业微信应用通知配置无效: " + err.Error()})
		return
	}
	if ch == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "企业微信应用通知测试发送器未初始化"})
		return
	}
	defer ch.Close()

	now := time.Now()
	ctx := notify.NotificationContext{
		Event:      "wecom_test",
		Text:       "这是一条企业微信应用图文测试通知",
		DeviceID:   "test_device_001",
		DeviceName: "测试设备",
		Timestamp:  now,
	}

	result, sendErr := ch.SendWithContextDetailed(ctx)
	if sendErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":      false,
			"message": "测试通知发送失败: " + sendErr.Error(),
			"errcode": result.ErrCode,
			"errmsg":  result.ErrMsg,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "测试通知已发送",
		"errcode": result.ErrCode,
		"errmsg":  result.ErrMsg,
	})
}
