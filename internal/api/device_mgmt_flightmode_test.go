package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/device"
	"github.com/iniwex5/vohive/internal/modem"
)

func TestFlightModeSuccessMessageUsesRequestedState(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    string
	}{
		{name: "enable", enabled: true, want: "飞行模式已开启"},
		{name: "disable", enabled: false, want: "飞行模式已关闭"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flightModeSuccessMessage(tt.enabled); got != tt.want {
				t.Fatalf("flightModeSuccessMessage(%v)=%q want %q", tt.enabled, got, tt.want)
			}
		})
	}
}

func TestFlightModeControlFailureKeepsSavedIntentAndStartsRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openTestDB(t)

	iccid := "8986000000000000999"
	pool := device.NewPool(&config.Config{})
	worker := &device.Worker{
		ID:      "esim-flight-test",
		Config:  config.DeviceConfig{ID: "esim-flight-test", DeviceBackend: backend.BackendAT},
		Backend: &ussdDeviceBackendStub{mode: backend.BackendAT, setModeErr: errors.New("Port has been closed")},
	}
	setNestedPrivateField(t, worker, []string{"state", "Identity", "ICCID"}, iccid)
	injectWorker(pool, worker)

	recoveryCalled := false
	server := &Server{pool: pool, controlRecovery: func(got *device.Worker, reason string) {
		recoveryCalled = got == worker && reason == "flight_mode_control_recovery"
	}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "device_id", Value: worker.ID}}
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/devices/esim-flight-test/flight-mode", strings.NewReader(`{"enabled":true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	server.handleDeviceMgmtSetFlightMode(ctx)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if !recoveryCalled {
		t.Fatal("control recovery was not scheduled")
	}
	policy, err := db.ResolveCardPolicy(iccid)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.AirplaneEnabled || policy.NetworkEnabled || policy.VoWiFiEnabled {
		t.Fatalf("flight intent was not preserved: %+v", policy)
	}
	if !worker.Config.AirplaneEnabled || worker.Config.NetworkEnabled || worker.Config.VoWiFiEnabled {
		t.Fatalf("worker projection was not preserved: %+v", worker.Config)
	}
}

func TestSetWorkerFlightModeFailsWhenBackendMissingEvenWithATModem(t *testing.T) {
	m, err := modem.New(config.DeviceConfig{
		ID:            "dev-qmi",
		DeviceBackend: "qmi",
		ATPort:        "/dev/ttyUSB-test",
	})
	if err != nil {
		t.Fatalf("modem.New() error = %v", err)
	}

	_, _, err = setWorkerFlightMode(context.Background(), &device.Worker{Modem: m}, false)
	if err == nil {
		t.Fatal("setWorkerFlightMode() error = nil, want backend initialization error")
	}
	if !strings.Contains(err.Error(), "设备后端未初始化") {
		t.Fatalf("setWorkerFlightMode() error = %q, want backend initialization error", err.Error())
	}
	if strings.Contains(err.Error(), "AT 管理器") || strings.Contains(err.Error(), "AT 端口") {
		t.Fatalf("setWorkerFlightMode() error = %q, must not come from legacy AT fallback", err.Error())
	}
}
