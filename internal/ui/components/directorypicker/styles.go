package directorypicker

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent = lipgloss.Color("#0078D4")
	colorText   = lipgloss.Color("252")
	colorMuted  = lipgloss.Color("240")
	colorDim    = lipgloss.Color("238")
	colorBorder = lipgloss.Color("245")
	colorFocus  = lipgloss.Color("#2899F5")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(colorText)

	dimStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	breadcrumbStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	pathStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9"))

	dirListStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	popupStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorFocus).
			Padding(1, 2)
)
