package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Paleta Microsoft Teams — azul corporativo clásico (#0078D4 / #6264A7)
	colorTeams    = lipgloss.Color("#0078D4") // Azul Teams
	colorTeamsAlt = lipgloss.Color("#6264A7") // Púrpura Teams alternativo
	colorAccent   = colorTeams
	colorFocus    = lipgloss.Color("#2899F5") // Azul brillante para foco
	colorBorder   = lipgloss.Color("240")     // Gris para bordes sin foco
	colorBorderF  = lipgloss.Color("245")     // Gris claro para borde enfocado
	colorText     = lipgloss.Color("252")     // Texto principal
	colorMuted    = lipgloss.Color("240")     // Texto secundario/meta
	colorDim      = lipgloss.Color("238")     // Texto muy tenue
	colorRed      = lipgloss.Color("9")
	colorYellow   = lipgloss.Color("11")
	colorGreen    = lipgloss.Color("10")
	colorSelBg    = lipgloss.Color("235")     // Fondo oscuro para item seleccionado
	colorSelBgF   = lipgloss.Color("236")     // Fondo para item seleccionado con foco

	// === Paneles ===
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	focusedPaneStyle = paneStyle.
				BorderForeground(colorFocus)

	// === Títulos de sección ===
	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	// === Items de lista ===
	selectedItemStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	selectedItemBgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(colorSelBgF).
				Bold(true)

	normalItemStyle = lipgloss.NewStyle().
			Foreground(colorText)

	dimItemStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// === Texto general ===
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText).
			MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	// === Navbar / Tabs ===
	activeTabStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Padding(0, 1)

	tabDividerStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	// === Meta (horas, fechas) ===
	metaStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// === Eventos del sistema ===
	systemEventStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	// === Popups con doble borde ===
	popupStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorAccent).
			Padding(1, 3).
			Foreground(colorText).
			Bold(true)

	presencePopupStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(colorAccent).
				Padding(0, 2).
				Width(30)

	// === Splash screen ===
	splashLogoStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	splashTitleStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Bold(true)

	splashSubStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	splashHintStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	// === Footer con línea separadora ===
	footerStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			BorderForeground(colorBorder).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			PaddingTop(0).
			PaddingLeft(1)

	// === Top bar con línea separadora ===
	topBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			BorderForeground(colorBorder).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			PaddingBottom(0).
			PaddingRight(1)
)
