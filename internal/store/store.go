// Package store is a thin GORM wrapper that exposes intent-revealing
// helpers to the rest of FrpDeck instead of leaking *gorm.DB everywhere.
package store

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/teacat99/FrpDeck/internal/model"
)

// DefaultAdminUsername is the seed admin account username used when the
// users table is empty and no explicit FRPDECK_ADMIN_USERNAME is provided.
const DefaultAdminUsername = "admin"

// DefaultAdminPassword is the fallback password seeded on first boot when
// FRPDECK_ADMIN_PASSWORD is not provided. It exists purely for out-of-box
// convenience; operators are expected to change it via the UI immediately.
const DefaultAdminPassword = "passwd"

// Store is a thin GORM wrapper that exposes intent-revealing helpers to the
// rest of the codebase instead of leaking *gorm.DB everywhere.
type Store struct {
	db *gorm.DB
}

// New opens (or creates) a SQLite database at path and runs migrations.
func New(path string) (*Store, error) {
	gormLogger := logger.New(
		log.New(os.Stderr, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1) // SQLite writers are serialised

	if err := db.AutoMigrate(
		&model.Endpoint{},
		&model.Tunnel{},
		&model.Profile{},
		&model.ProfileBinding{},
		&model.RemoteNode{},
		&model.Setting{},
		&model.AuditLog{},
		&model.User{},
		&model.LoginAttempt{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// SeedAdminIfEmpty ensures there is at least one administrator row in the
// users table. When the table is empty it inserts one (username taken from
// preferredUsername or "admin"; password from preferredPassword or the
// hard-coded "passwd" fallback).
//
// Returns the seeded admin ID so the caller can use it as the implicit
// actor for ipwhitelist/none modes.
func (s *Store) SeedAdminIfEmpty(preferredUsername, preferredPassword string) (uint, error) {
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return s.firstAdminID()
	}

	username := preferredUsername
	if username == "" {
		username = DefaultAdminUsername
	}
	pw := preferredPassword
	usedFallback := pw == ""
	if usedFallback {
		pw = DefaultAdminPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash admin password: %w", err)
	}
	now := time.Now()
	u := &model.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         model.RoleAdmin,
		Disabled:     false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(u).Error; err != nil {
		return 0, fmt.Errorf("seed admin: %w", err)
	}
	if usedFallback {
		log.Printf("[WARN] seeded default admin user %q with password %q - please change it immediately via the UI", username, DefaultAdminPassword)
	} else {
		log.Printf("seeded admin user %q from FRPDECK_ADMIN_PASSWORD", username)
	}
	return u.ID, nil
}

func (s *Store) firstAdminID() (uint, error) {
	var u model.User
	err := s.db.Where("role = ? AND disabled = ?", model.RoleAdmin, false).
		Order("id ASC").First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return u.ID, nil
}

// DB returns the underlying *gorm.DB for callers that need advanced queries.
func (s *Store) DB() *gorm.DB { return s.db }

// ------------------------- endpoints -------------------------

// ListEndpoints returns every endpoint ordered by creation time.
func (s *Store) ListEndpoints() ([]model.Endpoint, error) {
	var out []model.Endpoint
	err := s.db.Order("id ASC").Find(&out).Error
	return out, err
}

// GetEndpoint fetches a single endpoint; returns (nil, nil) when absent.
func (s *Store) GetEndpoint(id uint) (*model.Endpoint, error) {
	var e model.Endpoint
	if err := s.db.First(&e, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// CreateEndpoint inserts a new endpoint and returns the populated row.
func (s *Store) CreateEndpoint(e *model.Endpoint) error {
	return s.db.Create(e).Error
}

// UpdateEndpoint persists the full entity.
func (s *Store) UpdateEndpoint(e *model.Endpoint) error {
	return s.db.Save(e).Error
}

// DeleteEndpoint hard-deletes the endpoint and every tunnel under it.
// Wrapped in a single transaction so a partial failure leaves no
// orphaned tunnels behind.
func (s *Store) DeleteEndpoint(id uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("endpoint_id = ?", id).Delete(&model.Tunnel{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Endpoint{}, id).Error
	})
}

// ------------------------- tunnels -------------------------

// ListTunnels returns every tunnel; the caller may apply filters
// client-side. P0 keeps the API minimal — pagination / filters arrive
// alongside the UI in P1.
func (s *Store) ListTunnels() ([]model.Tunnel, error) {
	var out []model.Tunnel
	err := s.db.Order("endpoint_id ASC, id ASC").Find(&out).Error
	return out, err
}

// ListTunnelsByEndpoint returns every tunnel attached to a single endpoint.
func (s *Store) ListTunnelsByEndpoint(endpointID uint) ([]model.Tunnel, error) {
	var out []model.Tunnel
	err := s.db.Where("endpoint_id = ?", endpointID).Order("id ASC").Find(&out).Error
	return out, err
}

// ListActiveTunnels returns tunnels currently in pending or active state.
// Used by the lifecycle manager at boot to restore scheduled timers.
func (s *Store) ListActiveTunnels() ([]model.Tunnel, error) {
	var out []model.Tunnel
	err := s.db.Where("status IN ?", []string{model.StatusPending, model.StatusActive}).
		Order("id ASC").Find(&out).Error
	return out, err
}

// GetTunnel fetches a single tunnel; returns (nil, nil) when absent.
func (s *Store) GetTunnel(id uint) (*model.Tunnel, error) {
	var t model.Tunnel
	if err := s.db.First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// CreateTunnel inserts a new tunnel.
func (s *Store) CreateTunnel(t *model.Tunnel) error {
	return s.db.Create(t).Error
}

// UpdateTunnel persists the full entity.
func (s *Store) UpdateTunnel(t *model.Tunnel) error {
	return s.db.Save(t).Error
}

// DeleteTunnel hard-deletes a tunnel.
func (s *Store) DeleteTunnel(id uint) error {
	return s.db.Delete(&model.Tunnel{}, id).Error
}

// ------------------------- remote nodes -------------------------

// ListRemoteNodes returns every paired peer regardless of direction.
// The UI splits them into "managed by me" / "manages me" tabs client side.
func (s *Store) ListRemoteNodes() ([]model.RemoteNode, error) {
	var out []model.RemoteNode
	err := s.db.Order("id ASC").Find(&out).Error
	return out, err
}

// GetRemoteNode returns nil (no error) when the row does not exist; the
// auth/redeem path uses this distinction to surface a stable 401.
func (s *Store) GetRemoteNode(id uint) (*model.RemoteNode, error) {
	var n model.RemoteNode
	if err := s.db.First(&n, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &n, nil
}

// CreateRemoteNode inserts a new pairing row.
func (s *Store) CreateRemoteNode(n *model.RemoteNode) error {
	return s.db.Create(n).Error
}

// UpdateRemoteNode persists the full entity (status / last_seen tracking).
func (s *Store) UpdateRemoteNode(n *model.RemoteNode) error {
	return s.db.Save(n).Error
}

// DeleteRemoteNode hard-deletes the pairing row. Callers are expected to
// also delete or stop the associated stcp tunnel; the store does NOT do
// that automatically because tunnel teardown requires a running driver
// reference that the store does not own.
func (s *Store) DeleteRemoteNode(id uint) error {
	return s.db.Delete(&model.RemoteNode{}, id).Error
}

// ------------------------- users -------------------------

func (s *Store) CreateUser(u *model.User) error {
	now := time.Now()
	u.CreatedAt = now
	u.UpdatedAt = now
	return s.db.Create(u).Error
}

func (s *Store) GetUserByID(id uint) (*model.User, error) {
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUserByUsername(name string) (*model.User, error) {
	var u model.User
	if err := s.db.Where("username = ?", name).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (s *Store) ListUsers() ([]model.User, error) {
	var out []model.User
	err := s.db.Order("id ASC").Find(&out).Error
	return out, err
}

func (s *Store) UpdateUserFields(id uint, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	return s.db.Model(&model.User{}).Where("id = ?", id).Updates(fields).Error
}

func (s *Store) SetUserPasswordHash(id uint, hash string) error {
	return s.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{"password_hash": hash, "updated_at": time.Now()}).Error
}

func (s *Store) DeleteUser(id uint) error {
	return s.db.Delete(&model.User{}, id).Error
}

func (s *Store) CountActiveAdmins() (int64, error) {
	var n int64
	err := s.db.Model(&model.User{}).
		Where("role = ? AND disabled = ?", model.RoleAdmin, false).
		Count(&n).Error
	return n, err
}

// ------------------------- settings (KV) -------------------------

func (s *Store) GetSetting(key, fallback string) (string, error) {
	v, ok, err := s.LookupSetting(key)
	if err != nil {
		return "", err
	}
	if !ok {
		return fallback, nil
	}
	return v, nil
}

func (s *Store) LookupSetting(key string) (string, bool, error) {
	var rows []model.Setting
	if err := s.db.Where("key = ?", key).Limit(1).Find(&rows).Error; err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	return rows[0].Value, true, nil
}

func (s *Store) SetSetting(key, value string) error {
	now := time.Now()
	return s.db.Save(&model.Setting{Key: key, Value: value, UpdatedAt: now}).Error
}

func (s *Store) ListSettings() ([]model.Setting, error) {
	var out []model.Setting
	err := s.db.Find(&out).Error
	return out, err
}

// ------------------------- audit -------------------------

func (s *Store) WriteAudit(entry *model.AuditLog) error {
	entry.CreatedAt = time.Now()
	return s.db.Create(entry).Error
}

// AuditFilter is the query payload for ListAudit.
type AuditFilter struct {
	From   time.Time
	To     time.Time
	IP     string
	Limit  int
	Offset int
}

func (s *Store) ListAudit(filter AuditFilter) ([]model.AuditLog, int64, error) {
	q := s.db.Model(&model.AuditLog{})
	if !filter.From.IsZero() {
		q = q.Where("created_at >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		q = q.Where("created_at <= ?", filter.To)
	}
	if filter.IP != "" {
		q = q.Where("actor_ip = ?", filter.IP)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	q = q.Order("created_at DESC")
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit).Offset(filter.Offset)
	}
	var out []model.AuditLog
	if err := q.Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *Store) PurgeHistory(retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	return s.db.Where("created_at < ?", cutoff).Delete(&model.AuditLog{}).Error
}

// ------------------------- login attempts -------------------------

func (s *Store) RecordLoginAttempt(a *model.LoginAttempt) error {
	a.CreatedAt = time.Now()
	return s.db.Create(a).Error
}

func (s *Store) CountLoginFailuresByIP(ip string, since time.Time) (int64, error) {
	var n int64
	err := s.db.Model(&model.LoginAttempt{}).
		Where("client_ip = ? AND success = ? AND created_at >= ?", ip, false, since).
		Count(&n).Error
	return n, err
}

func (s *Store) CountLoginFailuresByUsername(username string, since time.Time) (int64, error) {
	var n int64
	err := s.db.Model(&model.LoginAttempt{}).
		Where("username = ? AND success = ? AND created_at >= ?", username, false, since).
		Count(&n).Error
	return n, err
}

// CountLoginFailuresByIPSubnet returns the number of failed attempts whose
// recorded ClientIP falls inside the given CIDR prefix. Used only when
// LoginFailSubnetBits > 0.
func (s *Store) CountLoginFailuresByIPSubnet(prefix string, since time.Time) (int64, error) {
	parts := strings.SplitN(prefix, "/", 2)
	if len(parts) != 2 {
		return 0, nil
	}
	bits, err := strconv.Atoi(parts[1])
	if err != nil || bits <= 0 {
		return 0, nil
	}
	ip := net.ParseIP(parts[0])
	if ip == nil {
		return 0, nil
	}
	is4 := ip.To4() != nil
	matches, scanErr := s.scanIPsInRange(since, ip, bits, is4)
	if scanErr != nil {
		return 0, scanErr
	}
	if len(matches) == 0 {
		return 0, nil
	}
	var n int64
	err = s.db.Model(&model.LoginAttempt{}).
		Where("success = ? AND created_at >= ? AND client_ip IN ?", false, since, matches).
		Count(&n).Error
	return n, err
}

func (s *Store) scanIPsInRange(since time.Time, prefixIP net.IP, bits int, is4 bool) ([]string, error) {
	var ips []string
	err := s.db.Model(&model.LoginAttempt{}).
		Where("success = ? AND created_at >= ?", false, since).
		Distinct("client_ip").
		Pluck("client_ip", &ips).Error
	if err != nil {
		return nil, err
	}
	mask := net.CIDRMask(bits, 32)
	if !is4 {
		mask = net.CIDRMask(bits, 128)
	}
	expected := prefixIP.Mask(mask)
	out := make([]string, 0, len(ips))
	for _, raw := range ips {
		candidate := net.ParseIP(raw)
		if candidate == nil {
			continue
		}
		if is4 {
			c4 := candidate.To4()
			if c4 == nil {
				continue
			}
			if c4.Mask(mask).Equal(expected) {
				out = append(out, raw)
			}
		} else {
			if candidate.Mask(mask).Equal(expected) {
				out = append(out, raw)
			}
		}
	}
	return out, nil
}

func (s *Store) ListLoginAttempts(username string, limit int) ([]model.LoginAttempt, error) {
	q := s.db.Model(&model.LoginAttempt{})
	if username != "" {
		q = q.Where("username = ?", username)
	}
	if limit <= 0 {
		limit = 100
	}
	var out []model.LoginAttempt
	err := q.Order("created_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

// LastSuccessfulLogin returns the most recent successful login for username.
// Callers invoke this BEFORE recording the current success so the returned
// row is the previous session.
func (s *Store) LastSuccessfulLogin(username string) (*model.LoginAttempt, error) {
	var row model.LoginAttempt
	err := s.db.Where("username = ? AND success = ?", username, true).
		Order("created_at DESC").First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *Store) PurgeLoginAttempts(failRetentionDays, successRetentionDays int) error {
	if failRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -failRetentionDays)
		if err := s.db.Where("success = ? AND created_at < ?", false, cutoff).
			Delete(&model.LoginAttempt{}).Error; err != nil {
			return err
		}
	}
	if successRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -successRetentionDays)
		if err := s.db.Where("success = ? AND created_at < ?", true, cutoff).
			Delete(&model.LoginAttempt{}).Error; err != nil {
			return err
		}
	}
	return nil
}
