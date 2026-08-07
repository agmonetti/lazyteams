package ui

import "testing"

func TestReactionEmojiOnUNIX(t *testing.T) {
    cases := []struct {
        input    string
        expected string
    }{
        {"like","👍"},
        {"yes-tone0","👍"},
        {"heart", "\u2764\ufe0f"},
        {"laugh", "😂"},
		{"surprised", "😮"},
		{"sad","😢"},
		{"angry", "😡"},
		// Skin tone variants
		{"yes-tone1", "👍\U0001F3FB"},
		{"yes-tone2", "👍\U0001F3FC"},
		{"yes-tone3", "👍\U0001F3FD"},
		{"yes-tone4", "👍\U0001F3FE"},
		{"yes-tone5", "👍\U0001F3FF"},
		{"heartlightblue", "💙"},
		{"speechless", "😶"},
		{"fire", "🔥"},
		{"think", "🤔"},
		{"rofl", "🤣"},
		{"cool", "😎"},
		{"lipssealed", "🤐"},
		{"angryface", "😠"},
		{"no", "🙅"},
		{"no-tone1", "🙅🏻"},
		{"no-tone2", "🙅🏼"},
		{"no-tone3", "🙅🏽"},
		{"no-tone4", "🙅🏾"},
		{"no-tone5", "🙅🏿"},
		{"clappinghands", "👏"},
		{"clappinghands-tone1", "👏🏻"},
		{"clappinghands-tone2", "👏🏼"},
		{"clappinghands-tone3", "👏🏽"},
		{"clappinghands-tone4", "👏🏾"},
		{"clappinghands-tone5", "👏🏿"},
		{"follow", "👀"},
		{"soccerball", "⚽"},
		{"1f389_partypopper", "🎉"},
		{"1f410_goat", "🐐"},

		//  hex codepoint
		{"1f525_fire", "🔥"},
		{"1f4a9_poop", "💩"},
		{"1f600_grinning"  , "😀"},
		{"1f601_beaming"   , "😁"},
		{"1f602_joy"       , "😂"},
		{"1f923_rofl"      , "🤣"},
		{"1f60a_blush"     , "😊"},
		{"1f60d_hearts"    , "😍"},
		{"1f914_think"     , "🤔"},
		{"1f44f_clap"      , "👏"},
		{"1f4a5_boom"      , "💥"},
		{"1f6b6_walking"   , "🚶"},
		{"1f355_pizza"     , "🍕"},
		{"1f40d_snake"     , "🐍"},
		{"1f980_crab"      , "🦀"},
		{"1f47e_alien"     , "👾"},

		//not known 
		{"unknown","●"},
		{"xyz_something","●"},
    }
    for _, tc := range cases {
        got := reactionEmoji(tc.input)
        if got != tc.expected {
            t.Errorf("emoji(%q) = %q, want %q", tc.input, got, tc.expected)
        }
    }
}
