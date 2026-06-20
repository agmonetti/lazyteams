# msTTui

Cliente TUI para Microsoft Teams. Corre en la terminal, sin Electron, sin navegador.

## Requisitos

- Go 1.24+
- Cuenta de Microsoft Teams (universitaria o empresarial)
- Tokens de autenticación (ver sección abajo)

## Compilar

```bash
go build -o msTTui .
```

## Tokens

La app necesita 3 tokens. Se exportan como variables de entorno:

```bash
export MS_GRAPH_TOKEN="eyJ..."
export TEAMS_WEB_TOKEN="eyJ..."
export TEAMS_COOKIE="skype..."
```

### MS_GRAPH_TOKEN

1. Andá a https://developer.microsoft.com/graph/graph-explorer
2. Logueate con tu cuenta de Teams
3. Click en **Get access token**
4. Copiá el token

Este token se usa para: listar chats, equipos, canales, presencia, archivos.

### TEAMS_WEB_TOKEN

1. Andá a https://teams.microsoft.com
2. **F12** → pestaña **Network**
3. Filtrá por `chatsvc` y hacé click en cualquier chat
4. En **Headers**, copiá el valor de `Authorization: Bearer eyJ...`

Este token se usa para: leer/escribir mensajes (API interna de Teams).

### TEAMS_COOKIE

1. En https://teams.microsoft.com, **F12** → pestaña **Application** → **Cookies**
2. Buscá `authtoken` o `SkypeToken`
3. Copiá el valor

## Uso

```bash
./msTTui
```

### Atajos de teclado

| Tecla | Acción |
|-------|--------|
| `1` | Ir a Equipos |
| `2` | Ir a DMs |
| `↑/↓` | Navegar lista |
| `Enter` | Abrir conversación |
| `i` | Escribir mensaje |
| `f` | Ver archivos adjuntos |
| `C` | Cargar historial completo de archivos |
| `Esc` / `h` | Volver atrás |
| `q` | Salir |

### Presencia

La presencia de contactos se actualiza automáticamente:
- Primera carga: 5 segundos después de iniciar
- Actualizaciones: cada 60 segundos

Colores del indicador `●`:
- Verde: Available
- Rojo: Busy / DoNotDisturb
- Amarillo: Away / BeRightBack
- Gris: Offline / PresenceUnknown

## Arquitectura

```
msTTui/
├── main.go              # Entry point
└── internal/
    ├── auth/            # Lectura de tokens desde variables de entorno
    ├── graph/           # Cliente HTTP para Microsoft Graph API
    ├── teams/           # Cliente para API interna de Teams (chatsvc)
    └── ui/              # TUI (The Elm Architecture con Bubble Tea)
        ├── model.go     # Estado de la aplicación
        ├── update.go    # Lógica de manejo de eventos
        ├── view.go      # Renderizado de la interfaz
        ├── styles.go    # Estilos lipgloss
        └── prefs.go     # Persistencia de preferencias en disco
```

**Clean Architecture**: `ui` nunca hace llamadas HTTP. Toda red pasa por `graph` y `teams` vía comandos `tea.Cmd` (async).

**BYOT (Bring Your Own Token)**: No hay login. El usuario provee sus propios tokens JWT.

## Notas

- Los tokens expiran. Si ves errores 401, regeneralos.
- El endpoint de presencia (`/users/{id}/presence`) requiere el permiso `Presence.Read.All` en Graph Explorer. Si no lo consentís, los puntos simplemente no aparecen (la app no crashea).
- La app guarda preferencias en `~/.config/msTTui/prefs.json`.
