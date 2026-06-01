package quotas

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/joshka0/foxctl/internal/domain/agent"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

// Store defines the persistence interface for namespace quotas.
type Store interface {
	Close() error
	Get(ctx context.Context, ns string) (agent.Quotas, error)
	Set(ctx context.Context, ns string, quotas agent.Quotas) error
	Update(ctx context.Context, ns string, quotas agent.Quotas) error
	Delete(ctx context.Context, ns string) error
	ListAll(ctx context.Context) (map[string]agent.Quotas, error)
	GetConsumption(ctx context.Context, ns string) (agent.QuotaConsumption, error)
	UpdateConsumption(ctx context.Context, ns string, delta agent.QuotaConsumption) error
}

type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open initializes the quotas store rooted at the provided path.
// It opens the SQLite database file at root/quotas.db, applies required schema migrations, and returns a Store backed by that database.
// On failure it returns a non-nil error describing the problem.
func Open(ctx context.Context, root string) (Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "QUOTAS", "quotas.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("quotas: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}

func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func (s *sqlStore) Get(ctx context.Context, ns string) (agent.Quotas, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT ns, max_concurrent_jobs, cpu_limit, memMB_limit, llm_calls_per_min, egress_bytes_per_min
		FROM ns_quotas WHERE ns = ?`, ns)

	var quotas agent.Quotas
	err := row.Scan(&quotas.Namespace, &quotas.MaxConcurrentJobs, &quotas.CPULimit, &quotas.MemMBLimit,
		&quotas.LLMCallsPerMin, &quotas.EgressBytesPerMin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.Quotas{}, ErrNotFound
		}
		return agent.Quotas{}, fmt.Errorf("quotas: get: %w", err)
	}
	if err := ValidateLimits(quotas); err != nil {
		return agent.Quotas{}, fmt.Errorf("quotas: get invalid limits: %w", err)
	}

	return quotas, nil
}

func (s *sqlStore) Set(ctx context.Context, ns string, quotas agent.Quotas) error {
	if err := ValidateLimits(quotas); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ns_quotas (ns, max_concurrent_jobs, cpu_limit, memMB_limit, llm_calls_per_min, egress_bytes_per_min)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ns, quotas.MaxConcurrentJobs, quotas.CPULimit, quotas.MemMBLimit,
		quotas.LLMCallsPerMin, quotas.EgressBytesPerMin)
	if err != nil {
		return fmt.Errorf("quotas: set: %w", err)
	}
	return nil
}

func (s *sqlStore) Update(ctx context.Context, ns string, quotas agent.Quotas) error {
	if err := ValidateLimits(quotas); err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE ns_quotas
		SET max_concurrent_jobs = ?, cpu_limit = ?, memMB_limit = ?,
		    llm_calls_per_min = ?, egress_bytes_per_min = ?
		WHERE ns = ?`,
		quotas.MaxConcurrentJobs, quotas.CPULimit, quotas.MemMBLimit,
		quotas.LLMCallsPerMin, quotas.EgressBytesPerMin, ns)
	if err != nil {
		return fmt.Errorf("quotas: update: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("quotas: update rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *sqlStore) Delete(ctx context.Context, ns string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ns_quotas WHERE ns = ?`, ns)
	if err != nil {
		return fmt.Errorf("quotas: delete: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("quotas: delete rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *sqlStore) ListAll(ctx context.Context) (map[string]agent.Quotas, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ns, max_concurrent_jobs, cpu_limit, memMB_limit, llm_calls_per_min, egress_bytes_per_min
		FROM ns_quotas
		ORDER BY ns`)
	if err != nil {
		return nil, fmt.Errorf("quotas: list all: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close quotas list rows")
	}()

	result := make(map[string]agent.Quotas)
	for rows.Next() {
		var quotas agent.Quotas
		err := rows.Scan(&quotas.Namespace, &quotas.MaxConcurrentJobs, &quotas.CPULimit,
			&quotas.MemMBLimit, &quotas.LLMCallsPerMin, &quotas.EgressBytesPerMin)
		if err != nil {
			return nil, fmt.Errorf("quotas: scan: %w", err)
		}
		if err := ValidateLimits(quotas); err != nil {
			return nil, fmt.Errorf("quotas: scan invalid limits: %w", err)
		}
		result[quotas.Namespace] = quotas
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quotas: list all iteration: %w", err)
	}

	return result, nil
}

func (s *sqlStore) GetConsumption(ctx context.Context, ns string) (agent.QuotaConsumption, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT ns, active_jobs, cpu_used, memMB_used, llm_calls_1min, egress_bytes_1min, last_reset_ts
		FROM ns_consumption WHERE ns = ?`, ns)

	var consumption agent.QuotaConsumption
	err := row.Scan(&consumption.Namespace, &consumption.ActiveJobs, &consumption.CPUUsed,
		&consumption.MemMBUsed, &consumption.LLMCalls1Min, &consumption.EgressBytes1Min, &consumption.LastResetTS)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Return zero consumption if not found
			return agent.QuotaConsumption{Namespace: ns}, nil
		}
		return agent.QuotaConsumption{}, fmt.Errorf("quotas: get consumption: %w", err)
	}
	if err := ValidateConsumption(consumption); err != nil {
		return agent.QuotaConsumption{}, fmt.Errorf("quotas: get invalid consumption: %w", err)
	}

	return consumption, nil
}

func (s *sqlStore) UpdateConsumption(ctx context.Context, ns string, delta agent.QuotaConsumption) error {
	if delta.LastResetTS < 0 {
		return ErrNegativeConsumption
	}
	if nonNegativeConsumptionDelta(delta) {
		inserted, err := s.insertConsumption(ctx, ns, delta)
		if err != nil {
			return err
		}
		if inserted {
			return nil
		}
	}

	var updated int
	err := s.db.QueryRowContext(ctx, `
		UPDATE ns_consumption
		SET
			active_jobs = active_jobs + ?,
			cpu_used = cpu_used + ?,
			memMB_used = memMB_used + ?,
			llm_calls_1min = llm_calls_1min + ?,
			egress_bytes_1min = egress_bytes_1min + ?,
			last_reset_ts = CASE WHEN ? > last_reset_ts THEN ? ELSE last_reset_ts END
		WHERE
			ns = ? AND
			active_jobs + ? >= 0 AND
			cpu_used + ? >= 0 AND
			memMB_used + ? >= 0 AND
			llm_calls_1min + ? >= 0 AND
			egress_bytes_1min + ? >= 0
		RETURNING 1`,
		delta.ActiveJobs, delta.CPUUsed, delta.MemMBUsed,
		delta.LLMCalls1Min, delta.EgressBytes1Min,
		delta.LastResetTS, delta.LastResetTS, ns,
		delta.ActiveJobs, delta.CPUUsed, delta.MemMBUsed,
		delta.LLMCalls1Min, delta.EgressBytes1Min).Scan(&updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNegativeConsumption
		}
		return fmt.Errorf("quotas: update consumption: %w", err)
	}

	return nil
}

func (s *sqlStore) insertConsumption(ctx context.Context, ns string, consumption agent.QuotaConsumption) (bool, error) {
	var inserted int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO ns_consumption (ns, active_jobs, cpu_used, memMB_used, llm_calls_1min, egress_bytes_1min, last_reset_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ns) DO NOTHING
		RETURNING 1`,
		ns, consumption.ActiveJobs, consumption.CPUUsed, consumption.MemMBUsed,
		consumption.LLMCalls1Min, consumption.EgressBytes1Min, consumption.LastResetTS).Scan(&inserted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("quotas: insert consumption: %w", err)
	}
	return true, nil
}

func nonNegativeConsumptionDelta(delta agent.QuotaConsumption) bool {
	return delta.ActiveJobs >= 0 &&
		delta.CPUUsed >= 0 &&
		delta.MemMBUsed >= 0 &&
		delta.LLMCalls1Min >= 0 &&
		delta.EgressBytes1Min >= 0 &&
		delta.LastResetTS >= 0
}

func ValidateConsumption(consumption agent.QuotaConsumption) error {
	if nonNegativeConsumptionDelta(consumption) {
		return nil
	}
	return ErrNegativeConsumption
}

// ValidateLimits rejects negative quota limits. Zero is valid and means unlimited.
func ValidateLimits(quotas agent.Quotas) error {
	switch {
	case quotas.MaxConcurrentJobs < 0:
		return fmt.Errorf("%w: max_concurrent_jobs", ErrInvalidQuotaLimit)
	case quotas.CPULimit < 0:
		return fmt.Errorf("%w: cpu_limit", ErrInvalidQuotaLimit)
	case quotas.MemMBLimit < 0:
		return fmt.Errorf("%w: memMB_limit", ErrInvalidQuotaLimit)
	case quotas.LLMCallsPerMin < 0:
		return fmt.Errorf("%w: llm_calls_per_min", ErrInvalidQuotaLimit)
	case quotas.EgressBytesPerMin < 0:
		return fmt.Errorf("%w: egress_bytes_per_min", ErrInvalidQuotaLimit)
	default:
		return nil
	}
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS ns_quotas (
	ns                   TEXT PRIMARY KEY,
	max_concurrent_jobs  INTEGER NOT NULL DEFAULT 0,
	cpu_limit            INTEGER NOT NULL DEFAULT 0,
	memMB_limit          INTEGER NOT NULL DEFAULT 0,
	llm_calls_per_min    INTEGER NOT NULL DEFAULT 0,
	egress_bytes_per_min INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS ns_consumption (
	ns                TEXT PRIMARY KEY,
	active_jobs       INTEGER NOT NULL DEFAULT 0,
	cpu_used          INTEGER NOT NULL DEFAULT 0,
	memMB_used        INTEGER NOT NULL DEFAULT 0,
	llm_calls_1min    INTEGER NOT NULL DEFAULT 0,
	egress_bytes_1min INTEGER NOT NULL DEFAULT 0,
	last_reset_ts     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_consumption_ns ON ns_consumption(ns);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("quotas: migrate: %w", err)
	}
	return nil
}

// ErrNotFound indicates the quota record was not found.
var ErrNotFound = errors.New("quotas: not found")

// ErrInvalidQuotaLimit indicates one or more quota limits are invalid.
var ErrInvalidQuotaLimit = errors.New("quotas: limit cannot be negative")

// ErrNegativeConsumption indicates a consumption update would make usage counters negative.
var ErrNegativeConsumption = errors.New("quotas: consumption cannot be negative")
