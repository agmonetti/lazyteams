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
type messagesErrMsg struct {
	err            error
	conversationID string
	partialMsgs    []graph.Message // mensajes traídos antes del fallo
}

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

type notificationsMsg struct {
	items []graph.NotificationItem
}
type notificationsErrMsg struct {
	err error
}

type assignmentsMsg struct {
	items []graph.Assignment
}
type assignmentsErrMsg struct {
	err error
}

type ViewMode int

const (
	ModeChat ViewMode = iota
	ModeFiles
)

type ActivityFilter int

const (
	FilterAll      ActivityFilter = iota
	FilterUpcoming                 // Próximamente (vence en ≤7 días)
	FilterOverdue                  // Vencida
	FilterCompleted                // Completada/Entregada
)

type Workspace int

const (
	WorkspaceTeams       Workspace = iota
	WorkspaceDMs
	WorkspaceActivity
	WorkspaceAssignments
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
	teamsLoaded  bool
	channels     []graph.Channel
	messages     []graph.Message
	files        []graph.DriveItem
	viewMode     ViewMode
	workspace    Workspace
	chats        []graph.Chat
	selectedChat int
	chatsLoaded  bool
	selfID       string
	selectedTeam int
	loadedConvID string
	prefs        Preferences
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

	// Descarga de archivos
	currentFilesDriveID string          // "" = drive por defecto del equipo; sino, ID de drive explícito
	selectedFiles       map[int]bool    // índices marcados para descarga múltiple
	confirmingDownload  bool            // true mientras se muestra el popup de confirmación
	downloadTargets     []graph.DriveItem
	downloadStatus      string          // mensaje de resultado
	downloadStatusID    int             // incrementa con cada descarga, evita que un clear viejo borre un status nuevo
	downloading         bool            // true mientras se descarga
	folderCache         map[string][]graph.DriveItem // cache de carpetas: folderID → contenido
	editingDownloadDir  bool            // true mientras se edita la carpeta destino
	downloadDirInput    textinput.Model // input para editar la ruta de descarga

	// Mapa channelID → teamID para navegación desde notificaciones
	channelToTeam map[string]string

	// Input para enviar mensajes
	input    textinput.Model
	isTyping bool

	// Usuario
	userName string

	// Presencia (selección)
	showPresenceMenu bool
	presenceCursor   int
	presenceOptions  []string
	presenceError    string

	// Ventana deslizante para canales
	channelWindowStart int

	// Activity / Notificaciones
	notifications      []graph.NotificationItem
	notifLoaded        bool
	selectedNotif      int
	notifErr           error
	activityFilter     ActivityFilter


	// Assignments / Tareas
	assignments      []graph.Assignment
	assignLoaded     bool
	selectedAssign   int
	assignErr        error
	assignFilter     ActivityFilter

	// Polling de DMs
	chatUnread map[string]bool  // chatID → tiene mensajes nuevos
	presence   map[string]string // userID → Availability (Available, Busy, Away, Offline, etc.)
}

func New(client *graph.Client, userName string) Model {
	ti := textinput.New()
	ti.Placeholder = "Presiona 'i' para escribir un mensaje..."
	ti.CharLimit = 1000
	ti.Width = 50

	dirInput := textinput.New()
	dirInput.Placeholder = "~/Downloads"
	dirInput.CharLimit = 256

	return Model{
		client:       client,
		focusLeft:    true,
		focusList:    0,
		loading:      true,
		input:        ti,
		downloadDirInput: dirInput,
		prefs:        loadPrefs(),
		chatUnread:   make(map[string]bool),
		presence:     make(map[string]string),
		selectedFiles: make(map[int]bool),
		folderCache:  make(map[string][]graph.DriveItem),
		channelToTeam: make(map[string]string),
		userName:     userName,
		presenceOptions: []string{"Available", "Busy", "DoNotDisturb", "BeRightBack", "Away", "Reset (Automático)"},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadTeamsCmd(m.client), loadMeCmd(m.client), refreshTickCmd(), initialPresenceTickCmd())
}

func (m Model) activeConversationID() string {
	if m.workspace == WorkspaceDMs {
		if m.selectedChat < len(m.chats) {
			return m.chats[m.selectedChat].ID
		}
		return ""
	}
	if m.selectedChan < len(m.channels) {
		return m.channels[m.selectedChan].ID
	}
	return ""
}
