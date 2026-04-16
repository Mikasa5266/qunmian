package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const userContextKey contextKey = "auth_user"

var defaultQuestions = []string{
	"请每位同学在 60 秒内介绍一个你主导完成、且最有成就感的项目。",
	"团队意见分歧时，你如何推动结论落地？请给出真实案例。",
	"假设今天你负责这个产品上线前最后一天，你会如何排优先级？",
	"如果你只能保留简历中的一段经历，会选哪一段，为什么？",
}

type Server struct {
	mu          sync.RWMutex
	users       map[string]*User
	usersByName map[string]*User
	rooms       map[string]*Room
	invites     map[string]string
	jwtSecret   []byte
}

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
}

type Room struct {
	ID              string
	Name            string
	OwnerID         string
	InviteCode      string
	MaxParticipants int
	Started         bool
	Question        string
	Members         map[string]*RoomMember
	Clients         map[string]*Client
	UpdatedAt       time.Time
}

type RoomMember struct {
	UserID   string    `json:"userId"`
	Username string    `json:"username"`
	Muted    bool      `json:"muted"`
	JoinedAt time.Time `json:"joinedAt"`
}

type Client struct {
	server   *Server
	conn     *websocket.Conn
	send     chan []byte
	roomID   string
	userID   string
	username string
}

type AuthClaims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

type ParticipantView struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Muted    bool   `json:"muted"`
	Online   bool   `json:"online"`
}

type RoomState struct {
	RoomID          string            `json:"roomId"`
	Name            string            `json:"name"`
	InviteCode      string            `json:"inviteCode"`
	Started         bool              `json:"started"`
	Question        string            `json:"question"`
	MaxParticipants int               `json:"maxParticipants"`
	MinRequired     int               `json:"minRequired"`
	Participants    []ParticipantView `json:"participants"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func main() {
	secret := os.Getenv("JWT_SECRET")
	if strings.TrimSpace(secret) == "" {
		secret = "change-this-jwt-secret-in-production"
	}
	port := os.Getenv("APP_PORT")
	if strings.TrimSpace(port) == "" {
		port = "8080"
	}

	s := &Server{
		users:       make(map[string]*User),
		usersByName: make(map[string]*User),
		rooms:       make(map[string]*Room),
		invites:     make(map[string]string),
		jwtSecret:   []byte(secret),
	}

	addr := ":" + port
	log.Printf("server listening on %s", addr)
	if err := http.ListenAndServe(addr, s.routes()); err != nil {
		log.Fatal(err)
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/register", s.handleRegister)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.Handle("/api/auth/me", s.authMiddleware(http.HandlerFunc(s.handleMe)))
	mux.Handle("/api/invites", s.authMiddleware(http.HandlerFunc(s.handleCreateInvite)))
	mux.Handle("/api/invites/", s.authMiddleware(http.HandlerFunc(s.handleInviteByCode)))
	mux.Handle("/api/rooms/mine", s.authMiddleware(http.HandlerFunc(s.handleMyRooms)))
	mux.Handle("/api/rooms/", s.authMiddleware(http.HandlerFunc(s.handleRoomActions)))
	mux.Handle("/api/config/webrtc", s.authMiddleware(http.HandlerFunc(s.handleWebRTCConfig)))
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", s.spaHandler("./static"))
	return s.withCORS(s.withLogging(mux))
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) spaHandler(staticDir string) http.Handler {
	fs := http.FileServer(http.Dir(staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws") {
			http.NotFound(w, r)
			return
		}

		target := filepath.Join(staticDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "用户名至少 3 位，密码至少 6 位")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.usersByName[strings.ToLower(req.Username)]; ok {
		writeError(w, http.StatusConflict, "用户名已存在")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建账号")
		return
	}
	user := &User{
		ID:           newID(),
		Username:     req.Username,
		PasswordHash: string(hash),
	}
	s.users[user.ID] = user
	s.usersByName[strings.ToLower(user.Username)] = user

	token, err := s.issueToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建登录态")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.RLock()
	user, ok := s.usersByName[strings.ToLower(strings.TrimSpace(req.Username))]
	s.mu.RUnlock()
	if !ok || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token, err := s.issueToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建登录态")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := userFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := userFromContext(r.Context())
	var req struct {
		RoomName string `json:"roomName"`
	}
	_ = decodeJSON(r, &req)
	name := strings.TrimSpace(req.RoomName)
	if name == "" {
		name = "群面房间"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	roomID := newID()
	inviteCode := newInviteCode(10)
	room := &Room{
		ID:              roomID,
		Name:            name,
		OwnerID:         user.ID,
		InviteCode:      inviteCode,
		MaxParticipants: 5,
		Members:         make(map[string]*RoomMember),
		Clients:         make(map[string]*Client),
		UpdatedAt:       time.Now(),
	}
	room.Members[user.ID] = &RoomMember{UserID: user.ID, Username: user.Username, JoinedAt: time.Now()}
	s.rooms[roomID] = room
	s.invites[inviteCode] = roomID

	base := publicBaseURL(r)
	inviteLink := fmt.Sprintf("%s/hub?invite=%s", base, inviteCode)
	writeJSON(w, http.StatusCreated, map[string]any{
		"roomId":     room.ID,
		"roomName":   room.Name,
		"inviteCode": inviteCode,
		"inviteLink": inviteLink,
	})
}

func (s *Server) handleInviteByCode(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/invites/"), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "invite not found")
		return
	}
	if strings.HasSuffix(rest, "accept") {
		code := strings.TrimSuffix(rest, "accept")
		code = strings.Trim(code, "/")
		s.handleAcceptInvite(w, r, code)
		return
	}
	s.handleGetInvite(w, r, rest)
}

func (s *Server) handleGetInvite(w http.ResponseWriter, r *http.Request, code string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.RLock()
	roomID, ok := s.invites[code]
	if !ok {
		s.mu.RUnlock()
		writeError(w, http.StatusNotFound, "邀请码不存在")
		return
	}
	room := s.rooms[roomID]
	state := s.roomStateLocked(room)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"inviteCode": code, "room": state})
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request, code string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := userFromContext(r.Context())
	s.mu.Lock()
	roomID, ok := s.invites[code]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "邀请码不存在")
		return
	}
	room := s.rooms[roomID]
	if _, exists := room.Members[user.ID]; !exists && len(room.Members) >= room.MaxParticipants {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "房间人数已满")
		return
	}
	if _, exists := room.Members[user.ID]; !exists {
		room.Members[user.ID] = &RoomMember{UserID: user.ID, Username: user.Username, JoinedAt: time.Now()}
		room.UpdatedAt = time.Now()
	}
	state := s.roomStateLocked(room)
	s.mu.Unlock()

	s.broadcastRoomState(roomID)
	s.broadcastSystem(roomID, fmt.Sprintf("%s 加入了群面", user.Username))
	writeJSON(w, http.StatusOK, map[string]any{"room": state})
}

func (s *Server) handleMyRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := userFromContext(r.Context())
	s.mu.RLock()
	rooms := make([]RoomState, 0)
	for _, room := range s.rooms {
		if _, ok := room.Members[user.ID]; ok {
			rooms = append(rooms, s.roomStateLocked(room))
		}
	}
	s.mu.RUnlock()
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].UpdatedAt.After(rooms[j].UpdatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

func (s *Server) handleRoomActions(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/rooms/"), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	parts := strings.Split(rest, "/")
	roomID := parts[0]
	action := "state"
	if len(parts) > 1 {
		action = strings.Join(parts[1:], "/")
	}

	switch action {
	case "state":
		s.handleRoomState(w, r, roomID)
	case "start":
		s.handleStartRoom(w, r, roomID)
	case "end":
		s.handleEndRoom(w, r, roomID)
	case "question/next":
		s.handleNextQuestion(w, r, roomID)
	default:
		writeError(w, http.StatusNotFound, "unknown room action")
	}
}

func (s *Server) handleRoomState(w http.ResponseWriter, r *http.Request, roomID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := userFromContext(r.Context())
	s.mu.RLock()
	room, ok := s.rooms[roomID]
	if !ok {
		s.mu.RUnlock()
		writeError(w, http.StatusNotFound, "房间不存在")
		return
	}
	if _, ok := room.Members[user.ID]; !ok {
		s.mu.RUnlock()
		writeError(w, http.StatusForbidden, "你不在这个房间")
		return
	}
	state := s.roomStateLocked(room)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"room": state})
}

func (s *Server) handleStartRoom(w http.ResponseWriter, r *http.Request, roomID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := userFromContext(r.Context())
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "房间不存在")
		return
	}
	if _, ok := room.Members[user.ID]; !ok {
		s.mu.Unlock()
		writeError(w, http.StatusForbidden, "你不在这个房间")
		return
	}
	if len(room.Members) < 3 {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "至少 3 名面试者才能开始")
		return
	}
	if !room.Started {
		room.Started = true
		room.Question = randomQuestion()
		room.UpdatedAt = time.Now()
	}
	state := s.roomStateLocked(room)
	s.mu.Unlock()

	s.broadcastRoomState(roomID)
	s.broadcastSystem(roomID, "群面已开始，请围绕题目依次发言")
	writeJSON(w, http.StatusOK, map[string]any{"room": state})
}

func (s *Server) handleEndRoom(w http.ResponseWriter, r *http.Request, roomID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := userFromContext(r.Context())
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "房间不存在")
		return
	}
	if _, ok := room.Members[user.ID]; !ok {
		s.mu.Unlock()
		writeError(w, http.StatusForbidden, "你不在这个房间")
		return
	}
	room.Started = false
	room.Question = ""
	room.UpdatedAt = time.Now()
	state := s.roomStateLocked(room)
	s.mu.Unlock()

	s.broadcastRoomState(roomID)
	s.broadcastSystem(roomID, fmt.Sprintf("%s 结束了本场群面", user.Username))
	writeJSON(w, http.StatusOK, map[string]any{"room": state})
}

func (s *Server) handleNextQuestion(w http.ResponseWriter, r *http.Request, roomID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := userFromContext(r.Context())
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "房间不存在")
		return
	}
	if _, ok := room.Members[user.ID]; !ok {
		s.mu.Unlock()
		writeError(w, http.StatusForbidden, "你不在这个房间")
		return
	}
	if !room.Started {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "群面尚未开始")
		return
	}
	room.Question = randomQuestion()
	room.UpdatedAt = time.Now()
	state := s.roomStateLocked(room)
	s.mu.Unlock()

	s.broadcastRoomState(roomID)
	s.broadcastSystem(roomID, fmt.Sprintf("%s 切换了新题目", user.Username))
	writeJSON(w, http.StatusOK, map[string]any{"room": state})
}

func (s *Server) handleWebRTCConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	stuns := strings.Split(strings.TrimSpace(os.Getenv("WEBRTC_STUN")), ",")
	cleanStuns := make([]string, 0)
	for _, stun := range stuns {
		stun = strings.TrimSpace(stun)
		if stun != "" {
			cleanStuns = append(cleanStuns, stun)
		}
	}
	if len(cleanStuns) == 0 {
		cleanStuns = []string{"stun:stun.l.google.com:19302"}
	}
	turnURL := strings.TrimSpace(os.Getenv("WEBRTC_TURN_URL"))
	turnUser := strings.TrimSpace(os.Getenv("WEBRTC_TURN_USERNAME"))
	turnCred := strings.TrimSpace(os.Getenv("WEBRTC_TURN_CREDENTIAL"))

	iceServers := []map[string]any{{"urls": cleanStuns}}
	if turnURL != "" && turnUser != "" && turnCred != "" {
		iceServers = append(iceServers, map[string]any{
			"urls":       []string{turnURL},
			"username":   turnUser,
			"credential": turnCred,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"iceServers": iceServers})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	roomID := strings.TrimSpace(r.URL.Query().Get("roomId"))
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if roomID == "" || token == "" {
		writeError(w, http.StatusBadRequest, "roomId and token are required")
		return
	}
	user, err := s.userFromToken(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	s.mu.RLock()
	room, ok := s.rooms[roomID]
	if !ok {
		s.mu.RUnlock()
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if _, ok := room.Members[user.ID]; !ok {
		s.mu.RUnlock()
		writeError(w, http.StatusForbidden, "user not in room")
		return
	}
	s.mu.RUnlock()

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(_ *http.Request) bool {
			return true
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &Client{
		server:   s,
		conn:     conn,
		send:     make(chan []byte, 128),
		roomID:   roomID,
		userID:   user.ID,
		username: user.Username,
	}

	s.registerClient(client)
	go client.writePump()
	client.readPump()
}

func (s *Server) registerClient(c *Client) {
	s.mu.Lock()
	room := s.rooms[c.roomID]
	room.Clients[c.userID] = c
	room.UpdatedAt = time.Now()
	state := s.roomStateLocked(room)
	s.mu.Unlock()

	c.sendJSON("room_state", state)
	s.broadcastRoomState(c.roomID)
	s.broadcastSystem(c.roomID, fmt.Sprintf("%s 已进入语音视频房间", c.username))
}

func (s *Server) unregisterClient(c *Client) {
	s.mu.Lock()
	room, ok := s.rooms[c.roomID]
	if ok {
		if existing, exists := room.Clients[c.userID]; exists && existing == c {
			delete(room.Clients, c.userID)
			close(c.send)
			room.UpdatedAt = time.Now()
		}
	}
	s.mu.Unlock()

	if ok {
		s.broadcastRoomState(c.roomID)
		s.broadcastSystem(c.roomID, fmt.Sprintf("%s 已离开语音视频房间", c.username))
	}
}

func (c *Client) readPump() {
	defer func() {
		c.server.unregisterClient(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(1024 * 1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(_ string) error {
		return c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	for {
		_, payload, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var msg WSMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		c.server.handleClientMessage(c, msg)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(25 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleClientMessage(c *Client, msg WSMessage) {
	switch msg.Type {
	case "signal":
		var payload struct {
			To   string          `json:"to"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.To == "" {
			return
		}
		s.mu.RLock()
		room := s.rooms[c.roomID]
		target := room.Clients[payload.To]
		s.mu.RUnlock()
		if target == nil {
			return
		}
		target.sendJSON("signal", map[string]any{
			"from": c.userID,
			"data": json.RawMessage(payload.Data),
		})
	case "chat":
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return
		}
		payload.Text = strings.TrimSpace(payload.Text)
		if payload.Text == "" {
			return
		}
		s.broadcast(c.roomID, "chat", map[string]any{
			"from":      c.userID,
			"username":  c.username,
			"text":      payload.Text,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	case "mute":
		var payload struct {
			Muted bool `json:"muted"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return
		}
		s.mu.Lock()
		room := s.rooms[c.roomID]
		if member, ok := room.Members[c.userID]; ok {
			member.Muted = payload.Muted
			room.UpdatedAt = time.Now()
		}
		s.mu.Unlock()
		s.broadcastRoomState(c.roomID)
	}
}

func (c *Client) sendJSON(typ string, payload any) {
	data, err := json.Marshal(map[string]any{"type": typ, "payload": payload})
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
		// Drop message for slow clients to avoid blocking the room.
	}
}

func (s *Server) broadcast(roomID, typ string, payload any) {
	s.mu.RLock()
	room, ok := s.rooms[roomID]
	if !ok {
		s.mu.RUnlock()
		return
	}
	clients := make([]*Client, 0, len(room.Clients))
	for _, c := range room.Clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()
	for _, c := range clients {
		c.sendJSON(typ, payload)
	}
}

func (s *Server) broadcastRoomState(roomID string) {
	s.mu.RLock()
	room, ok := s.rooms[roomID]
	if !ok {
		s.mu.RUnlock()
		return
	}
	state := s.roomStateLocked(room)
	clients := make([]*Client, 0, len(room.Clients))
	for _, c := range room.Clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()
	for _, c := range clients {
		c.sendJSON("room_state", state)
	}
}

func (s *Server) broadcastSystem(roomID, text string) {
	s.broadcast(roomID, "system", map[string]any{
		"text":      text,
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func (s *Server) roomStateLocked(room *Room) RoomState {
	participants := make([]ParticipantView, 0, len(room.Members))
	for _, m := range room.Members {
		_, online := room.Clients[m.UserID]
		participants = append(participants, ParticipantView{
			UserID:   m.UserID,
			Username: m.Username,
			Muted:    m.Muted,
			Online:   online,
		})
	}
	sort.Slice(participants, func(i, j int) bool {
		if participants[i].Online == participants[j].Online {
			return participants[i].Username < participants[j].Username
		}
		return participants[i].Online
	})
	return RoomState{
		RoomID:          room.ID,
		Name:            room.Name,
		InviteCode:      room.InviteCode,
		Started:         room.Started,
		Question:        room.Question,
		MaxParticipants: room.MaxParticipants,
		MinRequired:     3,
		Participants:    participants,
		UpdatedAt:       room.UpdatedAt,
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := tokenFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "missing token")
			return
		}
		user, err := s.userFromToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) userFromToken(tokenString string) (*User, error) {
	claims := &AuthClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(_ *jwt.Token) (any, error) {
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[claims.UserID]
	if !ok {
		return nil, errors.New("user not found")
	}
	return user, nil
}

func (s *Server) issueToken(userID string) (string, error) {
	claims := AuthClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(72 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func tokenFromRequest(r *http.Request) (string, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", errors.New("no auth header")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("invalid auth header")
	}
	return strings.TrimSpace(parts[1]), nil
}

func userFromContext(ctx context.Context) *User {
	if user, ok := ctx.Value(userContextKey).(*User); ok {
		return user
	}
	return nil
}

func decodeJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, http.ErrBodyNotAllowed) || errors.Is(err, context.Canceled) {
			return errors.New("invalid request body")
		}
		if strings.Contains(err.Error(), "EOF") {
			return nil
		}
		return errors.New("请求体格式错误")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func newID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func newInviteCode(length int) string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var b strings.Builder
	for i := 0; i < length; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b.WriteByte(chars[n.Int64()])
	}
	return b.String()
}

func randomQuestion() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(defaultQuestions))))
	return defaultQuestions[n.Int64()]
}

func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); strings.TrimSpace(forwardedProto) != "" {
		scheme = strings.TrimSpace(forwardedProto)
	}
	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); strings.TrimSpace(forwardedHost) != "" {
		host = strings.TrimSpace(forwardedHost)
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}
