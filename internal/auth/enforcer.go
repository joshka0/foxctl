package auth

import (
	_ "embed"
	"errors"
	"fmt"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	"github.com/casbin/casbin/v2/util"
	"github.com/jkatigb/agentctl/internal/domain/identity"
)

//go:embed model.conf
var modelConf string

var errPostgresAdapterNotImplemented = errors.New("postgres casbin adapter is not implemented")

// NewEnforcer creates a Casbin enforcer with the embedded RBAC model and
// in-memory CSV policy adapter.
func NewEnforcer(policyCSV string) (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(modelConf)
	if err != nil {
		return nil, fmt.Errorf("parse casbin model: %w", err)
	}

	e, err := casbin.NewEnforcer(m, stringadapter.NewAdapter(policyCSV))
	if err != nil {
		return nil, fmt.Errorf("create casbin enforcer: %w", err)
	}

	// Enable wildcard domain matching so group definitions with domain "*"
	// apply across all tenants.
	e.AddNamedDomainMatchingFunc("g", "KeyMatch", util.KeyMatch)

	return e, nil
}

// NewPostgresEnforcer creates a Casbin enforcer backed by Postgres policy
// storage.
//
// The Postgres adapter wiring is a follow-up; this placeholder keeps the API
// stable without introducing the gorm adapter dependency yet.
func NewPostgresEnforcer(_ string) (*casbin.Enforcer, error) {
	return nil, errPostgresAdapterNotImplemented
}

// Enforce checks whether the principal can perform action on resource.
//
// Canonical subject resolution is delegated to Principal.Subject(). Anonymous
// principals and nil enforcers are treated as pass-through for backward
// compatibility with single-tenant mode.
func Enforce(e *casbin.Enforcer, principal identity.Principal, resource, action string) (bool, error) {
	if e == nil || principal.IsAnonymous() {
		return true, nil
	}

	tenant := principal.TenantID
	if tenant == "" {
		tenant = "*"
	}

	allowed, err := e.Enforce(principal.Subject(), tenant, resource, action)
	if err != nil {
		return false, fmt.Errorf("enforce policy: %w", err)
	}

	return allowed, nil
}
