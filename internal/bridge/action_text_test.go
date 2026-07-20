package bridge

import (
	"context"
	"strings"
	"testing"
)

func TestActionPressRejectsChordSyntaxBeforeDispatch(t *testing.T) {
	for _, key := range []string{"Control+A", "ctrl+shift+p"} {
		_, err := (&Bridge{}).actionPress(context.Background(), ActionRequest{Key: key})
		if err == nil {
			t.Fatalf("actionPress(%q) error = nil, want unsupported chord error", key)
		}
		if !strings.Contains(err.Error(), "unsupported key chord") || !strings.Contains(err.Error(), "Ctrl-style modifiers are not supported") {
			t.Fatalf("actionPress(%q) error = %q, want precise chord guidance", key, err)
		}
	}
}
