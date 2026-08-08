//go:build windows

package ui

import "testing"

func TestReactionEmojiOnWindows(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		// Known keys
		{"like", "[Like]"}, {"yes-tone0", "[Like]"}, {"yes-tone1", "[Like]"},
		{"yes-tone2", "[Like]"}, {"yes-tone3", "[Like]"}, {"yes-tone4", "[Like]"},
		{"yes-tone5", "[Like]"},
		{"heart", "[Heart]"}, {"heartlightblue", "[Heart]"},
		{"laugh", "[Laugh]"}, {"rofl", "[Laugh]"},
		{"surprised", "[Wow]"}, {"sad", "[Sad]"},
		{"angry", "[Angry]"}, {"angryface", "[Angry]"},
		{"speechless", "[...]"}, {"lipssealed", "[...]"},
		{"fire", "[Fire]"}, {"think", "[Think]"}, {"cool", "[Cool]"},
		{"no", "[No]"}, {"no-tone0", "[No]"}, {"no-tone1", "[No]"},
		{"no-tone2", "[No]"}, {"no-tone3", "[No]"}, {"no-tone4", "[No]"}, {"no-tone5", "[No]"},
		{"clappinghands", "[Clap]"}, {"clappinghands-tone0", "[Clap]"},
		{"clappinghands-tone1", "[Clap]"}, {"clappinghands-tone2", "[Clap]"},
		{"clappinghands-tone3", "[Clap]"}, {"clappinghands-tone4", "[Clap]"},
		{"clappinghands-tone5", "[Clap]"},
		{"follow", "[Eyes]"}, {"soccerball", "[Goal]"},
		{"1f389_partypopper", "[Party]"}, {"1f410_goat", "[GOAT]"},
		// Dynamic hex fallback → bracketed label
		{"1f44d_thumbsup", "[thumbsup]"}, {"1f525_fire", "[fire]"},
		{"1f600_grinning", "[grinning]"},
		// Unknown → bracketed key/label
		{"unknown", "[unknown]"}, {"xyz_something", "[something]"}, {"faceinclouds", "[faceinclouds]"},
	}
	for _, tc := range cases {
		got := reactionEmoji(tc.input)
		if got != tc.expected {
			t.Errorf("emoji(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
