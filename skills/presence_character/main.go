// Package main implements the presence/character skill.
// Manages character overlays and selects emotion-based sprites.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/oklog/ulid/v2"

	_ "modernc.org/sqlite"
)

const command = "presence/character"

// Input defines the input parameters for presence/character with character and overlay management.
type Input struct {
	Action         string  `json:"action" validate:"required,oneof=get select register list register_overlay"`
	ConversationID string  `json:"conversation_id"`
	CharacterID    string  `json:"character_id"`
	Emotion        string  `json:"emotion"`
	Intensity      float64 `json:"intensity"`
	Name           string  `json:"name"`
	AvatarDigest   string  `json:"avatar_digest"`
	VoiceID        string  `json:"voice_id"`
	OverlayDigest  string  `json:"overlay_digest"`
}

// Character represents a companion character with personality and visual assets.
type Character struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Name           string    `json:"name"`
	AvatarDigest   string    `json:"avatar_digest,omitempty"`
	VoiceID        string    `json:"voice_id,omitempty"`
	BaseMood       string    `json:"base_mood"`
	Backstory      string    `json:"backstory,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Overlay represents a character emotion overlay with intensity variants.
type Overlay struct {
	ID                  string    `json:"id"`
	CharacterID         string    `json:"character_id"`
	Emotion             string    `json:"emotion"`
	OverlayDigest       string    `json:"overlay_digest"`
	IntensityLowDigest  string    `json:"intensity_low_digest,omitempty"`
	IntensityHighDigest string    `json:"intensity_high_digest,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// Output defines the output for presence/character with character and overlay data.
type Output struct {
	Character  *Character  `json:"character,omitempty"`
	Overlay    *Overlay    `json:"overlay,omitempty"`
	Characters []Character `json:"characters,omitempty"`
	Overlays   []Overlay   `json:"overlays,omitempty"`
	Action     string      `json:"action"`
}

// main is the skill entry point for presence/character.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates character and overlay management with emotion-based sprite selection.
//
// Index:
//   Purpose: Manage companion characters and emotion overlays with intensity-based sprite selection
//   Keywords: presence/character, character_management, emotion_overlays, sprite_selection, companion_system
//   Related: getCharacter, selectOverlay, registerCharacter, listCharacters, registerOverlay, nullString
//   Flow: open companion database → validate action → route to handler → execute database operations → emit results
//   Resources: companion SQLite database
//   Events: character management events
//   OutputFields: character, overlay, characters, overlays, action
// [[domain:companion-character-management]]
// [[protocol:character-action-dispatch]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Open companion database
	dbPath := filepath.Join(rc.Config.Storage.Root, "companion.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return skillerr.WrapIO("open companion database", err)
	}
	defer db.Close()

	var out Output
	out.Action = in.Action

	switch in.Action {
	case "get":
		if in.CharacterID == "" {
			return skillerr.Arg("character_id required for get action")
		}
		char, overlays, err := getCharacter(ctx, db, in.CharacterID)
		if err != nil {
			return err
		}
		out.Character = char
		out.Overlays = overlays

	case "select":
		if in.CharacterID == "" {
			return skillerr.Arg("character_id required for select action")
		}
		if in.Emotion == "" {
			return skillerr.Arg("emotion required for select action")
		}
		overlay, err := selectOverlay(ctx, db, in.CharacterID, in.Emotion, in.Intensity)
		if err != nil {
			return err
		}
		out.Overlay = overlay

	case "register":
		if in.ConversationID == "" {
			return skillerr.Arg("conversation_id required for register action")
		}
		if in.Name == "" {
			return skillerr.Arg("name required for register action")
		}
		char, err := registerCharacter(ctx, db, in)
		if err != nil {
			return err
		}
		out.Character = char

	case "list":
		if in.ConversationID == "" {
			return skillerr.Arg("conversation_id required for list action")
		}
		chars, err := listCharacters(ctx, db, in.ConversationID)
		if err != nil {
			return err
		}
		out.Characters = chars

	case "register_overlay":
		if in.CharacterID == "" {
			return skillerr.Arg("character_id required for register_overlay action")
		}
		if in.Emotion == "" {
			return skillerr.Arg("emotion required for register_overlay action")
		}
		if in.OverlayDigest == "" {
			return skillerr.Arg("overlay_digest required for register_overlay action")
		}
		overlay, err := registerOverlay(ctx, db, in)
		if err != nil {
			return err
		}
		out.Overlay = overlay

	default:
		return skillerr.Arg("invalid action: " + in.Action)
	}

	return skillout.Emit(rc, command, out)
}

// getCharacter retrieves a character and its overlays with null-safe field handling.
func getCharacter(ctx context.Context, db *sql.DB, characterID string) (*Character, []Overlay, error) {
	char := &Character{}
	var avatar, voice, backstory sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, conversation_id, name, avatar_digest, voice_id, base_mood, backstory, created_at, updated_at
		FROM companion_characters
		WHERE id = ?
	`, characterID).Scan(
		&char.ID, &char.ConversationID, &char.Name,
		&avatar, &voice,
		&char.BaseMood, &backstory,
		&char.CreatedAt, &char.UpdatedAt,
	)
	// Check errors before accessing scanned values (NullString fields are invalid on error)
	if err == sql.ErrNoRows {
		return nil, nil, skillerr.NotFound("character not found: " + characterID)
	}
	if err != nil {
		return nil, nil, skillerr.WrapIO("query character", err)
	}
	// Now safe to extract NullString values
	char.AvatarDigest = avatar.String
	char.VoiceID = voice.String
	char.Backstory = backstory.String

	// Get overlays
	rows, err := db.QueryContext(ctx, `
		SELECT id, character_id, emotion, overlay_digest, intensity_low_digest, intensity_high_digest, created_at
		FROM companion_character_overlays
		WHERE character_id = ?
		ORDER BY emotion
	`, characterID)
	if err != nil {
		return nil, nil, skillerr.WrapIO("query overlays", err)
	}
	defer rows.Close()

	var overlays []Overlay
	for rows.Next() {
		var o Overlay
		var lowDigest, highDigest sql.NullString
		if err := rows.Scan(&o.ID, &o.CharacterID, &o.Emotion, &o.OverlayDigest, &lowDigest, &highDigest, &o.CreatedAt); err != nil {
			return nil, nil, skillerr.WrapIO("scan overlay", err)
		}
		o.IntensityLowDigest = lowDigest.String
		o.IntensityHighDigest = highDigest.String
		overlays = append(overlays, o)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, skillerr.WrapIO("iterate overlays", err)
	}

	return char, overlays, nil
}

// selectOverlay finds the best matching overlay for the given emotion and intensity with fallback logic.
func selectOverlay(ctx context.Context, db *sql.DB, characterID, emotion string, intensity float64) (*Overlay, error) {
	var o Overlay
	var lowDigest, highDigest sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT id, character_id, emotion, overlay_digest, intensity_low_digest, intensity_high_digest, created_at
		FROM companion_character_overlays
		WHERE character_id = ? AND emotion = ?
	`, characterID, emotion).Scan(
		&o.ID, &o.CharacterID, &o.Emotion, &o.OverlayDigest, &lowDigest, &highDigest, &o.CreatedAt,
	)

	if err == sql.ErrNoRows {
		// Fall back to neutral if specific emotion not found
		err = db.QueryRowContext(ctx, `
			SELECT id, character_id, emotion, overlay_digest, intensity_low_digest, intensity_high_digest, created_at
			FROM companion_character_overlays
			WHERE character_id = ? AND emotion = 'neutral'
		`, characterID).Scan(
			&o.ID, &o.CharacterID, &o.Emotion, &o.OverlayDigest, &lowDigest, &highDigest, &o.CreatedAt,
		)
	}

	if err == sql.ErrNoRows {
		return nil, skillerr.NotFound(fmt.Sprintf("no overlay found for character %s emotion %s", characterID, emotion))
	}
	if err != nil {
		return nil, skillerr.WrapIO("query overlay", err)
	}

	o.IntensityLowDigest = lowDigest.String
	o.IntensityHighDigest = highDigest.String

	// Select intensity variant based on intensity level
	if intensity < 0.33 && o.IntensityLowDigest != "" {
		o.OverlayDigest = o.IntensityLowDigest
	} else if intensity > 0.66 && o.IntensityHighDigest != "" {
		o.OverlayDigest = o.IntensityHighDigest
	}
	// Otherwise use the default overlay_digest

	return &o, nil
}

// registerCharacter creates a new character with upsert support for conversation+name uniqueness.
func registerCharacter(ctx context.Context, db *sql.DB, in Input) (*Character, error) {
	id := ulid.Make().String()
	now := time.Now().UTC()

	baseMood := "neutral"
	if in.Emotion != "" {
		baseMood = in.Emotion
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO companion_characters (id, conversation_id, name, avatar_digest, voice_id, base_mood, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id, name) DO UPDATE SET
			avatar_digest = COALESCE(excluded.avatar_digest, companion_characters.avatar_digest),
			voice_id = COALESCE(excluded.voice_id, companion_characters.voice_id),
			updated_at = excluded.updated_at
	`, id, in.ConversationID, in.Name, nullString(in.AvatarDigest), nullString(in.VoiceID), baseMood, now, now)
	if err != nil {
		return nil, skillerr.WrapIO("insert character", err)
	}

	// Retrieve the actual character (in case of upsert)
	char := &Character{}
	var avatar, voice, backstory sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT id, conversation_id, name, avatar_digest, voice_id, base_mood, backstory, created_at, updated_at
		FROM companion_characters
		WHERE conversation_id = ? AND name = ?
	`, in.ConversationID, in.Name).Scan(
		&char.ID, &char.ConversationID, &char.Name,
		&avatar, &voice,
		&char.BaseMood, &backstory,
		&char.CreatedAt, &char.UpdatedAt,
	)
	if err != nil {
		return nil, skillerr.WrapIO("query created character", err)
	}
	char.AvatarDigest = avatar.String
	char.VoiceID = voice.String
	char.Backstory = backstory.String

	return char, nil
}

// listCharacters returns all characters for a conversation ordered by name.
func listCharacters(ctx context.Context, db *sql.DB, conversationID string) ([]Character, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, conversation_id, name, avatar_digest, voice_id, base_mood, backstory, created_at, updated_at
		FROM companion_characters
		WHERE conversation_id = ?
		ORDER BY name
	`, conversationID)
	if err != nil {
		return nil, skillerr.WrapIO("query characters", err)
	}
	defer rows.Close()

	var chars []Character
	for rows.Next() {
		var c Character
		var avatar, voice, backstory sql.NullString
		if err := rows.Scan(&c.ID, &c.ConversationID, &c.Name, &avatar, &voice, &c.BaseMood, &backstory, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, skillerr.WrapIO("scan character", err)
		}
		c.AvatarDigest = avatar.String
		c.VoiceID = voice.String
		c.Backstory = backstory.String
		chars = append(chars, c)
	}
	if err := rows.Err(); err != nil {
		return nil, skillerr.WrapIO("iterate characters", err)
	}

	return chars, nil
}

// registerOverlay adds or updates an emotion overlay for a character with conflict resolution.
func registerOverlay(ctx context.Context, db *sql.DB, in Input) (*Overlay, error) {
	id := ulid.Make().String()
	now := time.Now().UTC()

	_, err := db.ExecContext(ctx, `
		INSERT INTO companion_character_overlays (id, character_id, emotion, overlay_digest, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(character_id, emotion) DO UPDATE SET
			overlay_digest = excluded.overlay_digest
	`, id, in.CharacterID, in.Emotion, in.OverlayDigest, now)
	if err != nil {
		return nil, skillerr.WrapIO("insert overlay", err)
	}

	// Retrieve the actual overlay
	var o Overlay
	var lowDigest, highDigest sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT id, character_id, emotion, overlay_digest, intensity_low_digest, intensity_high_digest, created_at
		FROM companion_character_overlays
		WHERE character_id = ? AND emotion = ?
	`, in.CharacterID, in.Emotion).Scan(
		&o.ID, &o.CharacterID, &o.Emotion, &o.OverlayDigest, &lowDigest, &highDigest, &o.CreatedAt,
	)
	if err != nil {
		return nil, skillerr.WrapIO("query created overlay", err)
	}
	o.IntensityLowDigest = lowDigest.String
	o.IntensityHighDigest = highDigest.String

	return &o, nil
}

// nullString converts empty string to sql.NullString for database nullable fields.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
