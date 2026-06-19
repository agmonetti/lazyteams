package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("\n[!] Error: %v\n\nPresiona 'q' para salir.\n", m.err)
	}

	if len(m.teams) == 0 {
		return "\nNo se encontraron equipos. Esta cuenta puede no tener Teams Enterprise o no pertenece a ningún equipo.\n\nPresiona 'q' para salir.\n"
	}

	// === Panel Izquierdo ===
	leftContent := ""

	// Título Equipos
	leftContent += titleStyle.Render("Equipos") + "\n"
	for i, t := range m.teams {
		cursor := "  "
		style := normalItemStyle
		if i == m.selectedTeam {
			if m.focusLeft && m.focusList == 0 {
				cursor = "▶ "
				style = selectedItemStyle
			} else {
				// Resaltar suave si no tiene foco activo pero está seleccionado
				style = style.Copy().Foreground(lipgloss.Color("245"))
			}
		}
		leftContent += fmt.Sprintf("%s%s\n", cursor, style.Render(t.DisplayName))
	}

	leftContent += "\n"

	// Título Canales
	leftContent += titleStyle.Render("Canales") + "\n"
	
	if m.channelErr != nil {
		// Mostrar error de API de forma elegante (Ej: Materia bloqueada)
		leftContent += lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(fmt.Sprintf("  [Bloqueado: %v]\n", m.channelErr))
	} else if len(m.channels) == 0 {
		leftContent += "  (sin canales)\n"
	} else {
		for i, c := range m.channels {
			cursor := "  "
			style := normalItemStyle
			if i == m.selectedChan {
				if m.focusLeft && m.focusList == 1 {
					cursor = "▶ "
					style = selectedItemStyle
				} else {
					style = style.Copy().Foreground(lipgloss.Color("245"))
				}
			}
			leftContent += fmt.Sprintf("%s%s\n", cursor, style.Render(c.DisplayName))
		}
	}

	// Le pasamos todo ese texto al viewport izquierdo
	if m.ready {
		m.leftVp.SetContent(leftContent)
		leftContent = m.leftVp.View()
	}

	// Aplicar estilos al panel izquierdo
	lStyle := paneStyle.Width((m.width / 3) - 2).Height(m.height - 4)
	if m.focusLeft {
		lStyle = focusedPaneStyle.Width((m.width / 3) - 2).Height(m.height - 4)
	}
	leftPanel := lStyle.Render(leftContent)

	// === Panel Derecho ===
	rightContent := ""
	if m.loading {
		rightContent = "Cargando...\n"
	} else if len(m.channels) > 0 && len(m.messages) == 0 && m.viewMode == ModeChat {
		rightContent = "No hay mensajes o selecciona un canal."
	} else {
		if len(m.channels) > 0 && m.selectedChan < len(m.channels) {
			tabChat := "  Publicaciones  "
			tabFiles := "  Archivos  "

			if m.viewMode == ModeChat {
				tabChat = activeTabStyle.Render(tabChat)
				tabFiles = inactiveTabStyle.Render(tabFiles)
			} else {
				tabChat = inactiveTabStyle.Render(tabChat)
				tabFiles = activeTabStyle.Render(tabFiles)
			}

			title := fmt.Sprintf("# %s", m.channels[m.selectedChan].DisplayName)
			if m.viewMode == ModeFiles && len(m.folderStack) > 0 {
				title += " / Subcarpeta"
			}
			header := titleStyle.Render(title)
			// Agregamos el header y los tabs
			rightContent += fmt.Sprintf("%s    %s%s\n\n", header, tabChat, tabFiles)
		}
		
		// Magia del viewport: imprime solo lo que entra en la pantalla
		rightContent += m.viewport.View() + "\n"

		// Renderizar la barra de texto solo en modo chat
		if !m.focusLeft && m.viewMode == ModeChat {
			if m.isTyping {
				m.input.PromptStyle = selectedItemStyle
				m.input.TextStyle = normalItemStyle
			} else {
				m.input.PromptStyle = normalItemStyle
				m.input.TextStyle = normalItemStyle
			}
			rightContent += m.input.View()
		}
	}

	// Aplicar estilos al panel derecho
	rStyle := paneStyle.Width((m.width * 2 / 3) - 4).Height(m.height - 4)
	if !m.focusLeft {
		rStyle = focusedPaneStyle.Width((m.width * 2 / 3) - 4).Height(m.height - 4)
	}
	rightPanel := rStyle.Render(rightContent)

	// Juntar paneles
	ui := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Footer (ayuda)
	help := helpStyle.Render(" [↑/↓] Navegar   [Enter] Leer   [i] Escribir   [f] Archivos   [Esc/h] Volver   [q] Salir")

	return lipgloss.JoinVertical(lipgloss.Left, ui, help)
}
