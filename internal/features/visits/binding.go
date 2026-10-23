package visits

import (
	"sync"

	"github.com/go-playground/validator/v10"
)

// binding returns the validator used when a body was pre-read by the
// idempotency middleware. It is the same engine Gin uses, so the rules match.
var binding = sync.OnceValue(func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.SetTagName("binding")
	return v
})
