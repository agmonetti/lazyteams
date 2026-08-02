package ui

import (
	"runtime"
	"strconv"
	"strings"
)

var (
	symReply     = "↳"
	symReplying  = "↩"
	symCursor    = "▶ "
	symArrowDown = "▼"
	symLock      = " 🔒"
)

func init() {
	if runtime.GOOS == "windows" {
		symReply = ">>"
		symReplying = "[Replying]"
		symCursor = "> "
		symArrowDown = "v"
		symLock = " [P]"
	}
}

func reactionEmoji(key string) string {
	if runtime.GOOS == "windows" {
		switch key {
		case "like", "yes-tone0", "yes-tone1", "yes-tone2", "yes-tone3", "yes-tone4", "yes-tone5":
			return "[Like]"
		case "heart", "heartlightblue":
			return "[Heart]"
		case "laugh", "rofl":
			return "[Laugh]"
		case "surprised":
			return "[Wow]"
		case "sad":
			return "[Sad]"
		case "angry", "angryface":
			return "[Angry]"
		case "speechless", "lipssealed":
			return "[...]"
		case "fire":
			return "[Fire]"
		case "think":
			return "[Think]"
		case "cool":
			return "[Cool]"
		case "no", "no-tone0", "no-tone1", "no-tone2", "no-tone3", "no-tone4", "no-tone5":
			return "[No]"
		case "clappinghands", "clappinghands-tone0", "clappinghands-tone1", "clappinghands-tone2", "clappinghands-tone3", "clappinghands-tone4", "clappinghands-tone5":
			return "[Clap]"
		case "follow":
			return "[Eyes]"
		case "soccerball":
			return "[Goal]"
		case "1f389_partypopper":
			return "[Party]"
		case "1f410_goat":
			return "[GOAT]"
		}
		
		// Fallback for dynamic emojis like "1f44d_thumbsup"
		parts := strings.SplitN(key, "_", 2)
		if len(parts) == 2 {
			return "[" + parts[1] + "]"
		}
		return "[" + key + "]"
	}

	switch key {
	// Standard
	case "like", "yes-tone0":      return "👍"
	case "heart":                   return "❤️"
	case "laugh":                   return "😂"
	case "surprised":               return "😮"
	case "sad":                     return "😢"
	case "angry":                   return "😡"
	// Skin tone variants
	case "yes-tone1":               return "👍🏻"
	case "yes-tone2":               return "👍🏼"
	case "yes-tone3":               return "👍🏽"
	case "yes-tone4":               return "👍🏾"
	case "yes-tone5":               return "👍🏿"
	// Extra
	case "heartlightblue":          return "💙"

	// Expressions
	case "speechless":              return "😶"
	case "fire":                    return "🔥"
	case "faceinclouds":            return "😶‍🌫️"
	case "think":                   return "🤔"
	case "rofl":                    return "🤣"
	case "fingerscrossed":          return "🤞"
	case "cool":                    return "😎"
	case "lipssealed":              return "🤐"
	case "angryface":               return "😠"
	case "sweat":                   return "😓"
	case "diagonalmouth":           return "😑"

	// Gestures
	case "no", "no-tone0":          return "🙅"
	case "no-tone1":                return "🙅🏻"
	case "no-tone2":                return "🙅🏼"
	case "no-tone3":                return "🙅🏽"
	case "no-tone4":                return "🙅🏾"
	case "no-tone5":                return "🙅🏿"
	case "clappinghands", "clappinghands-tone0": return "👏"
	case "clappinghands-tone1":     return "👏🏻"
	case "clappinghands-tone2":     return "👏🏼"
	case "clappinghands-tone3":     return "👏🏽"
	case "clappinghands-tone4":     return "👏🏾"
	case "clappinghands-tone5":     return "👏🏿"
	case "follow":                  return "👀"

	// Objects / symbols
	case "soccerball":              return "⚽"
	case "1f389_partypopper":       return "🎉"
	case "1f410_goat":              return "🐐"

	default:
		parts := strings.SplitN(key, "_", 2)
		if len(parts) >= 1 {
			if cp, err := strconv.ParseInt(parts[0], 16, 32); err == nil {
				return string(rune(cp))
			}
		}
		return "●"
	}
}
