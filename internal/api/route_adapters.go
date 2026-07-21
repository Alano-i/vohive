package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/device"
)

type enabledPatchRequest struct {
	Enabled *bool `json:"enabled"`
}

type networkPatchRequest struct {
	Enabled   *bool  `json:"enabled"`
	IPVersion string `json:"ip_version"`
	APN       string `json:"apn"`
}

func (s *Server) handleDeviceNetworkPatch(c *gin.Context) {
	var req networkPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "enabled 为必填项"})
		return
	}

	deviceID := deviceIDParam(c)

	if *req.Enabled {
		worker := s.pool.GetWorker(deviceID)
		if worker == nil {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到"})
			return
		}
		iccid := strings.TrimSpace(worker.CurrentICCID())
		if iccid == "" {
			refreshCtx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
			_ = worker.RefreshIdentityLive(refreshCtx, "network_enable")
			cancel()
			iccid = strings.TrimSpace(worker.CurrentICCID())
		}
		if iccid == "" {
			c.JSON(http.StatusConflict, gin.H{"status": "error", "message": "未检测到 SIM 卡，无法启动数据网络"})
			return
		}
		previous, err := db.ResolveCardPolicy(iccid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "获取卡策略失败: " + err.Error()})
			return
		}

		// 落库：network_enabled=true + ip_version + apn（APN/IP 供下次连接生效）
		ipVersion := strings.TrimSpace(req.IPVersion)
		apn := strings.TrimSpace(req.APN)
		_, applied, err := s.patchCardPolicyForDevice(deviceID, func(p *db.CardPolicy) {
			p.NetworkEnabled = true
			p.VoWiFiEnabled = false
			p.AirplaneEnabled = false
			if ipVersion != "" {
				p.IPVersion = ipVersion
			}
			p.APN = apn
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		if !applied {
			c.JSON(http.StatusConflict, gin.H{"status": "error", "message": "未检测到 SIM 卡，无法启动数据网络"})
			return
		}
		if ipVersion == "" {
			ipVersion = previous.IPVersion
		}
		s.pool.SetWorkerCardPolicyProjection(deviceID, true, false, false, ipVersion, apn)

		worker, nc, statusCode, startErr := s.startDeviceNetwork(deviceID)
		if startErr == nil {
			go func() { _ = worker.RefreshRuntime(context.Background(), "start_network") }()
			c.JSON(http.StatusOK, gin.H{
				"status": "ok", "message": "数据网络已启动", "device": deviceID,
				"network_connected": worker.NetworkConnected(),
				"private_ip":        nc.GetPrivateIP(), "private_ipv6": nc.GetPrivateIPv6(),
				"public_ip": worker.GetCachedIP(), "public_ipv6": worker.GetCachedIPv6(),
			})
			return
		}

		if statusCode == http.StatusInternalServerError && isRecoverableQMINetworkStartError(worker, startErr) {
			if recoveryErr := s.beginNetworkControlRecovery(c.Request.Context(), worker); recoveryErr == nil {
				c.JSON(http.StatusAccepted, gin.H{
					"status": "ok", "message": "正在恢复模块并连接数据网络", "device": deviceID,
					"connecting": true, "network_connected": false,
				})
				return
			} else {
				startErr = fmt.Errorf("%w；自动恢复失败: %v", startErr, recoveryErr)
			}
		}

		if rollbackErr := restoreCardPolicyAfterNetworkStartFailure(s, deviceID, previous); rollbackErr != nil {
			startErr = fmt.Errorf("%w；恢复原网络设置失败: %v", startErr, rollbackErr)
		}
		c.JSON(statusCode, gin.H{"status": "error", "message": startErr.Error()})
		return
	}

	// enabled=false：落库 network_enabled=false
	iccid, applied, err := s.patchCardPolicyForDevice(deviceID, func(p *db.CardPolicy) {
		p.NetworkEnabled = false
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if applied {
		if p, resolveErr := db.ResolveCardPolicy(iccid); resolveErr == nil {
			s.pool.SetWorkerCardPolicyProjection(deviceID, p.NetworkEnabled, p.VoWiFiEnabled, p.AirplaneEnabled, p.IPVersion, p.APN)
		}
	}
	s.handleDeviceMgmtStopNetwork(c)
}

func restoreCardPolicyAfterNetworkStartFailure(s *Server, deviceID string, previous db.CardPolicy) error {
	if err := db.UpsertCardPolicy(previous); err != nil {
		return err
	}
	s.pool.SetWorkerCardPolicyProjection(deviceID, previous.NetworkEnabled, previous.VoWiFiEnabled, previous.AirplaneEnabled, previous.IPVersion, previous.APN)
	return nil
}

func isRecoverableQMINetworkStartError(worker *device.Worker, err error) bool {
	if worker == nil || err == nil {
		return false
	}
	isQMI := worker.QMICore != nil || strings.EqualFold(strings.TrimSpace(worker.Config.DeviceBackend), backend.BackendQMI)
	if worker.Backend != nil {
		isQMI = isQMI || strings.EqualFold(strings.TrimSpace(worker.Backend.Mode()), backend.BackendQMI)
	}
	if !isQMI {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"manager core not started",
		"qmi core not started",
		"qmi service not ready",
		"qmi 服务未就绪",
		"transaction timed out",
		"qmi control not ready",
		"context deadline exceeded",
		"timeout",
		"error=0x001a",
		"no effect",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func isRecoverableDeviceControlError(worker *device.Worker, err error) bool {
	if err == nil {
		return false
	}
	if isRecoverableQMINetworkStartError(worker, err) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"at 管理器未启动",
		"at manager not started",
		"port has been closed",
		"port closed",
		"serial port",
		"no such file",
		"device disconnected",
		"设备未连接",
		"context deadline exceeded",
		"timeout",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func (s *Server) beginNetworkControlRecovery(ctx context.Context, worker *device.Worker) error {
	if s.networkRecovery != nil {
		return s.networkRecovery(ctx, worker)
	}
	if worker == nil {
		return fmt.Errorf("设备未找到")
	}
	// Bootstrap already owns the single QMI start/retry loop. Starting a second
	// QMI StartCore call here would cross old/new transactions. Escalate through
	// the existing deduplicated worker rebuild path instead, which closes the
	// stale core before rediscovery and reapplies the saved network preference.
	s.pool.MarkLifecycleRecovery(worker.ID, device.LifecyclePhaseQMIStarting, "network_enable_wait_qmi", 3*time.Minute)
	s.scheduleDeviceControlRecovery(worker, "network_enable_qmi_recovery")
	return nil
}

func (s *Server) scheduleDeviceControlRecovery(worker *device.Worker, reason string) {
	if worker == nil || s.pool == nil {
		return
	}
	if s.controlRecovery != nil {
		s.controlRecovery(worker, reason)
		return
	}
	if s.pool.GetWorker(worker.ID) == worker {
		s.pool.ScheduleNetworkControlRecovery(worker, reason)
	}
}

func (s *Server) handleDeviceVoWiFiPatch(c *gin.Context) {
	var req enabledPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "enabled 为必填项"})
		return
	}

	deviceID := deviceIDParam(c)
	if s.pool == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "服务未就绪"})
		return
	}

	if *req.Enabled {
		// 落库：仅置 vowifi_enabled=true。不碰 airplane_enabled——它是用户的纯飞行
		// 意图，作为关闭 VoWiFi 后的回退依据；VoWiFi 接管射频由运行时投影派生。
		iccid, applied, err := s.patchCardPolicyForDevice(deviceID, vowifiEnablePolicyMutation)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		if !applied {
			c.JSON(http.StatusConflict, gin.H{"status": "error", "message": "设备尚未识别到 SIM 卡 ICCID，无法保存 VoWiFi 策略"})
			return
		}
		// 同步 w.Config，使概览即时切到 VoWiFi 模式面板（EnableVoWiFi 不碰 Config）。
		s.pool.SetWorkerVoWiFiPolicy(deviceID, true)
		if err := s.requestVoWiFiEnable(deviceID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": "VoWiFi 启动请求失败: " + err.Error(),
				"device":  deviceID,
			})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{
			"status":  "accepted",
			"message": "VoWiFi 开启目标已保存，正在后台建立连接",
			"device":  deviceID,
			"iccid":   iccid,
			"desired": true,
		})
		return
	}

	// 落库：仅清 vowifi_enabled=false，保留 airplane_enabled（用户飞行意图）。
	// 关闭 VoWiFi 后 DisableVoWiFi 会按当前卡策略重投影：之前是飞行则回飞行，否则回在线。
	_, applied, err := s.patchCardPolicyForDevice(deviceID, vowifiDisablePolicyMutation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if !applied {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "message": "设备尚未识别到 SIM 卡 ICCID，无法保存 VoWiFi 策略"})
		return
	}
	s.pool.SetWorkerVoWiFiPolicy(deviceID, false)
	s.handleVoWiFiDisable(c)
}

func (s *Server) requestVoWiFiEnable(deviceID string) error {
	if s.vowifiEnableRequest != nil {
		return s.vowifiEnableRequest(deviceID)
	}
	return s.pool.RequestEnableVoWiFi(deviceID)
}

// vowifiEnablePolicyMutation 开 VoWiFi 的落库副作用：只置 vowifi，飞行意图保持不变。
func vowifiEnablePolicyMutation(p *db.CardPolicy) { p.VoWiFiEnabled = true }

// vowifiDisablePolicyMutation 关 VoWiFi 的落库副作用：只清 vowifi，保留用户飞行意图以便回退。
func vowifiDisablePolicyMutation(p *db.CardPolicy) { p.VoWiFiEnabled = false }
