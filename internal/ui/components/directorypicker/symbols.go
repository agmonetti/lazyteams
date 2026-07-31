package directorypicker

import "runtime"

var symCursor = "▶ "

func init() {
	if runtime.GOOS == "windows" {
		symCursor = "> "
	}
}
