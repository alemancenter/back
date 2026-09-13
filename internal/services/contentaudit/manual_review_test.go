package contentaudit

import (
	"context"
	"errors"
	"testing"
)

func TestGeneratedTextCannotBeAppliedWithoutReviewer(t *testing.T) {
	// A nil repository ensures rejected calls never reach reads or writes.
	svc := &Service{}
	zero := uint(0)
	for _, reviewer := range []*uint{nil, &zero} {
		if _, err := svc.ApplyGroundedFix(context.Background(), 1, reviewer, ""); !errors.Is(err, ErrHumanReviewRequired) {
			t.Fatalf("grounded apply: %v", err)
		}
		if _, err := svc.ApplyFix(context.Background(), 1, reviewer, ""); !errors.Is(err, ErrHumanReviewRequired) {
			t.Fatalf("legacy apply: %v", err)
		}
		if _, err := svc.applySafeMetadataFix(context.Background(), nil, reviewer, ""); !errors.Is(err, ErrHumanReviewRequired) {
			t.Fatalf("metadata apply: %v", err)
		}
	}
}
