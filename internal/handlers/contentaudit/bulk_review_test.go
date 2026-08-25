package contentaudit

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
)

func TestNormalizeBulkFixReviewRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     bulkFixReviewRequest
		wantIDs []uint64
		wantErr bool
	}{
		{name: "apply deduplicates while preserving order", req: bulkFixReviewRequest{Action: " APPLY ", FixPreviewIDs: []uint64{7, 3, 7}}, wantIDs: []uint64{7, 3}},
		{name: "reject is accepted", req: bulkFixReviewRequest{Action: "reject", FixPreviewIDs: []uint64{9}}, wantIDs: []uint64{9}},
		{name: "requires selection", req: bulkFixReviewRequest{Action: "apply"}, wantErr: true},
		{name: "rejects unknown action", req: bulkFixReviewRequest{Action: "delete", FixPreviewIDs: []uint64{1}}, wantErr: true},
		{name: "rejects zero id", req: bulkFixReviewRequest{Action: "reject", FixPreviewIDs: []uint64{1, 0}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBulkFixReviewRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error state: %v", err)
			}
			if tt.wantErr {
				return
			}
			if len(got.FixPreviewIDs) != len(tt.wantIDs) {
				t.Fatalf("got ids %v, want %v", got.FixPreviewIDs, tt.wantIDs)
			}
			for i := range tt.wantIDs {
				if got.FixPreviewIDs[i] != tt.wantIDs[i] {
					t.Fatalf("got ids %v, want %v", got.FixPreviewIDs, tt.wantIDs)
				}
			}
		})
	}
}

func TestNormalizeBulkFixReviewRequestLimitsBatch(t *testing.T) {
	ids := make([]uint64, maxBulkFixReviews+1)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	if _, err := normalizeBulkFixReviewRequest(bulkFixReviewRequest{Action: "apply", FixPreviewIDs: ids}); err == nil {
		t.Fatal("expected oversized batch to be rejected")
	}
}

func TestBulkFixReviewTargetKeySeparatesCountryAndType(t *testing.T) {
	article := &models.ContentAIFixPreview{CountryCode: "JO", ContentType: "Article", ContentID: "jo:12"}
	post := &models.ContentAIFixPreview{CountryCode: "jo", ContentType: "post", ContentID: "jo:12"}
	otherCountry := &models.ContentAIFixPreview{CountryCode: "sa", ContentType: "article", ContentID: "sa:12"}
	if bulkFixReviewTargetKey(article) == bulkFixReviewTargetKey(post) {
		t.Fatal("article and post targets must not collide")
	}
	if bulkFixReviewTargetKey(article) == bulkFixReviewTargetKey(otherCountry) {
		t.Fatal("targets in different countries must not collide")
	}
}
