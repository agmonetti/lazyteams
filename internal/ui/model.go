package ui

import (
	"teamsTUI/internal/graph"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type errMsg struct{ err error }
type channelsErrMsg struct {
	teamID string
	err    error
}
type messagesErrMsg struct{ err error }

type teamsMsg struct {
	teams []graph.Team
}

type channelsMsg struct {
	teamID   string
	channels []graph.Channel
}

type messagesMsg struct {
	messages []graph.Message
}

type ViewMode int

const (
	ModeChat ViewMode = iota
	ModeFiles
)

type FolderNode struct {
	ID      string
	Name    string
	// DriveID, si no está vacío, indica que este nodo vive en un Drive
	// distinto al del Team (caso shortcut/remoteItem, ej: Materiales de clase).
	DriveID string
}

type Model struct {
	client       *graph.Client
	teams        []graph.Team
	channels     []graph.Channel
	messages     []graph.Message
	files        []graph.DriveItem
	viewMode     ViewMode
	selectedTeam int
	selectedChan int
	selectedFile int
	focusLeft    bool // true = foco en panel izquierdo (equipos/canales)
	focusList    int  // 0 = teams, 1 = channels (dentro del panel izquierdo)
	loading      bool
	err          error
	width        int
	height       int

	// Viewports y estado
	leftVp      viewport.Model
	viewport    viewport.Model
	ready       bool
	channelErr  error
	folderStack []FolderNode // Pila de nodos para volver atrás en subcarpetas y armar la ruta

	// Input para enviar mensajes
	input    textinput.Model
	isTyping bool
}

func New(client *graph.Client) Model {
	ti := textinput.New()
	ti.Placeholder = "Presiona 'i' para escribir un mensaje..."
	ti.CharLimit = 1000
	ti.Width = 50

	return Model{
		client:    client,
		focusLeft: true,
		focusList: 0,
		loading:   true,
		input:     ti,
	}
}

func (m Model) Init() tea.Cmd {
	return loadTeamsCmd(m.client)
}
