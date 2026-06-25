# msTTui

Cliente TUI para Microsoft Teams. Corre en la terminal, sin Electron, sin navegador.

**~3,500 líneas de Go.** Clean Architecture + Elm Architecture (Bubble Tea). Un solo binario estático.

## Screenshots

```
╔══════════════════════╦══════════════════════════════════════╗
║ Chats                ║            TEAMS-TUI                 ║
║                      ║                                      ║
║ ► Notas personales   ║  Microsoft Teams Terminal UI         ║
║   MONETTI FRANCISCO  ║  v1.0.0-beta                        ║
║   CACERES GARCIA...  ║                                      ║
║   WEHBE RICARDO...   ║  [↑/↓] Navegar equipos · [Enter]   ║
║   ...                ║                                      ║
║                      ║                                      ║
╠══════════════════════╩══════════════════════════════════════╣
║ [1] Equipos  [2] DMs  [3] Actividad  [4] Tareas            ║
╚═════════════════════════════════════════════════════════════╝
```

## Requisitos

- Go 1.24+
- Cuenta de Microsoft Teams (universitaria o empresarial)
- Linux (testeado en Arch). Playwright necesita Firefox (para el auth helper).

## Compilar

```bash
# TUI principal
go build -o msTTui .

# Helper de autenticación (captura tokens automáticamente)
go build -o msTTui-auth ./cmd/auth-helper/
```

## Uso rápido

```bash
# Primera vez: capturar tokens (abre Firefox, hacé login una vez)
./msTTui-auth

# Ejecutar la TUI
./msTTui

# Renovar tokens (cuando expiran, ~24h)
./msTTui-auth
```

## Tokens

msTTui necesita 5 tokens para funcionar. El helper `msTTui-auth` los captura automáticamente via Playwright (Firefox) o podés exportarlos manualmente.

### Captura automática

```bash
./msTTui-auth
```

Abre Firefox con sesión persistente (~90 segundos total):

1. **TEAMS_COOKIE** — captura inmediata del header Cookie
2. **TEAMS_WEB_TOKEN** — al navegar a un canal (~8s)
3. **TEAMS_NOTIF_TOKEN** — al forzar request a notificaciones (~20s)
4. **EDU_TOKEN** — al clickear Assignments (~40s)
5. **MS_GRAPH_TOKEN** — via Graph Explorer al final (~45s)

Los tokens se guardan en `~/.config/teamstui/tokens.env`.

### Tokens manualmente

```bash
export MS_GRAPH_TOKEN="eyJ..."
export TEAMS_WEB_TOKEN="eyJ..."
export TEAMS_NOTIF_TOKEN="eyJ..."
export EDU_TOKEN="eyJ..."
export TEAMS_COOKIE="skype..."
```

### Descripción de cada token

| Token | Expiración | Uso | API |
|-------|-----------|-----|-----|
| `MS_GRAPH_TOKEN` | ~1h | Chats, equipos, canales, presencia, archivos | Microsoft Graph v1.0 |
| `TEAMS_WEB_TOKEN` | ~24h | Leer/escribir mensajes | API interna de Teams (ChatSvc) |
| `TEAMS_NOTIF_TOKEN` | ~24h | Notificaciones push | API interna de Teams |
| `EDU_TOKEN` | ~1h | Tareas/Assignments educativos | Microsoft Education API |
| `TEAMS_COOKIE` | ~24h | Autenticación de sesión | Headers HTTP |

### Renovación

- **TEAMS_WEB_TOKEN / TEAMS_NOTIF_TOKEN / TEAMS_COOKIE**: ~24h. Al expirar, la TUI muestra un error amigable.
- **MS_GRAPH_TOKEN / EDU_TOKEN**: ~1h. Si la TUI los necesita, se muestra instrucción para renovar.
- **No hay renovación automática** — corré `./msTTui-auth` de nuevo.

## Funcionalidades

### Equipos y Canales (workspace `1`)
- Lista equipos unidos (`/me/joinedTeams`)
- Lista canales por equipo con **paginación** (`@odata.nextLink`)
- Sliding window para listas largas de canales
- Selección de canal carga mensajes via ChatSvc API

### Mensajes Directos / Chats (workspace `2`)
- Lista chats 1:1 y grupales (`/me/chats?$expand=members`)
- **Resolución inteligente de nombres**: topic del grupo, nombres de miembros (excluyendo al usuario actual), preview del último mensaje
- **Auto-descubrimiento de chat personal** ("Notas personales"): prueba 4 formatos MRI contra ChatSvc
- **Indicadores de no leído** (`●` amarillo) en chats con mensajes nuevos
- Polling cada 15 segundos

### Mensajes
- **Leer mensajes** via ChatSvc API (hasta 2000 mensajes, paginados de 200)
- **Enviar mensajes** (RichText/Html, límite 1000 caracteres)
- **Limpieza HTML**: convierte `<br>`, `<p>`, `<div>`, `<tr>`, `<td>`, `<li>` a texto plano
- **Links clickeables** usando ANSI OSC 8 (funciona en Kitty, Alacritty, GNOME Terminal)
- Auto-refresh de mensajes cada 15 segundos
- Tipos de mensaje soportados: Text, RichText/Html, Event/Call, MemberAdded/Removed

### Archivos / Navegador de Drive
- Explora archivos de canales via Graph API (`/groups/{id}/drive`)
- Navegación recursiva de carpetas con stack de retroceso
- **Soporte completo para remote items** (atajos de SharePoint, ej: "Materiales de clase")
- Descubrimiento automático de la carpeta "Class Materials" en tenants educativos
- Iconos por extensión: `[PDF]`, `[PPT]`, `[DOC]`, `[XLS]`, `[VID]`, `[ZIP]`, `[DIR]`, `[LINK]`, `[FILE]`
- **Multi-selección** con Space, descarga con confirmación `[y]/[n]`
- Descarga a `~/Downloads/`
- Fallback: si falla, abre en el navegador del sistema (`xdg-open`)

### Notificaciones / Actividad (workspace `3`)
- Fetch de notificaciones via ChatSvc `48:notifications`
- Tipos etiquetados: `[TAREA]`, `[VENCE]`, `[@]`, `[RESP]`, `[MSG]`, `[NOTIF]`, `[like]`
- Timestamps relativos: "ahora", "hace N min", "hace N h", "hace N días"
- Filtros: "Todas", "Próximas", "Vencidas"
- **Navegar al canal origen** desde el detalle de notificación con `[o]`

### Asignaciones Educacionales (workspace `4`)
- **UI completamente construida**: filtros, vista de detalle, estados
- **La API está bloqueada por WAF** (TLS fingerprinting) — el workspace muestra un error explicativo
- Estados: `[ENTREGADA]`, `[DEVUELTA]`, `[PENDIENTE]`, `[TAREA]`

### Presencia
- **Leer presencia** de contactos (Graph API, concurrente con goroutines)
- **Cambiar tu propio estado** con `p`: Available, Busy, DoNotDisturb, BeRightBack, Away, Reset (Automático)
- **Auto-refresh** cada 60 segundos
- Indicadores de color: 🟢 Available, 🔴 Busy/DoNotDisturb, 🟡 Away/BeRightBack, ⚫ Offline

## Atajos de teclado

### Global
| Tecla | Acción |
|-------|--------|
| `1` | Ir a Equipos |
| `2` | Ir a DMs |
| `3` | Ir a Actividad |
| `4` | Ir a Tareas |
| `Tab` | Toggle foco entre paneles |
| `p` | Menú de presencia |
| `q` / `Ctrl+C` | Salir |

### Navegación (panel izquierdo)
| Tecla | Acción |
|-------|--------|
| `↑` / `k` | Mover cursor arriba |
| `↓` / `j` | Mover cursor abajo |
| `←` / `h` | En Equipos: volver a lista de equipos. En Archivos: retroceder carpeta |
| `→` / `l` | En Equipos: entrar a canales del equipo seleccionado |
| `Enter` | Abrir selección |

### Chat (panel derecho)
| Tecla | Acción |
|-------|--------|
| `↑` / `k` | Scroll mensajes arriba |
| `↓` / `j` | Scroll mensajes abajo |
| `i` | Modo escritura |
| `f` | Ver archivos del canal/DM |
| `o` | Ir al canal del hilo actual (desde notificaciones) |
| `Esc` / `h` | Volver al panel izquierdo |

### Escritura
| Tecla | Acción |
|-------|--------|
| `Enter` | Enviar mensaje |
| `Esc` | Cancelar escritura |

### Archivos
| Tecla | Acción |
|-------|--------|
| `↑` / `k` | Mover selección arriba |
| `↓` / `j` | Mover selección abajo |
| `Enter` | Abrir carpeta / abrir archivo en navegador |
| `Space` | Toggle selección (multi-selección) |
| `o` | Descargar seleccionados (confirmación) |
| `c` / `C` | Cargar historial completo para archivos del DM |
| `←` / `h` | Retroceder en carpetas |
| `f` | Volver a vista de chat |

### Notificaciones
| Tecla | Acción |
|-------|--------|
| `←` / `h` | Filtro izquierdo |
| `→` / `l` | Filtro derecho |
| `o` | Ir al canal origen |
| `r` | Reintentar carga |

### Presencia (popup)
| Tecla | Acción |
|-------|--------|
| `↑` / `↓` | Navegar opciones |
| `Enter` | Confirmar |
| `Esc` / `q` | Cancelar |

### Descarga (popup)
| Tecla | Acción |
|-------|--------|
| `y` / `Enter` | Confirmar |
| `n` / `Esc` | Cancelar |

## Arquitectura

```
msTTui/
├── main.go                          # Entry point
├── cmd/
│   ├── auth-helper/                 # Playwright auth helper (captura 5 tokens)
│   ├── debug-assignments/           # Debug tool para Education API
│   └── debug-endpoints/             # Debug tool para endpoints
└── internal/
    ├── auth/                        # Tokens (lectura + JWT parsing)
    │   └── auth.go                  # GetTokens(), loadTokensFile(), ParseUserNameFromToken()
    ├── graph/                       # HTTP client para Microsoft Graph API
    │   ├── client.go                # Client, doReq(), cleanHTML()
    │   ├── teams_api.go             # Equipos y canales
    │   ├── chats_api.go             # Chats, auto-descubrimiento self-chat
    │   ├── messages_api.go          # Leer/escribir mensajes (ChatSvc)
    │   ├── files_api.go             # Navegador de Drive, remote items
    │   ├── presence_api.go          # Get/Set/Clear presencia
    │   ├── assignments_api.go       # Education Assignments (bloqueado por WAF)
    │   ├── activity_api.go          # Notificaciones via ChatSvc
    │   ├── download_api.go          # Descarga de archivos
    │   └── debug_api.go             # Endpoints de debug
    ├── teams/teams.go               # Agregación de adjuntos, iconos de archivos
    └── ui/                          # TUI (Elm Architecture con Bubble Tea)
        ├── model.go                 # Estado de la aplicación (~200 líneas)
        ├── update.go                # Lógica de eventos (~1400 líneas)
        ├── view.go                  # Renderizado (~700 líneas)
        ├── styles.go                # Estilos lipgloss
        └── prefs.go                 # Preferencias persistentes en disco
```

**Clean Architecture**: `ui` nunca hace llamadas HTTP. Toda red pasa por `graph` y `teams` vía comandos `tea.Cmd` (async).

**4 Workspaces**: Equipos (1), DMs (2), Actividad (3), Tareas (4). Cada uno con panel izquierdo (lista) y derecho (detalle).

### Auth Helper — Cómo funciona

El helper usa **Playwright** con Firefox para capturar tokens automáticamente:

1. **Sesión persistente** (`~/.config/teamstui/browser-session/`): después del primer login, las veces siguientes no pedís credenciales.
2. **Request interceptor** (`context.On("request")`): captura cada Bearer token que Teams envía, discriminando por `aud` (claim del JWT).
3. **Navegación automática**: el helper navega por Teams para disparar requests que contienen los tokens necesarios.
4. **Graph Explorer**: al final del loop, si falta `MS_GRAPH_TOKEN`, abre Graph Explorer y captura el token del interceptor o del DOM.

**Descubrimientos clave durante el desarrollo:**
- Teams v2 usa `substrate.office.com` como proxy de Graph — no hace requests directos a `graph.microsoft.com`
- El `aud` de Microsoft Graph es el GUID `00000003-0000-0000-c000-000000000000`, no la cadena `graph.microsoft.com`
- `TEAMS_NOTIF_TOKEN` y `TEAMS_WEB_TOKEN` son el mismo token (intercambiables)
- MSAL no persiste refresh tokens en IndexedDB — los tokens viven solo en memoria del JS

## Persistencia de datos

| Archivo | Propósito | Permisos |
|---------|-----------|----------|
| `~/.config/teamstui/tokens.env` | Tokens de autenticación | 0600 |
| `~/.config/teamstui/prefs.json` | Preferencias (self-chat MRI cache) | 0600 |
| `~/.config/teamstui/browser-session/` | Sesión de Playwright/Firefox | 0700 |
| `~/Downloads/` | Archivos descargados | - |

## Limitaciones conocidas

### Funcionalidad
- **No hay edición ni eliminación de mensajes** — solo envío de nuevos
- **No hay subida de archivos** — solo descarga y exploración
- **No hay reacciones/likes** en mensajes
- **No hay soporte de hilos/reply** — mensajes son planos
- **No hay búsqueda** de mensajes o archivos
- **No hay autocompletado de @menciones**
- **No hay indicadores de "escribiendo..."**
- **No hay picker de emoji** — hay que escribir Unicode directamente
- **No hay logout** — `Ctrl+C` mata la app inmediatamente

### API
- **Education Assignments**: la API está bloqueada por WAF (TLS fingerprinting). El workspace 4 tiene la UI construida pero no funciona.
- **Presencia "Offline"**: la API de Graph la prohíbe explícitamente.
- **ChatSvc API**: es undocumented/private. Microsoft puede romperla en cualquier momento.
- **Poll de chats no actualiza badges de no leído** — `lastModifiedDateTime` no está disponible en este tenant.

### Plataforma
- **Solo testeado en Arch Linux** — no hay tests automatizados.
- **Dependencia de Playwright/Firefox** para el auth helper — no hay opción CLI/headless.
- **Links clickeables** requieren terminal con soporte OSC 8.

## Dependencias

| Paquete | Versión | Uso |
|---------|---------|-----|
| `charmbracelet/bubbletea` | v1.3.10 | Framework TUI (Elm Architecture) |
| `charmbracelet/lipgloss` | v1.1.0 | Estilos y layout de terminal |
| `charmbracelet/bubbles` | (indirect) | Componentes UI (viewport, textinput) |
| `playwright-community/playwright-go` | (indirect) | Automatización de browser (solo auth helper) |

## Seguridad

- Los tokens se almacenan en texto plano en `~/.config/teamstui/tokens.env`
- No hay encriptación de tokens almacenados
- No hay certificate pinning ni mTLS
- La API ChatSvc es undocumented — puede cambiar sin previo aviso
- El browser session de Playwright guarda cookies de autenticación en disco

## Debug

La app incluye herramientas de debug en `cmd/`:

```bash
# Debug de endpoints de Graph
go run ./cmd/debug-endpoints/

# Debug de Education Assignments
go run ./cmd/debug-assignments/
```

Los dumps de respuesta de API se guardan en `debug_*.json` en la raíz del proyecto.
