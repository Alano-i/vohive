package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iniwex5/vohive/internal/config"
)

func newPushplusTestChannel(t *testing.T, handler http.Handler) (*PushplusChannel, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	channel, err := NewPushplusChannel(config.PushplusConfig{Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	channel.endpoint = server.URL
	return channel, server.Close
}

func TestPushplusRejectsBusinessError(t *testing.T) {
	channel, closeServer := newPushplusTestChannel(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":500,"msg":"invalid token"}`))
	}))
	defer closeServer()

	err := channel.Send("hello")
	if err == nil || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("Send() error = %v, want pushplus business error", err)
	}
}

func TestPushplusRejectsOversizedResponse(t *testing.T) {
	channel, closeServer := newPushplusTestChannel(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxPushplusResponseBody+1)))
	}))
	defer closeServer()

	err := channel.Send("hello")
	if err == nil || !strings.Contains(err.Error(), "响应体过大") {
		t.Fatalf("Send() error = %v, want oversized response error", err)
	}
}
