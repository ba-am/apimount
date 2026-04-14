package core

import "fmt"

// Kind classifies the category of a core error.
type Kind int

const (
	KindValidation Kind = iota // request body failed schema validation
	KindAuth                   // authentication / authorization failure
	KindNetwork                // network-level failure
	KindUpstream               // upstream API returned an error status
	KindPolicy                 // RBAC / policy rejection
	KindInternal               // internal / programming error
)

func (k Kind) String() string {
	switch k {
	case KindValidation:
		return "validation"
	case KindAuth:
		return "auth"
	case KindNetwork:
		return "network"
	case KindUpstream:
		return "upstream"
	case KindPolicy:
		return "policy"
	case KindInternal:
		return "internal"
	}
	return "unknown"
}

// Error is the canonical error type that all core components and frontends use.
// Frontends translate it into their native surface (errno, HTTP status, MCP error).
type Error struct {
	Kind    Kind
	Code    string // stable machine-readable code, e.g. "upstream.not_found"
	Message string // human-readable description
	OpID    string // operationId that triggered the error, if applicable
	Status  int    // upstream HTTP status code, 0 if not applicable
	Cause   error  // underlying error, may be nil
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// New returns a new Error.
func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

// Wrap wraps an existing error into a core.Error.
func Wrap(kind Kind, code, message string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Message: message, Cause: cause}
}
