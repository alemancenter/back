package models

import (
	"reflect"
	"strings"
	"testing"
)

func TestVisitorTrackingUserAgentTextHasNoDefault(t *testing.T) {
	field, ok := reflect.TypeOf(VisitorTracking{}).FieldByName("UserAgent")
	if !ok {
		t.Fatal("UserAgent field not found")
	}

	gormTag := field.Tag.Get("gorm")
	if strings.Contains(gormTag, "default") {
		t.Fatalf("user_agent is TEXT and must not declare a default in MySQL: %q", gormTag)
	}
}
