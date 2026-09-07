package contentaudit

import (
	"github.com/imanjo/fiber-api/internal/database"
	"testing"
)

func TestQualityBatchCountryBinding(t *testing.T) {
	for _, id := range []database.CountryID{1, 2, 3, 4} {
		req, err := bindQualityBatchCountry(contentQualityBatchRequest{Limit: 100}, id)
		if err != nil || req.CountryCode != database.CountryCode(id) || req.Limit != 100 {
			t.Fatalf("wrong request context: %+v, %v", req, err)
		}
	}
	if _, err := bindQualityBatchCountry(contentQualityBatchRequest{CountryCode: "jo"}, database.CountrySaudi); err == nil {
		t.Fatal("cross-country write accepted")
	}
}
