package directorypicker

// Msg types emitted by the picker
type SelectedMsg struct {
	Path string
}

type CancelledMsg struct{}

// Options for creating a new picker
type Options struct {
	Title        string
	InitialPath  string
	LastUsedPath string
	Mode         string // "dir" or "file"
	Width        int
	Height       int
}
