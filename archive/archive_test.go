package archive

import (
	"testing"
	"time"
)

func TestMonthHint(t *testing.T) {
	cases := []struct {
		text, want string
	}{
		{"покажи что я просил в августе 25", "август"},
		{"в марте", "март"},
		{"hello world", ""},
		{"АПРЕЛЬ 2024", "апрел"},
	}
	for _, c := range cases {
		got := MonthHint(c.text)
		if got != c.want {
			t.Errorf("MonthHint(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestParseMonth(t *testing.T) {
	now := time.Date(2026, time.September, 14, 0, 0, 0, 0, time.UTC)
	m, y := ParseMonth("в августе 25", now)
	if m != time.August || y != 2025 {
		t.Errorf("want Aug 2025, got %s %d", m, y)
	}

	m, y = ParseMonth("в октябре 2024", now)
	if m != time.October || y != 2024 {
		t.Errorf("want Oct 2024, got %s %d", m, y)
	}

	m, y = ParseMonth("в октябре", now)
	if m != time.October || y != 2026 {
		t.Errorf("want Oct 2026, got %s %d", m, y)
	}
}

func TestFileName(t *testing.T) {
	got := FileName(2025, time.August)
	want := "2025-08.xlsx"
	if got != want {
		t.Errorf("FileName() = %q, want %q", got, want)
	}
}
