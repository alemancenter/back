package contentaudit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Together-compatible structured outputs are usually schema-correct, but some
// models still collapse single-item arrays to scalars (for example
// source_notes: "..." instead of source_notes: ["..."]). The grounded repair
// pipeline remains strict about evidence and claim validation; these helpers
// only normalize equivalent JSON shapes before the existing validation gates.
func decodeGroundedStringList(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	var list []string
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		return list, nil
	}

	var single string
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, err
	}
	single = strings.TrimSpace(single)
	if single == "" {
		return nil, nil
	}
	return []string{single}, nil
}

func decodeGroundedIntList(raw json.RawMessage) ([]int, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	var ints []int
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &ints); err == nil {
			return ints, nil
		}
		var stringsList []string
		if err := json.Unmarshal(raw, &stringsList); err != nil {
			return nil, err
		}
		for _, value := range stringsList {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("invalid integer list item %q: %w", value, err)
			}
			ints = append(ints, n)
		}
		return ints, nil
	}

	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return []int{n}, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(single))
	if err != nil {
		return nil, fmt.Errorf("invalid integer value %q: %w", single, err)
	}
	return []int{n}, nil
}

func decodeGroundedBool(raw json.RawMessage) (bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return false, err
	}
	value, err := strconv.ParseBool(strings.TrimSpace(text))
	if err != nil {
		return false, err
	}
	return value, nil
}

func (fact *groundedFact) UnmarshalJSON(data []byte) error {
	var raw struct {
		Claim       string          `json:"claim"`
		EvidenceIDs json.RawMessage `json:"evidence_ids"`
		Confidence  int             `json:"confidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	evidenceIDs, err := decodeGroundedStringList(raw.EvidenceIDs)
	if err != nil {
		return fmt.Errorf("evidence_ids: %w", err)
	}
	*fact = groundedFact{Claim: raw.Claim, EvidenceIDs: evidenceIDs, Confidence: raw.Confidence}
	return nil
}

func (result *groundedFactExtraction) UnmarshalJSON(data []byte) error {
	var raw struct {
		Purpose            string          `json:"purpose"`
		Audience           json.RawMessage `json:"audience"`
		Facts              []groundedFact  `json:"facts"`
		InsufficientSource json.RawMessage `json:"insufficient_source"`
		SourceNotes        json.RawMessage `json:"source_notes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	audience, err := decodeGroundedStringList(raw.Audience)
	if err != nil {
		return fmt.Errorf("audience: %w", err)
	}
	sourceNotes, err := decodeGroundedStringList(raw.SourceNotes)
	if err != nil {
		return fmt.Errorf("source_notes: %w", err)
	}
	insufficient, err := decodeGroundedBool(raw.InsufficientSource)
	if err != nil {
		return fmt.Errorf("insufficient_source: %w", err)
	}
	*result = groundedFactExtraction{
		Purpose:            raw.Purpose,
		Audience:           audience,
		Facts:              raw.Facts,
		InsufficientSource: insufficient,
		SourceNotes:        sourceNotes,
	}
	return nil
}

func (draft *groundedDraft) UnmarshalJSON(data []byte) error {
	var raw struct {
		Title           string          `json:"title"`
		ContentHTML     string          `json:"content_html"`
		UsedFactIndexes json.RawMessage `json:"used_fact_indexes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	indexes, err := decodeGroundedIntList(raw.UsedFactIndexes)
	if err != nil {
		return fmt.Errorf("used_fact_indexes: %w", err)
	}
	*draft = groundedDraft{Title: raw.Title, ContentHTML: raw.ContentHTML, UsedFactIndexes: indexes}
	return nil
}

func (validation *groundedValidation) UnmarshalJSON(data []byte) error {
	var raw struct {
		GroundingScore    int             `json:"grounding_score"`
		SupportedClaims   int             `json:"supported_claims"`
		UnsupportedClaims json.RawMessage `json:"unsupported_claims"`
		Notes             json.RawMessage `json:"notes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	unsupported, err := decodeGroundedStringList(raw.UnsupportedClaims)
	if err != nil {
		return fmt.Errorf("unsupported_claims: %w", err)
	}
	notes, err := decodeGroundedStringList(raw.Notes)
	if err != nil {
		return fmt.Errorf("notes: %w", err)
	}
	*validation = groundedValidation{
		GroundingScore:    raw.GroundingScore,
		SupportedClaims:   raw.SupportedClaims,
		UnsupportedClaims: unsupported,
		Notes:             notes,
	}
	return nil
}
