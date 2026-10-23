// Package httpx owns the HTTP envelope: how success and failure are written,
// the stable error identities clients branch on, and the message catalog.
//
// An error response does three jobs and each has its own field:
//
//	reason   - stable snake_case identity; the contract clients switch on
//	message  - resolved human text from the catalog; what a person reads
//	severity - a coarse band for dashboards; never used for branching
package httpx

import (
	"fmt"
	"net/http"
)

// Reason is a stable error identity. Renaming one is a breaking change.
type Reason string

// Every identity the service can return. New ones are added here, never as
// inline strings.
const (
	ReasonValidation          Reason = "validation_error"
	ReasonInvalidCredentials  Reason = "invalid_credentials"
	ReasonTokenInvalid        Reason = "token_invalid"
	ReasonNotFound            Reason = "not_found"
	ReasonAlreadyExists       Reason = "already_exists"
	ReasonVersionConflict     Reason = "version_conflict"
	ReasonIdempotencyMismatch Reason = "idempotency_mismatch"
	ReasonPayloadTooLarge     Reason = "payload_too_large"
	ReasonRateLimited         Reason = "rate_limited"
	ReasonChaos               Reason = "chaos_injected"
	ReasonInternal            Reason = "internal_error"
)

// Severity answers "should this page someone?", not "what went wrong?".
type Severity int

// Bands, roughly ordered by who is at fault.
const (
	SeverityBodyValidation       Severity = 1
	SeveritySuspiciousValidation Severity = 2
	SeverityAuth                 Severity = 3
	SeverityForbidden            Severity = 4
	SeverityNotFound             Severity = 5
	SeverityConflict             Severity = 6
	SeverityBusinessRule         Severity = 7
	SeverityRateLimited          Severity = 8
	SeverityUpstream             Severity = 9
	SeverityServerFault          Severity = 10
)

// severityFor maps an identity to its band. Coarse by design.
func severityFor(r Reason) Severity {
	switch r {
	case ReasonValidation, ReasonPayloadTooLarge:
		return SeverityBodyValidation
	case ReasonIdempotencyMismatch:
		return SeveritySuspiciousValidation
	case ReasonInvalidCredentials, ReasonTokenInvalid:
		return SeverityAuth
	case ReasonNotFound:
		return SeverityNotFound
	case ReasonAlreadyExists, ReasonVersionConflict:
		return SeverityConflict
	case ReasonRateLimited:
		return SeverityRateLimited
	case ReasonChaos:
		return SeverityUpstream
	default:
		return SeverityServerFault
	}
}

// DomainError is a domain rejection returned as a value from a service. It is
// never panicked. Fail renders it; nothing else inspects its fields.
type DomainError struct {
	Reason      Reason
	MessageKey  MessageKey
	Status      int
	FieldErrors map[string][]string
	// Data is extra payload the client needs to recover, for example the
	// current record on a version conflict.
	Data any
	// RetryAfter, in seconds, is set only for rate limiting.
	RetryAfter int
	// Diagnostic is operator-facing detail. It is logged, never served.
	Diagnostic string
}

func (e *DomainError) Error() string {
	if e.Diagnostic != "" {
		return fmt.Sprintf("%s: %s", e.Reason, e.Diagnostic)
	}
	return string(e.Reason)
}

// Domain builds a DomainError with the given identity, message and status.
func Domain(r Reason, key MessageKey, status int) *DomainError {
	return &DomainError{Reason: r, MessageKey: key, Status: status}
}

// WithData attaches recovery data to the error.
func (e *DomainError) WithData(d any) *DomainError { e.Data = d; return e }

// WithDiagnostic attaches operator-facing detail to the error.
func (e *DomainError) WithDiagnostic(d string) *DomainError { e.Diagnostic = d; return e }

// Common constructors, so call sites do not repeat status codes.

// NotFound is the standard not_found error.
func NotFound(key MessageKey) *DomainError {
	return Domain(ReasonNotFound, key, http.StatusNotFound)
}

// Validation is the standard validation_error with per-field detail.
func Validation(fields map[string][]string) *DomainError {
	e := Domain(ReasonValidation, MsgValidation, http.StatusBadRequest)
	e.FieldErrors = fields
	return e
}
