package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/device"
)

func TestHandleDeviceMgmtListHasNoDeviceQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := writeDeviceMgmtTestConfig(t, `
server:
  port: ":7575"
devices:
  - id: dev-1
  - id: dev-2
  - id: dev-3
  - id: dev-4
  - id: dev-5
  - id: dev-6
`)
	if err := config.InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/devices", nil)
	server := &Server{pool: device.NewPool(&config.Config{}), configPath: path}

	server.handleDeviceMgmtList(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal() error = %v body=%s", err, rec.Body.String())
	}
	if _, exists := body["device_limit"]; exists {
		t.Fatalf("response still contains device_limit: %s", rec.Body.String())
	}
	var devices []deviceMgmtListItem
	if err := json.Unmarshal(body["devices"], &devices); err != nil {
		t.Fatalf("Unmarshal(devices) error = %v", err)
	}
	if len(devices) != 6 {
		t.Fatalf("devices len=%d want=6 body=%s", len(devices), rec.Body.String())
	}
}
