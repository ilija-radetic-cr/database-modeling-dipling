package workspace

import "errors"

// ErrRequirementDecisionsNeeded stops conceptual modeling before any provider
// call when deterministic requirement readiness is not satisfied.
var ErrRequirementDecisionsNeeded = errors.New("requirements need your decision before conceptual modeling")
