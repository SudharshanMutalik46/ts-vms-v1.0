package cameras

import (
	"fmt"
)

// SfuStepError wraps an error with a specific step and error code.
// This allows the API handler to return structured JSON errors.
type SfuStepError struct {
	Step           string
	ErrorCode      string
	SafeMessage    string
	RequiredAction string
	FallbackHint   bool
	FallbackURL    string
	Err            error
}

func (e *SfuStepError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Step, e.ErrorCode, e.SafeMessage, e.Err)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Step, e.ErrorCode, e.SafeMessage)
}

func (e *SfuStepError) Unwrap() error {
	return e.Err
}

// NewSfuError creates a new SfuStepError
func NewSfuError(step, code, msg string, err error) *SfuStepError {
	return &SfuStepError{
		Step:        step,
		ErrorCode:   code,
		SafeMessage: msg,
		Err:         err,
	}
}

// NewSfuErrorWithFallback creates a new SfuStepError with fallback hint and URL
func NewSfuErrorWithFallback(step, code, msg, url string, err error) *SfuStepError {
	return &SfuStepError{
		Step:         step,
		ErrorCode:    code,
		SafeMessage:  msg,
		FallbackHint: true,
		FallbackURL:  url,
		Err:          err,
	}
}
