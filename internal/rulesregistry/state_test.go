package rulesregistry

import "testing"

func TestDeriveReadinessStateNotIndexableIsAlwaysCriticalBlocker(t *testing.T) {
	if got := DeriveReadinessState(false, nil); got != StateCriticalBlocker {
		t.Fatalf("expected StateCriticalBlocker, got %s", got)
	}
	// Even an editorial-standard match must not override a non-indexable gate.
	if got := DeriveReadinessState(false, []string{"thin_content"}); got != StateCriticalBlocker {
		t.Fatalf("expected StateCriticalBlocker, got %s", got)
	}
}

func TestDeriveReadinessStateOfficialRequirementWins(t *testing.T) {
	got := DeriveReadinessState(true, []string{"thin_content", "policy_blocked"})
	if got != StateCriticalBlocker {
		t.Fatalf("expected StateCriticalBlocker (policy_blocked is official), got %s", got)
	}
}

func TestDeriveReadinessStateEditorialStandardIsNeedsImprovement(t *testing.T) {
	got := DeriveReadinessState(true, []string{"thin_content", "meta_description"})
	if got != StateNeedsImprovement {
		t.Fatalf("expected StateNeedsImprovement, got %s", got)
	}
}

func TestDeriveReadinessStateOptimizationOnlyIsInternallyReady(t *testing.T) {
	got := DeriveReadinessState(true, []string{"short_title"})
	if got != StateInternallyReady {
		t.Fatalf("expected StateInternallyReady (short_title is optimization-only), got %s", got)
	}
}

func TestDeriveReadinessStateNoMatchesIsInternallyReady(t *testing.T) {
	if got := DeriveReadinessState(true, nil); got != StateInternallyReady {
		t.Fatalf("expected StateInternallyReady, got %s", got)
	}
}

func TestDeriveReadinessStateUnknownCodeIsIgnored(t *testing.T) {
	got := DeriveReadinessState(true, []string{"not_a_real_code"})
	if got != StateInternallyReady {
		t.Fatalf("expected unknown codes to be ignored, got %s", got)
	}
}
