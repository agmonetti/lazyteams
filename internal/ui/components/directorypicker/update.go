package directorypicker

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return CancelledMsg{} }

		case "enter":
			if m.mode == "file" && len(m.entries) > 0 {
				selected := m.entries[m.cursor]
				if !selected.IsDir {
					return m, func() tea.Msg { return SelectedMsg{Path: selected.Path} }
				}
				// Dir in file mode → navigate into it
				m.currentPath = selected.Path
				m.loadDir()
				return m, nil
			}
			// Dir mode → select current directory
			return m, func() tea.Msg { return SelectedMsg{Path: m.currentPath} }

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}

		case "right", "l":
			if len(m.entries) > 0 && m.entries[m.cursor].IsDir {
				m.currentPath = m.entries[m.cursor].Path
				m.loadDir()
			}

		case "left", "h":
			parent := parentPath(m.currentPath)
			if parent != m.currentPath {
				m.currentPath = parent
				m.loadDir()
			}

		case "home":
			m.cursor = 0

		case "end":
			if len(m.entries) > 0 {
				m.cursor = len(m.entries) - 1
			}

		case "pgup":
			m.cursor -= 10
			if m.cursor < 0 {
				m.cursor = 0
			}

		case "pgdown":
			m.cursor += 10
			if m.cursor >= len(m.entries) {
				m.cursor = len(m.entries) - 1
			}
		}
	}
	return m, nil
}
