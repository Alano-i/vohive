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
)

type networkPatchControllerStub struct {
	connectErr error
	connected  bool
}

type networkIdentityBackendStub struct {
	ussdDeviceBackendStub
	iccid string
}

func (s *networkIdentityBackendStub) GetICCID(context.Context) (string, error) {
	return s.iccid, nil
}

func (s *networkIdentityBackendStub) GetICCIDLive(context.Context) (string, error) {
	return s.iccid, nil
}

func (s *networkIdentityBackendStub) GetIMSILive(context.Context) (string, error) {
	return "", nil
}

func (s *networkPatchControllerStub) Connect() error {
	if s.connectErr != nil {
		return s.connectErr
	}
	s.connected = true
	return nil
}

func (s *networkPatchControllerStub) Disconnect() error {
	s.connected = false
	return nil
}

func (s *networkPatchControllerStub) IsConnected() bool { return s.connected }
func (s *networkPatchControllerStub) RotateIP() error   { return nil }
func (s *networkPatchControllerStub) GetPrivateIP() string {
	return ""
}
func (s *networkPatchControllerStub) GetPrivateIPv6() string { return "" }
func (s *networkPatchControllerStub) GetPublicIPv4AndV6NoCache() (string, string) {
	return "", ""
}

func newNetworkPatchTestServer(t *testing.T, connectErr error) (*Server, *device.Worker, string) {
	t.Helper()
	openTestDB(t)

	iccid := "8986000000000000420"
	pool := device.NewPool(&config.Config{})
	worker := &device.Worker{
		ID: "qmi-network-test",
		Config: config.DeviceConfig{
			ID:            "qmi-network-test",
			DeviceBackend: backend.BackendQMI,
			IPVersion:     "v4",
		},
		Backend: &ussdDeviceBackendStub{mode: backend.BackendQMI},
	}
	setNestedPrivateField(t, worker, []string{"state", "Identity", "ICCID"}, iccid)
	setNestedPrivateField(t, worker, []string{"netOverride"}, device.NetworkController(&networkPatchControllerStub{connectErr: connectErr}))
	injectWorker(pool, worker)

	return &Server{pool: pool}, worker, iccid
}

func patchNetworkEnabled(t *testing.T, server *Server) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "device_id", Value: "qmi-network-test"}}
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/devices/qmi-network-test/network", strings.NewReader(`{"enabled":true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	server.handleDeviceNetworkPatch(ctx)
	return recorder
}

func TestNetworkPatchKeepsPolicyAndWaitsForQMIWhenCoreIsNotReady(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server, worker, iccid := newNetworkPatchTestServer(t, errors.New("manager core not started"))
	recoveryCalled := false
	server.networkRecovery = func(_ context.Context, got *device.Worker) error {
		recoveryCalled = got == worker
		return nil
	}

	recorder := patchNetworkEnabled(t, server)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if !recoveryCalled {
		t.Fatal("QMI recovery was not started")
	}
	policy, err := db.GetCardPolicy(iccid)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.NetworkEnabled || !worker.Config.NetworkEnabled {
		t.Fatalf("network intent was not preserved: policy=%+v worker=%+v", policy, worker.Config)
	}
}

func TestNetworkPatchRefreshesTemporarilyEmptyWorkerICCID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openTestDB(t)

	iccid := "8986000000000000421"
	pool := device.NewPool(&config.Config{})
	worker := &device.Worker{
		ID: "qmi-network-test",
		Config: config.DeviceConfig{
			ID:            "qmi-network-test",
			DeviceBackend: backend.BackendQMI,
			IPVersion:     "v4",
		},
		Backend: &networkIdentityBackendStub{
			ussdDeviceBackendStub: ussdDeviceBackendStub{mode: backend.BackendQMI},
			iccid:                 iccid,
		},
	}
	setNestedPrivateField(t, worker, []string{"netOverride"}, device.NetworkController(&networkPatchControllerStub{}))
	injectWorker(pool, worker)

	recorder := patchNetworkEnabled(t, &Server{pool: pool})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := worker.CurrentICCID(); got != iccid {
		t.Fatalf("CurrentICCID=%q want=%q", got, iccid)
	}
	policy, err := db.GetCardPolicy(iccid)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.NetworkEnabled {
		t.Fatalf("network policy was not enabled: %+v", policy)
	}
}

func TestBeginNetworkControlRecoveryDoesNotRebootRegisteredModem(t *testing.T) {
	pool := device.NewPool(&config.Config{})
	backendStub := &ussdDeviceBackendStub{mode: backend.BackendQMI}
	worker := &device.Worker{ID: "qmi-wait", Backend: backendStub}
	server := &Server{pool: pool}

	if err := server.beginNetworkControlRecovery(context.Background(), worker); err != nil {
		t.Fatal(err)
	}
	if backendStub.rebooted {
		t.Fatal("network enable must not reboot a modem while QMI bootstrap is converging")
	}
	if phase := pool.LifecycleSnapshot(worker.ID).Phase; phase != device.LifecyclePhaseQMIStarting {
		t.Fatalf("lifecycle=%q want qmi_starting", phase)
	}
}

func TestNetworkPatchRollsBackPolicyOnNonRecoverableStartFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server, worker, iccid := newNetworkPatchTestServer(t, errors.New("packet service rejected"))

	recorder := patchNetworkEnabled(t, server)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	policy, err := db.GetCardPolicy(iccid)
	if err != nil {
		t.Fatal(err)
	}
	if policy.NetworkEnabled || worker.Config.NetworkEnabled {
		t.Fatalf("failed start left network enabled: policy=%+v worker=%+v", policy, worker.Config)
	}
}

func TestRecoverableQMINetworkStartErrorDoesNotMatchATBackend(t *testing.T) {
	worker := &device.Worker{Backend: &ussdDeviceBackendStub{mode: backend.BackendAT}}
	if isRecoverableQMINetworkStartError(worker, errors.New("manager core not started")) {
		t.Fatal("AT backend error must not enter QMI modem recovery")
	}
}

func TestRecoverableQMINetworkStartErrorMatchesStaleDataSession(t *testing.T) {
	worker := &device.Worker{Backend: &ussdDeviceBackendStub{mode: backend.BackendQMI}}
	err := errors.New("start network failed: QMI error: service=0x01 msg=0x0020 result=0x0001 error=0x001a")
	if !isRecoverableQMINetworkStartError(worker, err) {
		t.Fatal("QMI no-effect/stale data session must enter modem recovery")
	}
}
