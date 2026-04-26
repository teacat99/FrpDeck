package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/lifecycle"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/runtime"
)

// fakeUserRepo is the minimal UserRepo the WS handler exercises: it
// only needs to look up the principal behind a JWT, so the login-
// failure plumbing can be a no-op.
type fakeUserRepo struct {
	users map[uint]*model.User
	byUN  map[string]*model.User
}

func (f *fakeUserRepo) GetUserByUsername(name string) (*model.User, error) {
	return f.byUN[name], nil
}
func (f *fakeUserRepo) GetUserByID(id uint) (*model.User, error) { return f.users[id], nil }
func (f *fakeUserRepo) RecordLoginAttempt(*model.LoginAttempt) error { return nil }
func (f *fakeUserRepo) CountLoginFailuresByIP(string, time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeUserRepo) CountLoginFailuresByIPSubnet(string, time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeUserRepo) CountLoginFailuresByUsername(string, time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeUserRepo) LastSuccessfulLogin(string) (*model.LoginAttempt, error) {
	return nil, nil
}

// newWSTestServer assembles the smallest possible Server slice needed
// for the WS handshake: real auth + mock driver + a stubbed user repo.
// Any field the WS handler does not touch (lifecycle, store, captcha,
// notify) is left nil — the handler must not depend on them.
func newWSTestServer(t *testing.T) (*httptest.Server, string, *frpcd.Mock) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Listen:    ":0",
		AuthMode:  config.AuthModePassword,
		JWTSecret: "ws-test-secret",
	}
	rt := runtime.New(cfg)

	user := &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin}
	repo := &fakeUserRepo{
		users: map[uint]*model.User{1: user},
		byUN:  map[string]*model.User{"admin": user},
	}
	a := auth.New(cfg, rt, repo)

	driver := frpcd.NewMock()

	srv := &Server{
		cfg:       cfg,
		rt:        rt,
		store:     nil,
		lifecycle: (*lifecycle.Manager)(nil),
		driver:    driver,
		auth:      a,
	}
	engine := gin.New()
	srv.Router(engine)
	ts := httptest.NewServer(engine)
	t.Cleanup(ts.Close)

	token, err := auth.IssueTestToken(a, user)
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}
	return ts, token, driver
}

func dialWS(t *testing.T, ts *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"jwt", token},
	})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") })
	return conn
}

func readMessage(t *testing.T, conn *websocket.Conn) wsMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var m wsMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode ws msg %q: %v", string(data), err)
	}
	return m
}

func writeMessage(t *testing.T, conn *websocket.Conn, m wsMessage) {
	t.Helper()
	data, _ := json.Marshal(m)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func TestWebSocket_HelloAndSubscribeFiltersByTopic(t *testing.T) {
	ts, token, driver := newWSTestServer(t)
	conn := dialWS(t, ts, token)

	hello := readMessage(t, conn)
	if hello.Event != "hello" {
		t.Fatalf("expected hello, got %+v", hello)
	}

	writeMessage(t, conn, wsMessage{Op: "subscribe", Topics: []string{"tunnels"}})
	if ack := readMessage(t, conn); ack.Event != "ack" || ack.Op != "subscribe" {
		t.Fatalf("expected subscribe ack, got %+v", ack)
	}

	driver.Bus().Publish(frpcd.Event{Type: frpcd.EventEndpointState, EndpointID: 5, State: "connected"})
	driver.Bus().Publish(frpcd.Event{Type: frpcd.EventTunnelState, TunnelID: 9, State: "running"})

	got := readMessage(t, conn)
	if got.Event != string(frpcd.EventTunnelState) {
		t.Fatalf("expected tunnel_state, got %s", got.Event)
	}
	var ev frpcd.Event
	if err := json.Unmarshal(got.Data, &ev); err != nil {
		t.Fatalf("decode tunnel event: %v", err)
	}
	if ev.TunnelID != 9 || ev.State != "running" {
		t.Fatalf("unexpected event payload: %+v", ev)
	}
}

func TestWebSocket_RejectsUnauthenticated(t *testing.T) {
	ts, _, _ := newWSTestServer(t)
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"jwt", "not-a-real-jwt"},
	})
	if err == nil {
		t.Fatalf("expected dial to fail")
	}
	if resp == nil || resp.StatusCode != 401 {
		if resp != nil {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	}
}

func TestWebSocket_LogTopicScopedByEndpoint(t *testing.T) {
	ts, token, driver := newWSTestServer(t)
	conn := dialWS(t, ts, token)
	_ = readMessage(t, conn) // drain hello

	writeMessage(t, conn, wsMessage{Op: "subscribe", Topics: []string{"logs:endpoint:7"}})
	_ = readMessage(t, conn) // ack

	driver.Bus().Publish(frpcd.Event{Type: frpcd.EventLog, EndpointID: 1, Msg: "skip me"})
	driver.Bus().Publish(frpcd.Event{Type: frpcd.EventLog, EndpointID: 7, Msg: "deliver me"})

	got := readMessage(t, conn)
	if got.Event != string(frpcd.EventLog) {
		t.Fatalf("expected log, got %s", got.Event)
	}
	var ev frpcd.Event
	_ = json.Unmarshal(got.Data, &ev)
	if ev.EndpointID != 7 || ev.Msg != "deliver me" {
		t.Fatalf("wrong log delivered: %+v", ev)
	}
}
