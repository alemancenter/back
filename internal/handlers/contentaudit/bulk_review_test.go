package contentaudit

import "testing"

func TestNormalizeBulkFixReviewRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     bulkFixReviewRequest
		wantIDs []uint64
		wantErr bool
	}{
		{name: "reject deduplicates while preserving order", req: bulkFixReviewRequest{Action: " REJECT ", FixPreviewIDs: []uint64{7, 3, 7}}, wantIDs: []uint64{7, 3}},
		{name: "bulk apply cannot publish", req: bulkFixReviewRequest{Action: " APPLY ", FixPreviewIDs: []uint64{7}}, wantErr: true},
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
	if _, err := normalizeBulkFixReviewRequest(bulkFixReviewRequest{Action: "reject", FixPreviewIDs: ids}); err == nil {
		t.Fatal("expected oversized batch to be rejected")
	}
}
