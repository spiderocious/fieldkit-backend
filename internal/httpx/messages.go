package httpx

// MessageKey names a human-readable message in the catalog. Handlers and
// services never embed display text; they reference a key.
type MessageKey string

// Keys, sliced by feature.
const (
	MsgValidation          MessageKey = "common.validation"
	MsgInternal            MessageKey = "common.internal"
	MsgRateLimited         MessageKey = "common.rate_limited"
	MsgChaos               MessageKey = "common.chaos"
	MsgIdempotencyMismatch MessageKey = "common.idempotency_mismatch"
	MsgTokenInvalid        MessageKey = "auth.token_invalid"
	MsgInvalidCredentials  MessageKey = "auth.invalid_credentials"
	MsgVisitNotFound       MessageKey = "visits.not_found"
	MsgVisitExists         MessageKey = "visits.already_exists"
	MsgVisitConflict       MessageKey = "visits.version_conflict"
	MsgPhotoTooLarge       MessageKey = "photos.too_large"
	MsgPhotoNotFound       MessageKey = "photos.not_found"
)

// catalog is the one place display copy lives. A missing key is a bug, so the
// fallback is the key itself, which is visibly wrong in a UI.
var catalog = map[MessageKey]string{
	MsgValidation:          "Some fields need attention.",
	MsgInternal:            "Something went wrong on our side. Please try again.",
	MsgRateLimited:         "Too many requests. Please wait a moment.",
	MsgChaos:               "The server is deliberately failing for the demo.",
	MsgIdempotencyMismatch: "This request was already made with different details.",
	MsgTokenInvalid:        "Your session has expired. Please sign in again.",
	MsgInvalidCredentials:  "Phone number or PIN is incorrect.",
	MsgVisitNotFound:       "That visit could not be found.",
	MsgVisitExists:         "This visit has already been submitted.",
	MsgVisitConflict:       "This visit was changed by someone else. Review the latest version.",
	MsgPhotoTooLarge:       "Photos must be under 2 MB.",
	MsgPhotoNotFound:       "That photo could not be found.",
}

// Message resolves a key to display text.
func Message(k MessageKey) string {
	if m, ok := catalog[k]; ok {
		return m
	}
	return string(k)
}
