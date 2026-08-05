package ui

import (
	"testing"
	"time"

	"teamsTUI/internal/graph"
)

func TestFilterMessagesMatchesBodyAndSenderCaseInsensitively(t *testing.T) {
	m := Model{}
	messages := []graph.Message{
		{Body: "The exam is tomorrow", FromName: "Alice"},
		{Body: "Project notes", FromName: "Bob"},
		{Body: "Lunch plans", FromName: "Carla"},
	}

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{name: "body", query: "EXAM", want: 1},
		{name: "sender", query: "bob", want: 1},
		{name: "no match", query: "missing", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.filterMessages(messages, tt.query)
			if len(got) != tt.want {
				t.Fatalf("filterMessages(%q) returned %d messages, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

func TestFilterMessagesReturnsOriginalSliceForEmptyQuery(t *testing.T) {
	m := Model{}
	messages := []graph.Message{{Body: "one"}, {Body: "two"}}
	got := m.filterMessages(messages, "")
	if len(got) != len(messages) {
		t.Fatalf("empty query returned %d messages, want %d", len(got), len(messages))
	}
	if &got[0] != &messages[0] {
		t.Error("empty query should return the original message slice")
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		age  time.Duration
		want string
	}{
		{name: "now", age: 10 * time.Second, want: "now"},
		{name: "one minute", age: 1*time.Minute + time.Second, want: "1 min ago"},
		{name: "minutes", age: 5*time.Minute + time.Second, want: "5 min ago"},
		{name: "one hour", age: time.Hour + time.Second, want: "1 h ago"},
		{name: "hours", age: 3*time.Hour + time.Second, want: "3 h ago"},
		{name: "one day", age: 24*time.Hour + time.Second, want: "1 day ago"},
		{name: "days", age: 3*24*time.Hour + time.Second, want: "3 days ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatAge(time.Now().Add(-tt.age)); got != tt.want {
				t.Errorf("formatAge(%s) = %q, want %q", tt.age, got, tt.want)
			}
		})
	}
}
