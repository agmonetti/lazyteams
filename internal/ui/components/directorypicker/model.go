package directorypicker

import (
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	Title       string
	currentPath string
	entries     []DirEntry
	cursor      int
	err         error
	width       int
	height      int
	mode        string // "dir" or "file"
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
		mode:        mode,
		width:       opts.Width,
		height:      opts.Height,
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
