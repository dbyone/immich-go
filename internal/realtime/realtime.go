// Package realtime hosts the Socket.IO gateway (/api/socket.io) so the
// official web and mobile clients receive live updates — timeline
// additions, asset edits, trash/restore — mirroring the upstream
// WebsocketRepository: connections authenticate from the handshake
// request headers through the same chain as REST, sockets join their
// owner's and session's rooms, and domain events fan out per room.
//
// Backed by github.com/zishang520/socket.io (Socket.IO v4+ wire
// protocol, matching the official socket.io-client 4.x).
package realtime

import (
	"log/slog"
	"net/http"

	socket "github.com/zishang520/socket.io/servers/socket/v3"
)

// Credentials are the rooms a connection joins after authentication.
type Credentials struct {
	UserID    string
	SessionID string
}

// Authenticate resolves credentials from the handshake request (the
// same AcceptToken/cookie/API-key chain the REST guard uses).
type Authenticate func(r *http.Request) (Credentials, bool)

// Hub is the Socket.IO server plus immich event semantics.
type Hub struct {
	io      *socket.Server
	auth    Authenticate
	version any // payload for on_server_version
	log     *slog.Logger
}

// New builds the hub. version is emitted to clients as on_server_version
// right after a successful connection (the web refreshes its
// serverVersion store from it).
func New(auth Authenticate, version any, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	h := &Hub{
		io:      socket.NewServer(nil, nil),
		auth:    auth,
		version: version,
		log:     log,
	}
	h.io.Use(h.middleware)
	h.io.On("connection", h.onConnection)
	return h
}

// Handler serves the Engine.IO/Socket.IO endpoint.
func (h *Hub) Handler() http.Handler {
	return h.io.ServeHandler(nil)
}

// Close shuts the server down.
func (h *Hub) Close() {
	h.io.Close(func(error) {})
}

// middleware authenticates the handshake request and joins the user and
// session rooms; unknown callers are refused through the middleware
// error channel, which the library turns into a CONNECT_ERROR packet
// (the client surfaces it as connect_error). Disconnecting here first
// would drop the socket before the error packet can be delivered.
func (h *Hub) middleware(s *socket.Socket, next func(*socket.ExtendedError)) {
	req := s.Request().Request()
	creds, ok := h.auth(req)
	if !ok {
		next(socket.NewExtendedError("unauthorized", nil))
		return
	}
	s.Join(socket.Room(creds.UserID))
	if creds.SessionID != "" {
		s.Join(socket.Room(creds.SessionID))
	}
	h.log.Debug("websocket connect", "user", creds.UserID, "session", creds.SessionID)
	next(nil)
}

// onConnection fires per socket after middleware; the only client-facing
// immediate event is the server version.
func (h *Hub) onConnection(sockets ...any) {
	s, ok := sockets[0].(*socket.Socket)
	if !ok {
		return
	}
	if h.version != nil {
		_ = s.Emit("on_server_version", h.version)
	}
	s.On("disconnect", func(reason ...any) {
		h.log.Debug("websocket disconnect", "reason", reason)
	})
}

// BroadcastToUser emits to every socket in the owner's room.
func (h *Hub) BroadcastToUser(userID, event string, payload any) {
	h.io.To(socket.Room(userID)).Emit(event, payload)
}

// BroadcastAll emits to every connected socket.
func (h *Hub) BroadcastAll(event string, payload any) {
	h.io.Emit(event, payload)
}
