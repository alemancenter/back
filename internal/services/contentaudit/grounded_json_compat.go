package contentaudit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Together-compatible structured outputs are usually schema-correct, but some
// models still collapse single-item arrays to scalars or encode integer-valued
// fields as JSON decimals/strings (for example confidence: 1.0). The grounded
// repair pipeline remains strict about evidence and claim validation; these
// helpers only normalize semantically equivalent JSON shapes before the
// existing validation gates.
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

// decodeGroundedInt accepts canonical JSON integers plus integer-valued decimal
// numbers/strings such as 1.0, 98.0, "7", or "7.0". Fractional values such as
// 98.5 remain invalid; we never round model output silently.
func decodeGroundedInt(raw json.RawMessage) (int, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, nil
	}

	text := string(raw)
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}

	rat, ok := new(big.Rat).SetString(text)
	if !ok {
		return 0, fmt.Errorf("invalid integer value %q", text)
	}
	if !rat.IsInt() {
		return 0, fmt.Errorf("fractional value %q is not an integer", text)
	}
	if !rat.Num().IsInt64() {
		return 0, fmt.Errorf("integer value %q is out of range", text)
	}

	value := rat.Num().Int64()
	if strconv.IntSize == 32 && (value < -1<<31 || value > 1<<31-1) {
		return 0, fmt.Errorf("integer value %q is out of range", text)
	}
	return int(value), nil
}

func decodeGroundedIntList(raw json.RawMessage) ([]int, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		ints := make([]int, 0, len(items))
		for _, item := range items {
			n, err := decodeGroundedInt(item)
			if err != nil {
				return nil, err
			}
			ints = append(ints, n)
		}
		return ints, nil
	}

	n, err := decodeGroundedInt(raw)
	if err != nil {
		return nil, err
	}
	return []int{n}, nil
}

// decodeGroundedClaimCount keeps the validator's internal contract numeric while
// tolerating a common provider shape drift: several models return supported_claims
// as an array containing the supported claim texts instead of the requested integer
// count. In that case the array itself is semantically richer than the count, so we
// normalize it to the number of distinct non-empty strings. We deliberately reject
// objects, booleans and non-numeric scalar strings rather than guessing their meaning.
func decodeGroundedClaimCount(raw json.RawMessage) (int, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, nil
	}
	if raw[0] != '[' {
		return decodeGroundedInt(raw)
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	count := 0
	for _, item := range items {
		var claim string
		if err := json.Unmarshal(item, &claim); err != nil {
			return 0, fmt.Errorf("supported claim array item must be a string: %w", err)
		}
		claim = strings.TrimSpace(claim)
		if claim == "" || seen[claim] {
			continue
		}
		seen[claim] = true
		count++
	}
	return count, nil
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
		Confidence  json.RawMessage `json:"confidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	evidenceIDs, err := decodeGroundedStringList(raw.EvidenceIDs)
	if err != nil {
		return fmt.Errorf("evidence_ids: %w", err)
	}
	confidence, err := decodeGroundedInt(raw.Confidence)
	if err != nil {
		return fmt.Errorf("confidence: %w", err)
	}
	*fact = groundedFact{Claim: raw.Claim, EvidenceIDs: evidenceIDs, Confidence: confidence}
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
		GroundingScore    json.RawMessage `json:"grounding_score"`
		SupportedClaims   json.RawMessage `json:"supported_claims"`
		UnsupportedClaims json.RawMessage `json:"unsupported_claims"`
		Notes             json.RawMessage `json:"notes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	groundingScore, err := decodeGroundedInt(raw.GroundingScore)
	if err != nil {
		return fmt.Errorf("grounding_score: %w", err)
	}
	supportedClaims, err := decodeGroundedClaimCount(raw.SupportedClaims)
	if err != nil {
		return fmt.Errorf("supported_claims: %w", err)
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
		GroundingScore:    groundingScore,
		SupportedClaims:   supportedClaims,
		UnsupportedClaims: unsupported,
		Notes:             notes,
	}
	return nil
}
