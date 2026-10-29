package catalog

import "testing"

func TestUUIDValidation(t *testing.T) {
	good := []string{
		"00000000-0000-0000-0000-000000000000",
		"2f8388a0-b65d-4cce-87c5-be3f923ba168",
	}
	for _, id := range good {
		if !validID(id) {
			t.Errorf("valid UUID rejected: %s", id)
		}
	}
	bad := []string{
		"", "not-a-uuid", "2f8388a0-b65d-4cce-87c5-be3f923ba168-1234",
		"2f8388a0-b65d-4cce-87c5-be3f923ba16z",
	}
	for _, id := range bad {
		if validID(id) {
			t.Errorf("invalid UUID accepted: %q", id)
		}
	}
}

func TestImageURLs(t *testing.T) {
	for _, u := range []string{"", "https://cdn.example.com/meal.jpg", "https://images.example.org/x?a=1"} {
		if !validImage(u) { t.Errorf("safe image URL rejected: %q",u) }
	}
	for _, u := range []string{"javascript:alert(1)", "http://example.org/a.png", "file:///etc/passwd", "https://user:pass@example.org/x", "ftp://example.org"} {
		if validImage(u) { t.Errorf("unsafe image URL accepted: %q",u) }
	}
}
