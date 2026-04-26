package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/model"
)

func init() { gin.SetMode(gin.TestMode) }

func mustCIDR(s string) *net.IPNet {
	_, n, _ := net.ParseCIDR(s)
	return n
}

// fakeUserRepo is an in-memory UserRepo used by the tests; it keeps the
// test surface free of SQLite. Index by id and by username, and keeps a
// tiny slice of LoginAttempt rows so we can exercise the brute-force
// limiter without booting the database.
type fakeUserRepo struct {
	byID     map[uint]*model.User
	byName   map[string]*model.User
	attempts []model.LoginAttempt
}

func newFakeRepo(us ...*model.User) *fakeUserRepo {
	r := &fakeUserRepo{byID: map[uint]*model.User{}, byName: map[string]*model.User{}}
	for _, u := range us {
		r.byID[u.ID] = u
		r.byName[u.Username] = u
	}
	return r
}

func (r *fakeUserRepo) GetUserByUsername(name string) (*model.User, error) {
	return r.byName[name], nil
}

func (r *fakeUserRepo) GetUserByID(id uint) (*model.User, error) {
	return r.byID[id], nil
}

func (r *fakeUserRepo) RecordLoginAttempt(a *model.LoginAttempt) error {
	a.CreatedAt = time.Now()
	r.attempts = append(r.attempts, *a)
	return nil
}

func (r *fakeUserRepo) CountLoginFailuresByIP(ip string, since time.Time) (int64, error) {
	var n int64
	for _, a := range r.attempts {
		if !a.Success && a.ClientIP == ip && !a.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (r *fakeUserRepo) CountLoginFailuresByIPSubnet(prefix string, since time.Time) (int64, error) {
	parts := strings.SplitN(prefix, "/", 2)
	if len(parts) != 2 {
		return 0, nil
	}
	_, n4, err := net.ParseCIDR(prefix)
	if err != nil || n4 == nil {
		return 0, nil
	}
	var n int64
	for _, a := range r.attempts {
		if a.Success || a.CreatedAt.Before(since) {
			continue
		}
		ip := net.ParseIP(a.ClientIP)
		if ip == nil {
			continue
		}
		if n4.Contains(ip) {
			n++
		}
	}
	return n, nil
}

func (r *fakeUserRepo) CountLoginFailuresByUsername(username string, since time.Time) (int64, error) {
	var n int64
	for _, a := range r.attempts {
		if !a.Success && a.Username == username && !a.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (r *fakeUserRepo) LastSuccessfulLogin(username string) (*model.LoginAttempt, error) {
	for i := len(r.attempts) - 1; i >= 0; i-- {
		a := r.attempts[i]
		if a.Success && a.Username == username {
			return &a, nil
		}
	}
	return nil, nil
}

func hash(pw string) string {
	h, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	return string(h)
}

func newEngine(a *Authenticator) *gin.Engine {
	r := gin.New()
	pub := r.Group("/api")
	pub.POST("/auth/login", a.LoginHandler)
	pub.GET("/auth/status", a.StatusHandler)
	g := r.Group("/api", a.Middleware())
	g.GET("/ping", func(c *gin.Context) {
		id, name, role := Principal(c)
		c.JSON(200, gin.H{"id": id, "name": name, "role": role})
	})
	return r
}

func TestPasswordMode_LoginFlow(t *testing.T) {
	cfg := &config.Config{AuthMode: config.AuthModePassword, AdminUsername: "admin"}
	admin := &model.User{ID: 1, Username: "admin", PasswordHash: hash("secret"), Role: model.RoleAdmin}
	a := New(cfg, nil, newFakeRepo(admin))
	r := newEngine(a)

	// unauthenticated -> 401
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/ping", nil)
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}

	// wrong password -> 401
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}

	// correct password (with username) -> token
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("login: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Token == "" || resp.Role != model.RoleAdmin {
		t.Fatalf("unexpected login payload: %+v", resp)
	}

	// token accepted -> 200 with principal
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/ping", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("authed: want 200 got %d", w.Code)
	}
	var p struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.ID != 1 || p.Name != "admin" || p.Role != model.RoleAdmin {
		t.Fatalf("principal propagation: %+v", p)
	}

	// legacy client (no username) still works: falls back to cfg.AdminUsername
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("legacy login: want 200 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPasswordMode_DisabledUserRejected(t *testing.T) {
	cfg := &config.Config{AuthMode: config.AuthModePassword, AdminUsername: "admin"}
	admin := &model.User{ID: 1, Username: "admin", PasswordHash: hash("secret"), Role: model.RoleAdmin}
	disabled := &model.User{ID: 2, Username: "alice", PasswordHash: hash("s3cret"), Role: model.RoleUser, Disabled: true}
	a := New(cfg, nil, newFakeRepo(admin, disabled))
	r := newEngine(a)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"alice","password":"s3cret"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("disabled user: want 401 got %d", w.Code)
	}
}

func TestIPWhitelistMode_SystemAdminPrincipal(t *testing.T) {
	cfg := &config.Config{
		AuthMode:         config.AuthModeIPWhitelist,
		AdminIPWhitelist: []*net.IPNet{mustCIDR("10.0.0.0/24")},
	}
	admin := &model.User{ID: 7, Username: "sys", Role: model.RoleAdmin}
	a := New(cfg, nil, newFakeRepo(admin))
	a.SetSystemAdmin(admin.ID, admin.Username)
	r := newEngine(a)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/ping", nil)
	req.RemoteAddr = "10.0.0.5:12345"
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("whitelisted IP: want 200 got %d", w.Code)
	}
	var p struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.ID != 7 || p.Role != model.RoleAdmin {
		t.Fatalf("principal: %+v", p)
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/ping", nil)
	req.RemoteAddr = "203.0.113.9:1"
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("non-whitelisted IP: want 403 got %d", w.Code)
	}
}

func TestPasswordMode_IPLockout(t *testing.T) {
	cfg := &config.Config{
		AuthMode:               config.AuthModePassword,
		AdminUsername:          "admin",
		LoginFailMaxPerIP:      3,
		LoginFailWindowIPMin:   10,
		LoginFailMaxPerUser:    0, // focus on IP path
		LoginFailWindowUserMin: 0,
		LoginLockoutIPMin:      10,
	}
	admin := &model.User{ID: 1, Username: "admin", PasswordHash: hash("secret"), Role: model.RoleAdmin}
	a := New(cfg, nil, newFakeRepo(admin))
	r := newEngine(a)

	bad := strings.NewReader(``)
	_ = bad

	// Exhaust the limit: 3 failed attempts, all returning 401.
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/auth/login",
			strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.7:1234"
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("attempt #%d want 401 got %d", i+1, w.Code)
		}
	}

	// Next attempt - even with the correct password - must be locked out.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.7:5678"
	r.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Fatalf("after IP lockout: want 429 got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code    string `json:"code"`
		RetrySec int   `json:"retry_after_secs"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != ReasonLockedIP || resp.RetrySec != 600 {
		t.Fatalf("unexpected lockout payload: %+v", resp)
	}
}

func TestPasswordMode_UsernameLockout(t *testing.T) {
	cfg := &config.Config{
		AuthMode:               config.AuthModePassword,
		AdminUsername:          "admin",
		LoginFailMaxPerUser:    2,
		LoginFailWindowUserMin: 15,
		LoginLockoutUserMin:    15,
	}
	admin := &model.User{ID: 1, Username: "admin", PasswordHash: hash("secret"), Role: model.RoleAdmin}
	a := New(cfg, nil, newFakeRepo(admin))
	r := newEngine(a)

	// Two failed attempts from different IPs - simulating a proxy pool -
	// should still trip the per-username limiter.
	for i, ip := range []string{"198.51.100.1:1", "198.51.100.2:2"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/auth/login",
			strings.NewReader(`{"username":"admin","password":"nope"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("attempt #%d want 401 got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.9:9"
	r.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Fatalf("after username lockout: want 429 got %d", w.Code)
	}
}

func TestPasswordMode_LastLoginReturned(t *testing.T) {
	cfg := &config.Config{AuthMode: config.AuthModePassword, AdminUsername: "admin"}
	admin := &model.User{ID: 1, Username: "admin", PasswordHash: hash("secret"), Role: model.RoleAdmin}
	a := New(cfg, nil, newFakeRepo(admin))
	r := newEngine(a)

	// First login: no prior record, no last_login in response.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.1:1"
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("first login want 200 got %d", w.Code)
	}
	var first map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &first)
	if _, ok := first["last_login"]; ok {
		t.Fatalf("first login should NOT expose last_login: %+v", first)
	}

	// Second login from a different IP should echo the first IP as last_login.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.2:1"
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("second login want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var second map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &second)
	ll, ok := second["last_login"].(map[string]any)
	if !ok {
		t.Fatalf("second login must expose last_login: %+v", second)
	}
	if ip, _ := ll["client_ip"].(string); ip != "10.0.0.1" {
		t.Fatalf("unexpected last_login ip: %+v", ll)
	}
}

func TestNoneMode_SystemAdminPrincipal(t *testing.T) {
	cfg := &config.Config{AuthMode: config.AuthModeNone}
	admin := &model.User{ID: 42, Username: "system", Role: model.RoleAdmin}
	a := New(cfg, nil, newFakeRepo(admin))
	a.SetSystemAdmin(admin.ID, admin.Username)
	r := newEngine(a)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/ping", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("none mode: want 200 got %d", w.Code)
	}
	var p struct {
		ID uint `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.ID != 42 {
		t.Fatalf("system admin id propagated: %+v", p)
	}
}
