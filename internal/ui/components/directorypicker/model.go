package directorypicker

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

type QuickAccessItem struct {
	Name string
	Path string
}

type Model struct {
	Title        string
	currentPath  string
	entries      []DirEntry
	cursor       int
	quickAccess  []QuickAccessItem
	quickCursor  int
	onQuickPanel bool // true = focus on quick access, false = focus on dir list
	err          error
	width        int
	height       int
	lastUsedPath string // persistent last used folder
	mode         string // "dir" or "file"
}

func New(opts Options) Model {
	home := homeDir()
	initial := home
	if opts.InitialPath != "" && pathExists(opts.InitialPath) {
		initial = opts.InitialPath
	} else if opts.LastUsedPath != "" && pathExists(opts.LastUsedPath) {
		initial = opts.LastUsedPath
	}

	title := opts.Title
	if title == "" {
		title = "Select folder"
	}

	mode := opts.Mode
	if mode == "" {
		mode = "dir"
	}

	m := Model{
		Title:       title,
		currentPath: initial,
		quickAccess: []QuickAccessItem{
			{Name: "Home", Path: home},
			{Name: "Downloads", Path: filepath.Join(home, "Downloads")},
			{Name: "Documents", Path: filepath.Join(home, "Documents")},
			{Name: "Desktop", Path: filepath.Join(home, "Desktop")},
		},
		onQuickPanel: true,
		lastUsedPath: opts.LastUsedPath,
		mode:         mode,
		width:        opts.Width,
		height:       opts.Height,
	}

	m.loadDir()
	return m
}

func (m *Model) loadDir() {
	entries, err := readDir(m.currentPath, m.mode == "file")
	if err != nil {
		m.err = err
		m.entries = nil
		return
	}
	m.err = nil
	m.entries = entries
	m.cursor = 0
}

func (m Model) Init() tea.Cmd {
	return nil
}
