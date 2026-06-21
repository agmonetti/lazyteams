package ui

import "github.com/charmbracelet/lipgloss"

var (
	colorFocus  = lipgloss.Color("62") // Indigo
	colorNormal = lipgloss.Color("240") // Dark gray
	colorText   = lipgloss.Color("252")

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorText).
		MarginBottom(1)

	paneStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorNormal).
		Padding(0, 1)

	focusedPaneStyle = paneStyle.
		BorderForeground(colorFocus)

	selectedItemStyle = lipgloss.NewStyle().
		Foreground(colorFocus).
		Bold(true)

	normalItemStyle = lipgloss.NewStyle().
		Foreground(colorText)

	helpStyle = lipgloss.NewStyle().
		Foreground(colorNormal).
		MarginTop(1)

	// Estilos para el Navbar
	activeTabStyle = lipgloss.NewStyle().
		Foreground(colorFocus).
		Bold(true).
		Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
		Foreground(colorNormal).
		Padding(0, 1)

	tabDividerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// Gris oscuro para horas y fechas
	metaStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// Estilo sutil para eventos del sistema (reuniones) sin emojis
	systemEventStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Italic(true)

	// Popup de confirmación de descarga
	popupStyle = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("11")). // amarillo
		Padding(1, 3).
		Foreground(colorText).
		Bold(true)

	// Popup de presencia
	presencePopupStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorFocus).
		Padding(0, 2).
		Width(30)

	// Splash screen
	splashLogoStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("62")). // Indigo
		Bold(true)

	splashTitleStyle = lipgloss.NewStyle().
		Foreground(colorFocus).
		Bold(true)

	splashSubStyle = lipgloss.NewStyle().
		Foreground(colorNormal)

	splashHintStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	// Footer contextual
	footerStyle = helpStyle
)
