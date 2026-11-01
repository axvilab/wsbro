package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/axvilab/wsbro/internal/channel"
	"github.com/axvilab/wsbro/internal/connection"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// sendQueueSize is how many outgoing messages a client may lag behind before it is dropped.
const sendQueueSize = 64

// session is the state of one client connection. Unexported fields without atomics are
// owned by the read loop; the rest may be read by the dashboard at any time.
type session struct {
	server  *Server
	app     *app
	conn    *connection.Connection
	limiter rateLimiter
	// subscribed maps channel name to the presence user ID ("" for other channels).
	subscribed map[string]string
	// userChannel is the internal "#server-to-user-<id>" channel after pusher:signin.
	userChannel string

	connectedAt  time.Time
	remoteAddr   string
	userAgent    string
	channelCount atomic.Int32
	userID       atomic.Pointer[string]
}

// handleWebSocket upgrades the request and runs the Pusher protocol for one client.
// See: https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	a := s.appByKey(chi.URLParam(r, "appKey"))

	opts := &websocket.AcceptOptions{InsecureSkipVerify: true}
	if a != nil {
		if patterns := s.Config.OriginPatterns(a.App); patterns != nil {
			opts = &websocket.AcceptOptions{OriginPatterns: patterns}
		}
	}

	ws, err := websocket.Accept(w, r, opts)
	if err != nil {
		log.Debug().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	// Rejections are sent over the socket: that is how Pusher clients learn whether to retry.
	if a == nil {
		reject(ws, errAppDisabled, "Application does not exist")
		return
	}
	ws.SetReadLimit(int64(s.Config.MaxMessageSize))
	sess := &session{
		server:      s,
		app:         a,
		conn:        connection.New(newSocketID(), a.ID, ws, sendQueueSize),
		limiter:     rateLimiter{limit: s.messageLimit(a)},
		subscribed:  make(map[string]string),
		connectedAt: time.Now(),
		remoteAddr:  clientIP(r),
		userAgent:   r.UserAgent(),
	}
	if !s.register(a, sess) {
		reject(ws, errOverCapacity, "Over capacity")
		log.Warn().Str("app", a.ID).Msg("Connection rejected: max connections reached")
		return
	}

	// The socket is hijacked, so the handler can return. Running the session in a fresh goroutine
	// frees the request, its headers and the grown handler stack, which saves about 40% of the
	// memory each idle client costs.
	go sess.run()
}

// register adds a connection to its app unless the global or per-app limit is reached.
// The global limit is checked without a lock, so it is a soft limit under heavy concurrency.
func (s *Server) register(a *app, sess *session) bool {
	if limit := s.Config.MaxConnections; limit > 0 && s.total.Load() >= int64(limit) {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if limit := a.MaxConnections; limit > 0 && len(a.connections) >= limit {
		return false
	}
	a.connections[sess.conn.ID] = sess
	s.total.Add(1)
	return true
}

// clientIP returns the client address, preferring headers set by a reverse proxy.
// It is only displayed in the dashboard, never trusted for security decisions.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-Ip"); ip != "" {
		return ip
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) messageLimit(a *app) int {
	if a.MaxMessagesPerSecond > 0 {
		return a.MaxMessagesPerSecond
	}
	return s.Config.MaxMessagesPerSecond
}

func reject(ws *websocket.Conn, code int, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = ws.Write(ctx, websocket.MessageText, encodeError(code, message))
	_ = ws.Close(websocket.StatusCode(code), message)
}

func (sess *session) run() {
	s, a, conn := sess.server, sess.app, sess.conn

	s.Metrics.connectionOpened(s.total.Load())

	go conn.WriteLoop(s.PingInterval, s.PongTimeout)
	defer sess.close()
	defer func() {
		// A bug triggered by one client must not take the whole server down.
		if err := recover(); err != nil {
			log.Error().Interface("panic", err).Str("socket_id", conn.ID).Msg("Session crashed")
		}
	}()

	log.Debug().Str("app", a.ID).Str("socket_id", conn.ID).Msg("Connection opened")
	s.inspector.emit(debugEvent{App: a.ID, Type: debugConnected, SocketID: conn.ID, Data: sess.remoteAddr + " " + sess.userAgent})
	conn.Send(encodeEvent(eventConnectionEstablished, "", map[string]any{
		"socket_id":        conn.ID,
		"activity_timeout": activityTimeoutSeconds,
	}))

	for {
		raw, err := conn.Read(context.Background())
		if err != nil {
			return
		}

		if !sess.limiter.allow(time.Now()) {
			sess.fail(errRateLimited, "Rate limit exceeded")
			continue
		}

		var msg clientMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			sess.fail(errInvalidEvent, "Invalid JSON")
			continue
		}
		sess.handle(msg)
	}
}

func (sess *session) handle(msg clientMessage) {
	switch {
	case msg.Event == eventPing:
		sess.conn.Send(encodeEvent(eventPong, "", struct{}{}))
	case msg.Event == eventSubscribe:
		sess.subscribe(msg.Data)
	case msg.Event == eventUnsubscribe:
		sess.unsubscribe(msg.Data)
	case msg.Event == eventSignin:
		sess.signin(msg.Data)
	case strings.HasPrefix(msg.Event, "client-"):
		sess.clientEvent(msg)
	}
}

func (sess *session) subscribe(raw json.RawMessage) {
	s, a, conn := sess.server, sess.app, sess.conn

	var data subscribeData
	if err := decodeData(raw, &data); err != nil || !isValidChannelName(data.Channel) {
		sess.fail(errInvalidChannel, "Invalid channel name")
		return
	}
	name := data.Channel

	_, alreadySubscribed := sess.subscribed[name]
	if limit := s.Config.MaxChannelsPerConn; !alreadySubscribed && limit > 0 && len(sess.subscribed) >= limit {
		sess.fail(errTooManyChannels, "Too many channels for this connection")
		return
	}

	if channel.IsPrivate(name) && !validChannelAuth(a.Key, a.Secret, conn.ID, data) {
		s.inspector.emit(debugEvent{App: a.ID, Type: debugAuthFailed, Channel: name, SocketID: conn.ID})
		conn.Send(encodeError(errUnauthorized, "Invalid signature for channel "+name))
		return
	}

	if !channel.IsPresence(name) {
		_, created, _ := a.subscribe(conn, name, nil)
		sess.markSubscribed(name, "")
		conn.Send(encodeEvent(eventSubscriptionSucceeded, name, struct{}{}))
		sess.sendCached(name)
		if created {
			a.webhooks.send(webhookEvent{Name: webhookChannelOccupied, Channel: name})
		}
		return
	}

	member, err := parseMember(data.ChannelData)
	if err != nil {
		sess.fail(errUnauthorized, err.Error())
		return
	}

	ch, created, firstForUser := a.subscribe(conn, name, &member)
	sess.markSubscribed(name, member.UserID)
	conn.Send(encodeEvent(eventSubscriptionSucceeded, name, presencePayload(ch.Members())))
	sess.sendCached(name)

	if created {
		a.webhooks.send(webhookEvent{Name: webhookChannelOccupied, Channel: name})
	}
	if firstForUser {
		s.broadcast(a, name, encodeEvent(eventMemberAdded, name, member), conn.ID)
		a.webhooks.send(webhookEvent{Name: webhookMemberAdded, Channel: name, UserID: member.UserID})
		s.inspector.emit(debugEvent{App: a.ID, Type: debugMemberAdded, Channel: name, SocketID: conn.ID, UserID: member.UserID, Data: string(member.UserInfo)})
	}
}

func (sess *session) markSubscribed(name, userID string) {
	if _, ok := sess.subscribed[name]; !ok {
		sess.channelCount.Add(1)
	}
	sess.subscribed[name] = userID
	sess.server.inspector.emit(debugEvent{App: sess.app.ID, Type: debugSubscribed, Channel: name, SocketID: sess.conn.ID, UserID: userID})
}

// fail sends a pusher:error to the client and shows it in the debug console.
func (sess *session) fail(code int, message string) {
	sess.conn.Send(encodeError(code, message))
	sess.server.inspector.emit(debugEvent{App: sess.app.ID, Type: debugError, SocketID: sess.conn.ID, Data: fmt.Sprintf("%d %s", code, message)})
}

func (sess *session) unsubscribe(raw json.RawMessage) {
	var data subscribeData
	if err := decodeData(raw, &data); err != nil {
		return
	}
	if _, ok := sess.subscribed[data.Channel]; !ok {
		return
	}
	sess.leave(data.Channel)
}

// sendCached replays the last event of a cache channel, or tells the client there is none.
// See: https://pusher.com/docs/channels/using_channels/cache-channels/
func (sess *session) sendCached(name string) {
	if !channel.IsCache(name) {
		return
	}
	if msg, ok := sess.app.cachedMessage(name); ok {
		sess.conn.Send(msg)
		return
	}
	sess.conn.Send(encodeEvent(eventCacheMiss, name, struct{}{}))
}

// signin authenticates the connection as a user, which enables sendToUser and terminate_connections.
// See: https://pusher.com/docs/channels/library_auth_reference/auth-signatures/#user-authentication
func (sess *session) signin(raw json.RawMessage) {
	a, conn := sess.app, sess.conn

	var data signinData
	if err := decodeData(raw, &data); err != nil || !validUserAuth(a.Key, a.Secret, conn.ID, data) {
		sess.fail(errUnauthorized, "Invalid user authentication signature")
		return
	}

	userID, err := parseUserID(data.UserData)
	if err != nil {
		sess.fail(errUnauthorized, err.Error())
		return
	}

	if sess.userChannel != "" {
		a.unsubscribe(conn.ID, sess.userChannel)
	}
	sess.userChannel = userChannelPrefix + userID
	a.subscribe(conn, sess.userChannel, nil)

	sess.userID.Store(&userID)
	conn.Send(encodeEvent(eventSigninSuccess, "", map[string]string{"user_data": data.UserData}))
	sess.server.inspector.emit(debugEvent{App: a.ID, Type: debugSignin, SocketID: conn.ID, UserID: userID, Data: data.UserData})
}

func (sess *session) leave(name string) {
	s, a, conn := sess.server, sess.app, sess.conn
	delete(sess.subscribed, name)
	sess.channelCount.Add(-1)
	s.inspector.emit(debugEvent{App: a.ID, Type: debugUnsubscribed, Channel: name, SocketID: conn.ID})

	_, member, userLeft, vacated := a.unsubscribe(conn.ID, name)
	if userLeft {
		s.broadcast(a, name, encodeEvent(eventMemberRemoved, name, map[string]string{"user_id": member.UserID}), conn.ID)
		a.webhooks.send(webhookEvent{Name: webhookMemberRemoved, Channel: name, UserID: member.UserID})
		s.inspector.emit(debugEvent{App: a.ID, Type: debugMemberLeft, Channel: name, SocketID: conn.ID, UserID: member.UserID})
	}
	if vacated {
		a.webhooks.send(webhookEvent{Name: webhookChannelVacated, Channel: name})
	}
}

// clientEvent forwards a client-* event to the other subscribers of a private or presence channel.
// See: https://pusher.com/docs/channels/using_channels/events/#triggering-client-events
func (sess *session) clientEvent(msg clientMessage) {
	userID, subscribed := sess.subscribed[msg.Channel]
	switch {
	case !channel.IsPrivate(msg.Channel):
		sess.fail(errInvalidEvent, "Client events are only allowed on private and presence channels")
		return
	case !subscribed:
		sess.fail(errInvalidEvent, "Not subscribed to channel "+msg.Channel)
		return
	case !isValidEventName(msg.Event):
		sess.fail(errInvalidEvent, "Invalid event name")
		return
	case len(msg.Data) > sess.server.Config.MaxEventDataSize:
		sess.fail(errInvalidEvent, "Event data too large")
		return
	}

	data := msg.Data
	if len(data) == 0 {
		data = json.RawMessage("{}")
	}
	out, _ := json.Marshal(outgoingMessage{
		Event:   msg.Event,
		Channel: msg.Channel,
		Data:    data,
		UserID:  userID,
	})
	sess.server.broadcast(sess.app, msg.Channel, out, sess.conn.ID)
	sess.server.inspector.emit(debugEvent{
		App: sess.app.ID, Type: debugClientEvent, Channel: msg.Channel, SocketID: sess.conn.ID,
		Event: msg.Event, UserID: userID, Data: string(data),
	})
}

func (sess *session) close() {
	s, a, conn := sess.server, sess.app, sess.conn

	for name := range sess.subscribed {
		sess.leave(name)
	}
	if sess.userChannel != "" {
		a.unsubscribe(conn.ID, sess.userChannel)
	}

	a.mu.Lock()
	delete(a.connections, conn.ID)
	a.mu.Unlock()
	s.total.Add(-1)

	conn.Close(websocket.StatusNormalClosure, "")
	log.Debug().Str("app", a.ID).Str("socket_id", conn.ID).Msg("Connection closed")
	s.inspector.emit(debugEvent{App: a.ID, Type: debugDisconnected, SocketID: conn.ID})
}

// parseUserID reads the mandatory "id" from pusher:signin user_data.
func parseUserID(userData string) (string, error) {
	var user struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal([]byte(userData), &user); err != nil {
		return "", fmt.Errorf("user_data must be a JSON object")
	}
	id, ok := jsonID(user.ID)
	if !ok {
		return "", fmt.Errorf("id is required in user_data")
	}
	return id, nil
}

// jsonID accepts an identifier encoded as a JSON string or number.
func jsonID(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, s != ""
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String(), true
	}
	return "", false
}

// parseMember reads presence channel_data. Laravel sends user_id as a string,
// other backends may send a number; both are accepted.
func parseMember(channelData string) (channel.Member, error) {
	if channelData == "" {
		return channel.Member{}, fmt.Errorf("channel_data is required for presence channels")
	}

	var raw struct {
		UserID   json.RawMessage `json:"user_id"`
		UserInfo json.RawMessage `json:"user_info"`
	}
	if err := json.Unmarshal([]byte(channelData), &raw); err != nil {
		return channel.Member{}, fmt.Errorf("channel_data must be a JSON object")
	}

	userID, ok := jsonID(raw.UserID)
	if !ok {
		return channel.Member{}, fmt.Errorf("user_id is required in channel_data")
	}

	return channel.Member{UserID: userID, UserInfo: raw.UserInfo}, nil
}

// presencePayload builds the member list sent in subscription_succeeded.
func presencePayload(members []channel.Member) map[string]any {
	ids := make([]string, 0, len(members))
	hash := make(map[string]json.RawMessage, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
		info := m.UserInfo
		if len(info) == 0 {
			info = json.RawMessage("null")
		}
		hash[m.UserID] = info
	}
	return map[string]any{
		"presence": map[string]any{
			"count": len(members),
			"ids":   ids,
			"hash":  hash,
		},
	}
}

// newSocketID returns a random Pusher socket ID in the "123.456" format.
func newSocketID() string {
	limit := big.NewInt(1_000_000_000)
	a, _ := rand.Int(rand.Reader, limit)
	b, _ := rand.Int(rand.Reader, limit)
	return a.String() + "." + b.String()
}

// rateLimiter allows up to limit messages per one-second window. A zero limit disables it.
type rateLimiter struct {
	limit       int
	count       int
	windowStart time.Time
}

func (l *rateLimiter) allow(now time.Time) bool {
	if l.limit <= 0 {
		return true
	}
	if now.Sub(l.windowStart) >= time.Second {
		l.windowStart = now
		l.count = 0
	}
	l.count++
	return l.count <= l.limit
}
