package device

import (
	"testing"

	"github.com/iniwex5/vohive/internal/config"
)

func newPoolWithWorkerForSync(id string, cfg config.DeviceConfig) (*Pool, *Worker) {
	p := &Pool{workers: map[string]*Worker{}}
	w := &Worker{ID: id, Config: cfg}
	p.workers[id] = w
	return p, w
}

// 开 VoWiFi：同步 w.Config 为 vowifi=T、airplane=T、network=F（否则概览仍显示蜂窝面板）。
// 关 VoWiFi：仅清 vowifi，不在此清 airplane——airplane 是用户飞行意图，交由
// resolveAndApplyPolicy 按卡策略重投影回退。
func TestSetWorkerVoWiFiPolicySyncsConfig(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{NetworkEnabled: true})

	p.SetWorkerVoWiFiPolicy("wwan0", true)
	cfg := w.ConfigSnapshot()
	if !cfg.VoWiFiEnabled || !cfg.AirplaneEnabled || cfg.NetworkEnabled {
		t.Fatalf("开 vowifi 应 vowifi=T airplane=T network=F: %+v", cfg)
	}

	p.SetWorkerVoWiFiPolicy("wwan0", false)
	if cfg := w.ConfigSnapshot(); cfg.VoWiFiEnabled {
		t.Fatalf("关 vowifi 应清 vowifi=F: %+v", cfg)
	}
}

// 开飞行：同步 airplane=T、vowifi=F、network=F；关飞行仅清 airplane。
func TestSetWorkerAirplanePolicySyncsConfig(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{VoWiFiEnabled: true, NetworkEnabled: true})

	p.SetWorkerAirplanePolicy("wwan0", true)
	cfg := w.ConfigSnapshot()
	if !cfg.AirplaneEnabled || cfg.VoWiFiEnabled || cfg.NetworkEnabled {
		t.Fatalf("开飞行应 airplane=T vowifi=F network=F: %+v", cfg)
	}

	p.SetWorkerAirplanePolicy("wwan0", false)
	if cfg := w.ConfigSnapshot(); cfg.AirplaneEnabled {
		t.Fatalf("关飞行应 airplane=F: %+v", cfg)
	}
}

// 开网络：互斥关 vowifi/airplane，并同步 ip/apn。
func TestSetWorkerNetworkPolicyMutualExclusion(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{VoWiFiEnabled: true, AirplaneEnabled: true})

	p.SetWorkerNetworkPolicy("wwan0", true, "v4v6", "ims")
	cfg := w.ConfigSnapshot()
	if !cfg.NetworkEnabled || cfg.VoWiFiEnabled || cfg.AirplaneEnabled {
		t.Fatalf("开网络应互斥关 vowifi/airplane: %+v", cfg)
	}
	if cfg.IPVersion != "v4v6" || cfg.APN != "ims" {
		t.Fatalf("ip/apn 应同步: %+v", cfg)
	}
}
