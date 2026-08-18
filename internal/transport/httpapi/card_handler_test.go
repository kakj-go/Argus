package httpapi

import (
	"context"
	"net/http"
	"testing"

	actionservice "github.com/kakj-go/Argus/internal/action"
)

func TestCardActionErrorsHaveStablePublicMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{actionservice.ErrBindingConsumed, http.StatusConflict, "CARD_BINDING_CONSUMED"},
		{actionservice.ErrBindingExpired, http.StatusGone, "CARD_BINDING_EXPIRED"},
		{actionservice.ErrInvalidated, http.StatusConflict, "CARD_ACTION_INVALIDATED"},
	}
	for _, test := range tests {
		if got := cardStatus(test.err); got != test.status {
			t.Fatalf("cardStatus(%v) = %d, want %d", test.err, got, test.status)
		}
		if got := cardError(context.Background(), test.err).Code; got != test.code {
			t.Fatalf("cardError(%v) = %q, want %q", test.err, got, test.code)
		}
	}
}
