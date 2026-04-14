package calibration

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oklog/ulid/v2"

	"github.com/joshka0/foxctl/internal/storage/memory"
)

const (
	// ProfileName is the key used to store the calibration profile in memory.
	ProfileName = "calibration-profile"
	// ProfileType is the type identifier for calibration profiles.
	ProfileType = "calibration_profile"
)

// SaveProfile persists a calibration profile to the memory store.
func SaveProfile(ctx context.Context, store *memory.Store, profile *Profile) error {
	if profile == nil {
		return fmt.Errorf("profile is nil")
	}

	// Generate profile ID if not set
	if profile.ProfileID == "" {
		profile.ProfileID = ulid.Make().String()
	}

	result, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	// Build summary for searchability
	summary := buildSummary(profile)

	_, err = store.SaveResult(ctx, memory.SaveOptions{
		Name:      ProfileName,
		Type:      ProfileType,
		Workspace: profile.Workspace,
		Summary:   summary,
		Result:    result,
	})
	if err != nil {
		return fmt.Errorf("save profile: %w", err)
	}

	return nil
}

// LoadProfile retrieves a calibration profile from the memory store.
func LoadProfile(ctx context.Context, store *memory.Store, workspace string) (*Profile, error) {
	entry, err := store.Get(ctx, ProfileName, workspace)
	if err != nil {
		if err == memory.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get profile: %w", err)
	}

	var profile Profile
	if err := json.Unmarshal(entry.Result, &profile); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}

	return &profile, nil
}

// DeleteProfile removes a calibration profile from the memory store.
func DeleteProfile(ctx context.Context, store *memory.Store, workspace string) error {
	err := store.Delete(ctx, ProfileName, workspace)
	if err != nil && err != memory.ErrNotFound {
		return fmt.Errorf("delete profile: %w", err)
	}
	return nil
}

// buildSummary creates a searchable summary string for the profile.
func buildSummary(p *Profile) string {
	return fmt.Sprintf("calibration v%d: %s verbosity, %s depth, %s formality, %s autonomy",
		p.Version,
		p.Communication.Verbosity,
		p.Communication.TechnicalDepth,
		p.Tone.Formality,
		p.Trust.AutonomyLevel,
	)
}
