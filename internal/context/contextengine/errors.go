package contextengine

import "errors"

var (
	// ErrInvalidClaimStatus marks unknown claim statuses.
	ErrInvalidClaimStatus = errors.New("contextengine: invalid claim status")
	// ErrInvalidStalenessStatus marks unknown staleness statuses.
	ErrInvalidStalenessStatus = errors.New("contextengine: invalid staleness status")
	// ErrInvalidTransition marks disallowed lifecycle transitions.
	ErrInvalidTransition = errors.New("contextengine: invalid transition")
)
