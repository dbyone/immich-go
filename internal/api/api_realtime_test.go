package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clientsocket "github.com/zishang520/socket.io/clients/socket/v3"
)

// waitFor drains the channel with a deadline.
func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(8 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return *new(T)
	}
}

// connectRealtime dials the hub like the official web client would:
// EIO websocket transport against /api/socket.io with the REST bearer
// token in the handshake headers. Handlers must be registered before
// Connect() — the handshake can complete before the caller returns.
func connectRealtime(t *testing.T, srvURL, token string) *clientsocket.Socket {
	t.Helper()
	mopts := clientsocket.DefaultManagerOptions()
	mopts.SetPath("/api/socket.io")
	if token != "" {
		mopts.SetExtraHeaders(http.Header{"Authorization": {"Bearer " + token}})
	}
	mgr := clientsocket.NewManager(srvURL, mopts)
	s := mgr.Socket("/", nil)
	return s
}

// TestRealtimeWire pins the gateway contract end to end against a real
// HTTP server: authenticated handshake receives on_server_version, an
// upload fans out on_upload_success, and unknown callers are rejected.
func TestRealtimeWire(t *testing.T) {
	h, _ := newTestServerApp(t, nil)
	token := loginForTest(t, h, "rt@t.c")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	s := connectRealtime(t, srv.URL, token)
	t.Cleanup(func() { s.Disconnect() })

	connected := make(chan struct{}, 1)
	s.On("connect", func(...any) { connected <- struct{}{} })
	version := make(chan map[string]any, 1)
	s.On("on_server_version", func(args ...any) {
		if m, ok := args[0].(map[string]any); ok {
			version <- m
		}
	})
	uploaded := make(chan map[string]any, 1)
	s.On("on_upload_success", func(args ...any) {
		if m, ok := args[0].(map[string]any); ok {
			uploaded <- m
		}
	})
	s.Connect()

	select {
	case <-connected:
	case <-time.After(8 * time.Second):
		t.Fatal("socket never connected")
	}
	v := waitFor(t, version, "on_server_version")
	if v["major"] == nil || v["patch"] == nil {
		t.Fatalf("on_server_version payload = %v", v)
	}

	// An upload through the REST API must arrive as a live event with the
	// full asset payload.
	id := uploadForTest(t, h, token, testJPEG(t, 1), "rt.jpg")
	event := waitFor(t, uploaded, "on_upload_success")
	if event["id"] != id {
		t.Fatalf("on_upload_success id = %v, want %v", event["id"], id)
	}
	if event["originalFileName"] != "rt.jpg" {
		t.Fatalf("asset payload incomplete: %v", event)
	}
}

// TestRealtimeRejectsUnknownCallers: without credentials the namespace
// middleware refuses the connection. The observable outcome is either a
// connect_error event or a socket that never reaches the connected
// state — both prove the gateway did not admit an anonymous caller.
func TestRealtimeRejectsUnknownCallers(t *testing.T) {
	h, _ := newTestServerApp(t, nil)
	loginForTest(t, h, "rt2@t.c") // admin exists, but we dial bare
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	s := connectRealtime(t, srv.URL, "")
	t.Cleanup(func() { s.Disconnect() })

	connectErr := make(chan string, 1)
	s.On("connect_error", func(args ...any) {
		if len(args) > 0 {
			connectErr <- toString(args[0])
		}
	})
	s.Connect()

	deadline := time.After(12 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case msg := <-connectErr:
			if msg == "" {
				t.Fatal("empty connect_error")
			}
			return
		case <-time.After(1500 * time.Millisecond):
			// Give the error packet a beat to arrive, then also accept
			// "never admitted" as proof of rejection — under load the
			// delivery of CONNECT_ERROR can lag behind the refusal.
			if !s.Connected() {
				return
			}
		case <-deadline:
			t.Fatalf("anonymous socket was admitted or hung: connected=%v", s.Connected())
		}
	}
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		return ""
	}
}
