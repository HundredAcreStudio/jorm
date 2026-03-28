package errors

import (
	stderrors "errors"
	"strings"
)

// ErrorKind classifies the type of error encountered during a pipeline run.
type ErrorKind string

const (
	ErrKindRateLimit ErrorKind = "rate_limit"
	ErrKindAuth      ErrorKind = "auth"
	ErrKindNetwork   ErrorKind = "network"
	ErrKindCrash     ErrorKind = "crash"
	ErrKindShell     ErrorKind = "shell"
	ErrKindUnknown   ErrorKind = "unknown"
)

// ClassifiedError wraps an error with a human-readable classification.
type ClassifiedError struct {
	Kind    ErrorKind
	Msg     string
	Hint    string
	Wrapped error
}

func (e *ClassifiedError) Error() string {
	return e.Msg
}

func (e *ClassifiedError) Unwrap() error {
	return e.Wrapped
}

// IsResumable returns true if the error kind is likely to succeed on retry.
func (e *ClassifiedError) IsResumable() bool {
	return e.Kind == ErrKindRateLimit || e.Kind == ErrKindCrash
}

// ClassifyError inspects an error message and returns a ClassifiedError with
// a human-readable Msg, Kind, and optional Hint.
func ClassifyError(err error) *ClassifiedError {
	if err == nil {
		return nil
	}

	// If the error chain already contains a ClassifiedError, return it
	// rather than re-classifying from the string representation.
	var ce *ClassifiedError
	if stderrors.As(err, &ce) {
		return ce
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	// Rate limit detection takes priority.
	// Claude CLI outputs "You've hit your limit · resets Xpm" on rate limit.
	if strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "you've hit your limit") ||
		strings.Contains(lower, "resets ") {
		return &ClassifiedError{
			Kind:    ErrKindRateLimit,
			Msg:     "claude rate limit reached",
			Hint:    "run `jorm resume <run-id>` to retry from the failed stage",
			Wrapped: err,
		}
	}

	// Auth failures
	if strings.Contains(lower, "authentication") || strings.Contains(lower, "unauthorized") {
		return &ClassifiedError{
			Kind:    ErrKindAuth,
			Msg:     "authentication failed",
			Wrapped: err,
		}
	}

	// Network errors
	if strings.Contains(lower, "connection refused") || strings.Contains(lower, "timed out") || strings.Contains(lower, "timeout") {
		return &ClassifiedError{
			Kind:    ErrKindNetwork,
			Msg:     "network error",
			Wrapped: err,
		}
	}

	// Claude process crash (exit status without rate limit)
	if strings.Contains(lower, "claude exited") && strings.Contains(lower, "exit status") {
		return &ClassifiedError{
			Kind:    ErrKindCrash,
			Msg:     "claude process crashed",
			Hint:    "run `jorm resume <run-id>` to retry from the failed stage",
			Wrapped: err,
		}
	}

	// Generic exit status (shell command failure)
	if strings.Contains(lower, "exit status") {
		return &ClassifiedError{
			Kind:    ErrKindShell,
			Msg:     "shell command failed",
			Wrapped: err,
		}
	}

	return &ClassifiedError{
		Kind:    ErrKindUnknown,
		Msg:     msg,
		Wrapped: err,
	}
}
