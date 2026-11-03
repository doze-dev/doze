package fault

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestOf(t *testing.T) {
	base := errors.New("x")
	for _, tc := range []struct {
		err  error
		want Kind
	}{
		{nil, ""},
		{base, Failed},
		{With(NotFound, base), NotFound},
		{fmt.Errorf("wrapped: %w", With(Usage, base)), Usage},
		{With(Invalid, With(NotFound, base)), Invalid}, // the outermost kind wins
		{fmt.Errorf("boot: %w", context.DeadlineExceeded), Timeout},
		{context.Canceled, Interrupted},
	} {
		if got := Of(tc.err); got != tc.want {
			t.Errorf("Of(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
	if With(NotFound, nil) != nil {
		t.Error("With(kind, nil) must stay nil")
	}
	if err := With(NotFound, base); !errors.Is(err, base) || err.Error() != "x" {
		t.Errorf("With must keep the error and its message: %v", err)
	}
}
