// Profile + ProfileBinding CRUD helpers (P8-C).
//
// Profile semantics:
//   - One Profile may be `active` at a time. Activating row N atomically
//     sets every other Profile.Active=false so the invariant holds even
//     if two requests race.
//   - ProfileBinding is many-to-many across (Profile, Endpoint, Tunnel).
//     A binding with TunnelID == 0 means "every tunnel under endpoint".
//     The activation reconciler resolves this fan-out.

package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/teacat99/FrpDeck/internal/model"
)

// ListProfiles returns every saved profile in stable id order.
func (s *Store) ListProfiles() ([]model.Profile, error) {
	var out []model.Profile
	err := s.db.Order("id ASC").Find(&out).Error
	return out, err
}

// GetProfile fetches a single profile; returns (nil, nil) when absent.
func (s *Store) GetProfile(id uint) (*model.Profile, error) {
	var p model.Profile
	if err := s.db.First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetActiveProfile returns the currently-active profile, or (nil, nil)
// when no profile is active. The DB invariant (at most one row with
// active=true) is enforced by ActivateProfile.
func (s *Store) GetActiveProfile() (*model.Profile, error) {
	var p model.Profile
	if err := s.db.Where("active = ?", true).Order("id ASC").First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// CreateProfile inserts a new profile and any seed bindings supplied.
// Bindings are stored verbatim — the caller is responsible for ensuring
// referential integrity (the API layer validates IDs before calling).
func (s *Store) CreateProfile(p *model.Profile, bindings []model.ProfileBinding) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// Inserting an `active=true` profile must displace any prior
		// active row to keep the global invariant.
		if p.Active {
			if err := tx.Model(&model.Profile{}).Where("active = ?", true).Update("active", false).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(p).Error; err != nil {
			return err
		}
		for i := range bindings {
			bindings[i].ProfileID = p.ID
		}
		if len(bindings) > 0 {
			if err := tx.Create(&bindings).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateProfile rewrites the profile metadata and replaces its binding
// set wholesale. Wholesale replace is the simplest semantics that
// matches the "edit then save" UI flow without forcing the frontend to
// diff binding lists.
func (s *Store) UpdateProfile(p *model.Profile, bindings []model.ProfileBinding) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if p.Active {
			if err := tx.Model(&model.Profile{}).Where("active = ? AND id <> ?", true, p.ID).Update("active", false).Error; err != nil {
				return err
			}
		}
		if err := tx.Save(p).Error; err != nil {
			return err
		}
		if err := tx.Where("profile_id = ?", p.ID).Delete(&model.ProfileBinding{}).Error; err != nil {
			return err
		}
		for i := range bindings {
			bindings[i].ID = 0
			bindings[i].ProfileID = p.ID
		}
		if len(bindings) > 0 {
			if err := tx.Create(&bindings).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteProfile removes the profile and its bindings. Refuses if the
// profile is currently active — operators must deactivate first to
// avoid leaving the runtime in an undefined state.
func (s *Store) DeleteProfile(id uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var p model.Profile
		if err := tx.First(&p, id).Error; err != nil {
			return err
		}
		if p.Active {
			return errors.New("cannot delete the active profile")
		}
		if err := tx.Where("profile_id = ?", id).Delete(&model.ProfileBinding{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Profile{}, id).Error
	})
}

// ListProfileBindings returns every binding for a profile, ordered for
// stable diffs. Endpoint-scoped bindings (TunnelID=0) come first.
func (s *Store) ListProfileBindings(profileID uint) ([]model.ProfileBinding, error) {
	var out []model.ProfileBinding
	err := s.db.Where("profile_id = ?", profileID).
		Order("endpoint_id ASC, tunnel_id ASC, id ASC").
		Find(&out).Error
	return out, err
}

// ActivateProfile marks `id` as active and applies its bindings to
// Endpoint.Enabled / Tunnel.Enabled. Anything not referenced by the
// profile is disabled. Endpoint-scoped bindings (TunnelID == 0) enable
// every tunnel under that endpoint.
//
// The whole operation runs inside a single transaction so a partial
// failure rolls back: there is never a half-applied profile.
func (s *Store) ActivateProfile(id uint) (*model.Profile, error) {
	var out model.Profile
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&out, id).Error; err != nil {
			return err
		}

		// Single-active invariant.
		if err := tx.Model(&model.Profile{}).Where("active = ? AND id <> ?", true, id).Update("active", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&out).Update("active", true).Error; err != nil {
			return err
		}

		var bindings []model.ProfileBinding
		if err := tx.Where("profile_id = ?", id).Find(&bindings).Error; err != nil {
			return err
		}

		// Build allow-sets. Endpoint-scoped bindings (TunnelID==0) act
		// as a wildcard for that endpoint's tunnels.
		endpointAllow := map[uint]bool{}
		tunnelAllow := map[uint]bool{}
		endpointWildcard := map[uint]bool{}
		for _, b := range bindings {
			if b.EndpointID == 0 && b.TunnelID == 0 {
				continue
			}
			if b.EndpointID != 0 {
				endpointAllow[b.EndpointID] = true
			}
			if b.TunnelID == 0 {
				endpointWildcard[b.EndpointID] = true
			} else {
				tunnelAllow[b.TunnelID] = true
			}
		}

		// Endpoints: enabled iff present in endpointAllow.
		var eps []model.Endpoint
		if err := tx.Find(&eps).Error; err != nil {
			return err
		}
		for _, ep := range eps {
			want := endpointAllow[ep.ID]
			if ep.Enabled != want {
				if err := tx.Model(&model.Endpoint{}).Where("id = ?", ep.ID).Update("enabled", want).Error; err != nil {
					return err
				}
			}
		}

		// Tunnels: enabled iff their endpoint is wildcarded OR the
		// tunnel itself appears in the binding set.
		var tunnels []model.Tunnel
		if err := tx.Find(&tunnels).Error; err != nil {
			return err
		}
		for _, t := range tunnels {
			want := endpointWildcard[t.EndpointID] || tunnelAllow[t.ID]
			if t.Enabled != want {
				if err := tx.Model(&model.Tunnel{}).Where("id = ?", t.ID).Update("enabled", want).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("activate profile: %w", err)
	}
	out.Active = true
	return &out, nil
}

// DeactivateAllProfiles marks every profile as inactive. Useful as a
// "clear" action; tunnel/endpoint enabled flags are NOT touched so the
// last applied state stays in effect until a new profile is activated.
func (s *Store) DeactivateAllProfiles() error {
	return s.db.Model(&model.Profile{}).Where("active = ?", true).Update("active", false).Error
}
