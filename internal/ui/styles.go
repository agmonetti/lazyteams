package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Microsoft Teams palette — classic corporate blue (#0078D4 / #6264A7)
	colorTeams    = lipgloss.Color("#0078D4") // Teams blue
	colorTeamsAlt = lipgloss.Color("#6264A7") // Alternative Teams purple
	colorAccent   = colorTeams
	colorFocus    = lipgloss.Color("#2899F5") // Bright blue for focus
	colorBorder   = lipgloss.Color("240")     // Gray for unfocused borders
	colorBorderF  = lipgloss.Color("245")     // Light gray for focused border
	colorText     = lipgloss.Color("252")     // Primary text
	colorMuted    = lipgloss.Color("240")     // Secondary/meta text
	colorDim      = lipgloss.Color("238")     // Very dim text
	colorRed      = lipgloss.Color("9")
	colorYellow   = lipgloss.Color("11")
	colorGreen    = lipgloss.Color("10")
	colorSelBg    = lipgloss.Color("235") // Dark background for selected item
	colorSelBgF   = lipgloss.Color("236") // Background for focused selected item

	// === Panels ===
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	focusedPaneStyle = paneStyle.
				BorderForeground(colorFocus)

	// === Section titles ===
	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	// === List items ===
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

	// === General text ===
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

	// === Meta (timestamps, dates) ===
	metaStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// === System events ===
	systemEventStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	// === Double-border popups ===
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

	// === Footer with separator line ===
	footerStyle = lipgloss.NewStyle().
			BorderForeground(colorBorder).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			PaddingTop(0).
			PaddingLeft(1)

	// === Top bar with separator line ===
	topBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			BorderForeground(colorBorder).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			PaddingBottom(0).
			PaddingRight(1)

	unreadDotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))
)
