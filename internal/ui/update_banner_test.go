package ui

import (
	"strings"
	"testing"
	"time"

	"lazyteams/internal/version"
)

func TestUpdateBannerActive(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		m        Model
		expected bool
	}{
		{
			name:     "no version known",
			m:        Model{},
			expected: false,
		},
		{
			name:     "future deadline with version",
			m:        Model{latestVersion: "v9.9.9", updateBannerUntil: now.Add(time.Minute)},
			expected: true,
		},
		{
			name:     "expired deadline with version",
			m:        Model{latestVersion: "v9.9.9", updateBannerUntil: now.Add(-time.Second)},
			expected: false,
		},
		{
			name:     "zero deadline with version",
			m:        Model{latestVersion: "v9.9.9"},
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.updateBannerActive(); got != tt.expected {
				t.Errorf("updateBannerActive() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFooterTextBannerWhileActive(t *testing.T) {
	m := Model{latestVersion: "v9.9.9", updateBannerUntil: time.Now().Add(time.Minute)}
	got := m.footerText()
	if !strings.Contains(got, "New version v9.9.9 available") {
		t.Errorf("footerText() = %q, want update notice", got)
	}
	if strings.Contains(got, "Help") {
		t.Errorf("footerText() = %q, contextual hints must be hidden while the banner is active", got)
	}
}

func TestFooterTextNormalAfterExpiry(t *testing.T) {
	m := Model{latestVersion: "v9.9.9", updateBannerUntil: time.Now().Add(-time.Second)}
	got := m.footerText()
	if strings.Contains(got, "New version") {
		t.Errorf("footerText() = %q, update notice must not appear after expiry", got)
	}
	if !strings.Contains(got, "Help") {
		t.Errorf("footerText() = %q, contextual hints must return after expiry", got)
	}
}

func TestUpdateCheckMsgArmsBanner(t *testing.T) {
	m := Model{}
	updated, cmd := m.Update(updateCheckMsg{latest: "v9.9.9"})
	got := updated.(Model)
	if got.latestVersion != "v9.9.9" {
		t.Errorf("latestVersion = %q, want v9.9.9", got.latestVersion)
	}
	deadline := got.updateBannerUntil
	if deadline.IsZero() {
		t.Fatal("updateBannerUntil not set after updateCheckMsg")
	}
	delta := deadline.Sub(time.Now())
	if delta > updateBannerDuration+time.Second || delta < updateBannerDuration-time.Second {
		t.Errorf("updateBannerUntil delta = %v, want ~%v", delta, updateBannerDuration)
	}
	if cmd == nil {
		t.Error("expected a cmd to expire the banner, got nil")
	}
}

func TestUpdateCheckMsgIgnoresStaleVersion(t *testing.T) {
	version.Version = "v9.9.9"
	defer func() { version.Version = "dev" }()

	m := Model{}
	updated, _ := m.Update(updateCheckMsg{latest: "v1.0.0"})
	got := updated.(Model)
	if got.latestVersion != "" {
		t.Errorf("latestVersion = %q, want empty for stale update", got.latestVersion)
	}
	if !got.updateBannerUntil.IsZero() {
		t.Error("updateBannerUntil must stay zero for stale update")
	}
}

func TestUpdateBannerExpiredMsgClears(t *testing.T) {
	m := Model{latestVersion: "v9.9.9", updateBannerUntil: time.Now().Add(time.Minute)}
	updated, _ := m.Update(updateBannerExpiredMsg{})
	got := updated.(Model)
	if !got.updateBannerUntil.IsZero() {
		t.Error("updateBannerUntil must be cleared after expiry message")
	}
	if strings.Contains(got.footerText(), "New version") {
		t.Error("footerText() must not show the update notice after expiry message")
	}
}
