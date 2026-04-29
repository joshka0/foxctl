package contextengine

import "fmt"

// Validator is implemented by types that can self-validate.
type Validator interface {
	Validate() error
}

// ValidateAll runs validation on a collection of entities.
// It returns the first error encountered.
func ValidateAll(entities ...any) error {
	for i, e := range entities {
		if v, ok := e.(Validator); ok {
			if err := v.Validate(); err != nil {
				return fmt.Errorf("validate[%d]: %w", i, err)
			}
		}
	}
	return nil
}
