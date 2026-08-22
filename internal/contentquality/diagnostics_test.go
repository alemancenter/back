package contentquality

import "testing"

func TestEvaluateDiagnosticsProducesSignalsOnly(t *testing.T) {
	d := EvaluateDiagnostics("قصير", "كلمات قليلة فقط", "", 0, false)
	if d.WordCount == 0 {
		t.Fatal("expected word count")
	}
	if len(d.Signals) < 4 {
		t.Fatalf("expected multiple diagnostic signals, got %#v", d.Signals)
	}
}

func TestDiagnosticsThresholdsDoNotChangeGate(t *testing.T) {
	gate := Unaudited()
	_ = EvaluateDiagnostics("قصير", "قصير", "", 0, true)
	if !gate.Indexable {
		t.Fatal("editorial diagnostics must not change indexing")
	}
	if gate.AdsEligible {
		t.Fatal("unaudited gate must remain ad-ineligible")
	}
}
