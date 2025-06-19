package datetime

import (
	"testing"
	"time"
)

func TestDateTimeFunctions(t *testing.T) {
	// Test basic time formatting
	now := time.Now()
	formatted := now.Format(time.RFC3339)

	if formatted == "" {
		t.Fatal("Expected non-empty formatted time")
	}

	// Test parsing
	parsed, err := time.Parse(time.RFC3339, formatted)
	if err != nil {
		t.Fatalf("Failed to parse time: %v", err)
	}

	if parsed.Unix() != now.Unix() {
		t.Fatal("Parsed time doesn't match original")
	}
}

func TestTimeFormats(t *testing.T) {
	testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Test RFC3339 format
	rfc3339 := testTime.Format(time.RFC3339)
	expected := "2024-01-01T12:00:00Z"
	if rfc3339 != expected {
		t.Errorf("Expected RFC3339 format '%s', got '%s'", expected, rfc3339)
	}

	// Test RFC822 format
	rfc822 := testTime.Format(time.RFC822)
	if rfc822 == "" {
		t.Error("Expected non-empty RFC822 format")
	}

	// Test custom format
	custom := testTime.Format("2006-01-02 15:04:05")
	expected = "2024-01-01 12:00:00"
	if custom != expected {
		t.Errorf("Expected custom format '%s', got '%s'", expected, custom)
	}
}

func TestTimezoneParsing(t *testing.T) {
	// Test loading different timezones
	timezones := []string{
		"UTC",
		"America/New_York",
		"Europe/London",
		"Asia/Tokyo",
	}

	for _, tz := range timezones {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Errorf("Failed to load timezone '%s': %v", tz, err)
			continue
		}

		if loc == nil {
			t.Errorf("Expected non-nil location for timezone '%s'", tz)
		}
	}
}

func TestTimeParsingFormats(t *testing.T) {
	testCases := []struct {
		input    string
		format   string
		expected bool
	}{
		{"2024-01-01T12:00:00Z", time.RFC3339, true},
		{"2024-01-01 12:00:00", "2006-01-02 15:04:05", true},
		{"01 Jan 24 12:00 UTC", time.RFC822, true},
		{"invalid-time", time.RFC3339, false},
	}

	for _, tc := range testCases {
		_, err := time.Parse(tc.format, tc.input)
		success := err == nil

		if success != tc.expected {
			t.Errorf("Parsing '%s' with format '%s': expected success=%v, got success=%v",
				tc.input, tc.format, tc.expected, success)
		}
	}
}
