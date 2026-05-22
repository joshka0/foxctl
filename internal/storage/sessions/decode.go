package sessions

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

func decodeTimestampInto(dst *time.Time, src, field string) error {
	if src == "" {
		return fmt.Errorf("decode %s: empty timestamp", field)
	}
	ts, err := sqlutil.ScanTimestamp(src)
	if err != nil {
		return fmt.Errorf("decode %s: %w", field, err)
	}
	*dst = ts
	return nil
}

func decodeNullableTimestampInto(dst *time.Time, src sql.NullString, field string) error {
	if !src.Valid {
		return nil
	}
	if src.String == "" {
		return nil
	}
	return decodeTimestampInto(dst, src.String, field)
}

func decodeJSONInto(src, field string, dest any) error {
	if err := sqlutil.ScanJSON(src, dest); err != nil {
		return fmt.Errorf("decode %s: %w", field, err)
	}
	return nil
}

func decodeNullableJSONInto(src sql.NullString, field string, dest any) error {
	if !src.Valid {
		return nil
	}
	return decodeJSONInto(src.String, field, dest)
}
