package target

import "testing"

// TestRetryErrorMessage checks a RetryError reads as its reason.
// It asserts Error() returns the reason.
func TestRetryErrorMessage(t *testing.T) {
	var err error = RetryError{Reason: "skill not loaded: x"}
	if err.Error() != "skill not loaded: x" {
		t.Errorf("err = %q", err)
	}
}
