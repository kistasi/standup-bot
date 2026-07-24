package main

import (
	"strings"
	"testing"
)

// Only the rejection branch is exercised here: the other two reach out to
// GitHub and Anthropic, so they belong to the internal packages' tests.
func TestDispatch_UnknownCommand(t *testing.T) {
	err := dispatch([]string{"weekly"})
	if err == nil {
		t.Fatal("dispatch() error = nil, want an error for an unknown command")
	}
	if !strings.Contains(err.Error(), `"weekly"`) || !strings.Contains(err.Error(), "usage") {
		t.Errorf("dispatch() error = %v, want it to name the command and show usage", err)
	}
}
