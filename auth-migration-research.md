# Investigación para migración de autenticación rápida

## Estado de esta investigación

- Rama de trabajo: `auth-fast-migration-research`.
- Versionado: commits pequeños y frecuentes por cambio coherente; revisar diff y pruebas antes de cada commit; no incluir secretos ni artefactos de runtime.
- Estado de commits: a fecha 2026-09-16 no hay commits propios de la migración; los cambios actuales permanecen sin commit.
- `main` no se modificó.
- La investigación no cambia el código de la aplicación.
- Ya existían sin seguimiento al comenzar: `plan.md` e `internal/graph/diag_test.go`; se conservaron y no se incorporaron a esta investigación.
- Línea base ejecutada: `go test ./...` pasa en todos los paquetes, incluido `cmd/auth-helper` e `internal/graph`.
- No se ejecutaron flujos reales de Microsoft ni se accedió a credenciales.

El objetivo de esta rama es convertir el plan en una secuencia implementable y verificable. No implica afirmar todavía que Graph Explorer pueda renovarse sin interacción en una cuenta concreta.

## Conclusión operativa

La migración debe priorizar el arranque inmediato y la disponibilidad progresiva. La prueba real y aislada de renovación de Graph Explorer es una optimización posterior: puede mejorar el recorrido, pero no debe bloquear los cambios locales de mayor valor. Cuando se ejecute, deberá determinar, con un perfil dedicado y sin exponer secretos, cuál de estos recorridos funciona:

1. access token todavía válido;
2. renovación silenciosa desde la caché MSAL;
3. recuperación mediante la sesión SSO del navegador;
4. selector de cuenta;
5. MFA, consentimiento o interacción obligatoria;
6. sesión irrecuperable.

Si esa prueba es positiva, la implementación debe avanzar en cortes pequeños: estado de credenciales, arranque parcial, helper por lotes con eventos, y recién después renovación durante el uso. Fabric queda diferido y explícito hasta demostrar que la interceptación evita toda escritura.

## 1. Estado actual comprobado

### Regla de commits

La investigación se mantiene en `auth-fast-migration-research` o una rama hija explícita. Cada corte debe quedar en un commit separado y descriptivo después de revisar el estado del árbol, el diff y las pruebas. Los artefactos de autenticación y runtime permanecen fuera de Git.

### Arranque de la aplicación

`main.go` hace lo siguiente:

1. llama a `helpers.KillZombieBrowser()` antes de cualquier autenticación;
2. procesa flags;
3. llama a `auth.GetTokens()`;
4. aborta si faltan `MS_GRAPH_TOKEN`, `TEAMS_WEB_TOKEN` o `TEAMS_COOKIE`;
5. crea un `graph.Client` con ocho credenciales independientes;
6. lanza Bubble Tea.

Por tanto, hoy la TUI no puede abrirse para guiar una recuperación cuando existe una credencial parcial.

`Model.Init()` lanza en un único `tea.Batch` la validación de tokens, equipos, chats, perfil, presencia, notificaciones, asignaciones y timers. La ejecución ya es asíncrona; el problema no es simplemente moverla a background. Falta modelar capacidades independientes y responder a resultados parciales.

### Credenciales y cliente HTTP

`internal/auth/auth.go`:

- lee `~/.config/lazyteams/tokens.env`;
- permite override por variables de entorno;
- descomprime valores con `strconv.Unquote`;
- devuelve ocho strings;
- exige Graph, Web y cookie aunque EDU, Spaces, Fabric y Notif sean opcionales.

`internal/graph/client.go`:

- guarda los ocho tokens como campos mutables del `Client`;
- `ReloadTokens()` vuelve a parsear `tokens.env` con lógica distinta a `auth.loadTokensFile()`;
- actualiza campos individualmente, sin snapshot ni sincronización frente a requests concurrentes;
- no reemplaza valores por vacío, lo que conserva credenciales anteriores pero no representa una generación coherente.

`cmd/auth-helper/saveTokens()`:

- fusiona el archivo existente con los valores no vacíos capturados;
- preserva capturas parciales;
- escribe directamente sobre `tokens.env` con modo `0600`, no mediante temporal + `rename` atómico;
- no guarda vencimiento, cuenta, tenant, recurso, aplicación, permisos ni generación.

### Helper y navegador

El helper usa un perfil persistente común en `browser-session/`.

En el flujo completo:

- crea `tokens` vacío;
- abre Firefox persistente, inicialmente headless salvo primer uso o `--show`;
- registra interceptores de requests para tokens Bearer y cookies;
- `fullRenewal()` espera carga de Teams y ejecuta `captureLoop()`;
- si no captura Graph, cierra el contexto y reabre Firefox visible para Graph Explorer;
- después ofrece Fabric mediante una operación manual de cambio de rol;
- guarda al final lo que haya capturado.

`captureLoop()` contiene esperas temporizadas que son parte del recorrido normal:

- navegación inicial y `sleep` de 5 segundos;
- notificaciones después de 20 segundos;
- EDU después de 40 segundos;
- extracción EDU después de 55 segundos;
- Fabric después de 70 segundos;
- plazo general de 90 segundos.

Además, la renovación de Graph (`--renew graph`) cierra el contexto headless y siempre vuelve a abrir un contexto visible. La renovación `web`/`notif` está agrupada en una función, pero la TUI puede encolarla dos veces porque mantiene tipos separados.

El helper y la TUI matan procesos por ruta de perfil mediante `KillZombieBrowser()`. Eso no es exclusión mutua: puede matar un Firefox legítimamente usado por otra instancia.

### Renovación desde la TUI

`internal/ui/commands.go` lanza un proceso `lazyteams-auth --renew <tipo>`.

- Graph y Fabric no reciben `--headless`.
- Web, Notif y EDU reciben `--headless`.
- la TUI espera la terminación del proceso para producir `tokenRenewedMsg`;
- después llama `ReloadTokens()`;
- la recarga de datos se dispara cuando termina la cola completa, no cuando una función individual queda disponible;
- existen timeouts de 90 segundos para Web/Notif y 180 segundos para EDU; Graph/Fabric no tienen límite automático.

`internal/ui/update.go` clasifica principalmente errores por texto (`is401`, `detectExpiredToken`). Un 403 de Fabric se trata de forma particular sólo en la comprobación dedicada; otros 403/404 no están modelados como estados semánticos de proveedor.

## 2. Matriz de capacidades observada

| Capacidad | Credencial principal | Código observado | Comportamiento deseado |
|---|---|---|---|
| Equipos y canales | Graph | `teams_api.go` | Disponible apenas Graph esté listo. |
| Identidad y directorio | Graph | `client.go`, `chats_api.go` | No bloquear Web, EDU o Fabric. |
| Chats y mensajes | Graph para navegación + Web | `chats_api.go`, `messages_api.go` | Habilitar mensajería con Graph/Web, sin esperar EDU. |
| Actividad | Notif | `activity_api.go` | Cargar de forma independiente. |
| Imágenes inline | Web/AMS | `ams_api.go`, `download_api.go` | No depender de Graph para la captura de medios. |
| Archivos de canales | Graph | `files_api.go`, `upload_api.go` | Habilitar con Graph; uploads no deben reintentarse automáticamente tras respuesta perdida. |
| Assignments | EDU + cookie | `assignments_api.go` | Pendiente si no aplica o EDU falla; no bloquear chats. |
| Administración de equipos/canales | Graph, Spaces o Web según operación | `teams_api.go` | Informar la dependencia concreta. |
| Miembros de canales privados | Fabric + Graph/Web para resolución | `teams_api.go` | Solicitar Fabric sólo al usar la operación. |

La matriz implica que `loading` no puede representar un requisito global de autenticación. Deben existir estados por módulo y errores por dependencia.

## 3. Investigación externa relevante

### Graph Explorer y MSAL

El código público de Graph Explorer revisado en el commit fijado por `plan.md` muestra:

- MSAL Browser 4.5.1;
- `BrowserCacheLocation.LocalStorage`;
- `acquireTokenSilent()` como primer intento;
- `forceRefresh` como segundo intento silencioso en ciertos errores;
- fallback interactivo con `prompt: 'select_account'`;
- autoridad configurable por tenant (`common` por defecto en ese flujo);
- selección de cuenta a partir de las cuentas cacheadas.

El bundle desplegado no debe tratarse como una API estable. No conviene depender de `window.msalInstance`, ni asumir que una instancia MSAL está expuesta globalmente. La captura más conservadora es observar la request real a Graph generada por el flujo nativo de Graph Explorer.

Fuentes primarias:

- [MSAL app de Graph Explorer](https://github.com/microsoftgraph/microsoft-graph-explorer-v4/blob/48604f7b000d60220a8eb00836024fee3c1bb118/src/modules/authentication/msal-app.ts)
- [AuthenticationWrapper de Graph Explorer](https://github.com/microsoftgraph/microsoft-graph-explorer-v4/blob/48604f7b000d60220a8eb00836024fee3c1bb118/src/modules/authentication/AuthenticationWrapper.ts)

### Persistencia de la caché MSAL

La documentación actual de MSAL indica:

- `acquireTokenSilent()` consulta primero la caché y renueva si hace falta;
- `localStorage` ofrece persistencia entre ventanas y mejor UX que `sessionStorage`;
- desde MSAL v4, los artefactos de `localStorage` pueden estar cifrados con AES-GCM;
- la clave depende de la cookie `msal.cache.encryption`;
- esa cookie se elimina al cerrar la instancia del navegador, salvo que el usuario haya elegido "Keep me signed in";
- la caché puede quedar inutilizable aunque la sesión del proveedor todavía permita reautenticar sin pedir credenciales.

Consecuencia: conservar el directorio del perfil no garantiza conservar toda la caché MSAL. El experimento debe distinguir caché recuperable de SSO recuperable y de interacción requerida.

Fuentes:

- [Caching in MSAL.js](https://learn.microsoft.com/en-us/entra/msal/javascript/browser/caching)
- [Manage token lifetimes](https://learn.microsoft.com/en-us/entra/msal/javascript/browser/token-lifetimes)

### Ventana de refresh tokens SPA

Microsoft documenta que los refresh tokens entregados a SPAs tienen normalmente una ventana no deslizante de 24 horas. Se reemplazan al usarse, pero la ventana original no se extiende indefinidamente. Al vencer o revocarse, hace falta un nuevo flujo de autorización; una sesión SSO activa puede evitar que el usuario vuelva a escribir sus credenciales, pero no elimina todas las interacciones posibles.

Esto descarta diseñar la migración como "guardar un refresh token y listo".

Fuente: [Refresh tokens in the Microsoft identity platform](https://learn.microsoft.com/en-us/entra/identity-platform/refresh-tokens)

### Consentimiento y permisos

Los permisos delegados dependen de dos autorizaciones: la del cliente y la del usuario. Un token válido para Graph no implica que tenga todos los scopes o que provenga del cliente que ya tiene el consentimiento útil para LazyTeams. El plan debe conservar procedencia, cuenta, tenant y permisos conocidos; `/me` respondiendo no es una validación suficiente.

Fuente: [Permissions and consent overview](https://learn.microsoft.com/en-us/entra/identity-platform/permissions-consent-overview)

### Interceptación para investigar Fabric

La API de routing de Playwright permite que cada request coincidente quede detenida hasta llamar a `Continue`, `Fulfill` o `Abort`. La documentación advierte que `BrowserContext.Route` no intercepta requests manejadas por Service Workers y recomienda bloquearlos en las opciones del contexto cuando la interceptación sea necesaria.

La prueba de Fabric debe usar un contexto separado con Service Workers bloqueados, coincidir sólo con la operación de escritura objetivo y abortar antes de que llegue al servidor. Si el recorrido real usa Service Worker o una request no cubierta, la hipótesis queda descartada.

Fuentes:

- [BrowserContext route](https://playwright.dev/docs/api/class-browsercontext#browser-context-route)
- Firma disponible en el módulo Go: `BrowserContext.Route(url any, handler routeHandler, times ...int) error`.

## 4. Procedimiento recomendado por etapas

### Etapa 0 — Seguridad y aislamiento

1. Mantener todo trabajo experimental en `auth-fast-migration-research` o ramas hijas.
2. Usar un laboratorio persistente dedicado fuera del repositorio (por ejemplo `~/lazyteams-auth-lab/`) con `runtime/home`, perfil Firefox, caché Playwright y logs protegidos.
3. No copiar `tokens.env`, `browser-session/` ni `prefs.json` al repositorio; el probe y su documentación pueden versionarse cuando corresponda.
4. Para pruebas reales, usar un perfil de Firefox dedicado y una cuenta autorizada; nunca el perfil personal ni una sesión de producción compartida. El modo visible queda disponible para interacción manual.
5. Usar un wrapper que inyecte las rutas de configuración y caché; `/tmp` queda reservado para artefactos efímeros o evidencia histórica.
6. Registrar únicamente eventos, estados, tiempos y códigos HTTP. Nunca tokens, cookies, headers de autorización, claims completos ni contenido de respuestas.
7. No llamar `KillZombieBrowser()` desde un experimento que no sea propietario del perfil.

### Etapa 1 — Prueba de Graph Explorer y medición

Esta etapa no debe modificar todavía la semántica de la TUI.

Instrumentación mínima, sin secretos:

- instante de inicio del helper;
- creación del contexto headless/visible;
- URL y duración de cada navegación relevante;
- primera request de Graph observada, sólo host/ruta, status y clasificación;
- estado de autenticación observado: `authenticated`, `account_picker`, `login`, `mfa`, `consent`, `unknown`;
- instante de captura del token y longitud solamente, si hace falta confirmar que existe;
- cierre limpio del contexto y motivo de salida.

Protocolo:

1. Crear el perfil dedicado.
2. Ejecutar una autenticación inicial visible en Graph Explorer y completar sólo el consentimiento autorizado.
3. Ejecutar una adquisición dentro de esa misma sesión y confirmar si se produce request a Graph sin interacción adicional.
4. Cerrar Firefox limpiamente.
5. Reabrir el mismo perfil headless y cargar Graph Explorer.
6. Activar el recorrido nativo de Graph Explorer; preferir una consulta mínima y no mutante.
7. Clasificar el resultado según los seis estados anteriores.
8. Repetir después de que venza el access token y en otra sesión del día siguiente.
9. Repetir con un tenant universitario representativo, si está autorizado.
10. Comparar headless y visible; no inferir que uno funciona porque el otro funciona.

Criterio de salida:

- evidencia de si la caché MSAL persiste y puede renovar;
- evidencia separada de si SSO recupera la sesión;
- error concreto cuando se exige interacción;
- medición de cuánto del tiempo actual son sleeps, navegación o servidor;
- ningún token ni cookie en logs.

### Etapa 2 — Modelo de credenciales y ownership

Cambios futuros concentrados en `internal/auth`, `internal/graph/client.go`, `internal/helpers` y el lifecycle del helper.

Diseño propuesto:

```text
CredentialSet
  Generation
  AcquiredAt
  ExpiresAt (opcional y sólo si la adquisición lo informa)
  Account
  Tenant
  Source / Resource
  Permissions (conocidos)
  Graph, Web, Notif, EDU, Cookie, Spaces, Fabric
```

Reglas:

- un snapshot completo representa una generación coherente;
- una adquisición parcial genera un delta identificado por generación y proveedor;
- la lectura de cada request obtiene un snapshot estable;
- el reemplazo publica un snapshot nuevo de una vez;
- no se usa un JWT decodificado como única fuente de vencimiento;
- se conserva la procedencia y no se reemplaza una credencial válida por otra de cuenta, tenant, recurso o permisos incompatibles;
- se unifica el parser de `tokens.env` para carga inicial y recarga;
- la persistencia usa archivo temporal en el mismo directorio, `fsync` cuando corresponda, `chmod 0600` y `rename` atómico;
- los metadatos no contienen secretos en logs.

Ownership del perfil:

- adquirir un lock por `browser-session/` antes de abrir Playwright;
- registrar propietario, PID y generación de lock;
- liberar el lock en cierre normal;
- considerar huérfano sólo un lock cuyo propietario ya no existe y cuya identidad de proceso pueda verificarse;
- quitar el `pkill` global como camino normal;
- unificar cancelación: la TUI solicita terminación al helper, el helper cierra contextos y Playwright, y sólo después se libera el lock.

Criterio de salida: dos helpers concurrentes no compiten por el perfil, una cancelación no deja Firefox huérfano y una lectura concurrente nunca observa campos mezclados.

### Etapa 3 — Arranque progresivo

Archivos principales: `main.go`, `internal/auth/auth.go`, `internal/ui/model.go`, `commands.go`, `update.go`, `update_data.go`.

Secuencia propuesta:

1. `GetTokens()` devuelve un snapshot parcial y estados por proveedor, en vez de abortar por Graph/Web/cookie faltantes.
2. `main.go` abre la TUI con lo que existe; el nombre de usuario es opcional hasta que Graph esté disponible.
3. Cada comando comprueba su dependencia antes de enviar una request; la ausencia produce estado de capacidad, no un error de red ambiguo.
4. Graph listo dispara perfil, equipos y chats.
5. Web listo habilita mensajes, reacciones, imágenes y operaciones correspondientes.
6. Notif listo carga actividad.
7. EDU listo carga Assignments; si la cuenta no aplica, queda `not_applicable` y no reintenta indefinidamente.
8. Fabric sólo se solicita al entrar en la operación que lo necesita.
9. La UI muestra disponibilidad por módulo y no una falsa barrera "todo listo".

No reutilizar `loading` como barrera global. Mantener flags o estados explícitos por función, con errores diferenciados:

- `ready`;
- `missing`;
- `expired_or_revoked`;
- `interaction_required`;
- `permission_denied`;
- `network_unavailable`;
- `not_applicable`;
- `unknown`.

### Etapa 4 — Helper por lotes y eventos incrementales

El helper debe conservar un único navegador por lote y devolver resultados aprovechables antes de cerrar el lote.

Contrato recomendado entre helper y TUI:

- stdout del helper: protocolo interno de líneas JSON sin secretos;
- stderr o archivo de debug separado y siempre sanitizado;
- eventos mínimos: `started`, `credential_ready`, `interaction_required`, `provider_failed`, `completed`, `cancelled`;
- cada evento lleva proveedor, generación, estado, duración y error clasificado;
- nunca lleva el token ni cookies;
- la persistencia atómica se completa antes de publicar `credential_ready`;
- la TUI aplica el snapshot/delta y habilita sólo las funciones afectadas.

Agrupación:

- `web` y `notif` son un único trabajo de adquisición;
- Spaces se captura si aparece en ese recorrido, pero no bloquea chat;
- EDU se procesa después del núcleo de mensajería y puede finalizar en `not_applicable`;
- Graph headless se intenta antes de abrir una ventana visible;
- Fabric queda fuera del lote normal salvo que el usuario lo solicite.

Reemplazo de sleeps por señales:

- navegación completada o URL esperada;
- evento de request relevante;
- aparición de la pantalla necesaria;
- adquisición finalizada;
- pantalla que exige intervención;
- timeout sólo como límite de fallo.

La TUI no debe esperar `cmd.Wait()` para enterarse de un `credential_ready`; debe consumir el pipe de eventos y mantener el proceso registrado para cancelación.

### Etapa 5 — Graph silencioso primero

Orden por intento:

1. usar credencial vigente guardada;
2. abrir el perfil dedicado headless;
3. cargar Graph Explorer y usar su flujo nativo;
4. capturar sólo un token de request cuyo host, audiencia, cuenta/tenant y permisos conocidos sean compatibles;
5. si MSAL exige interacción, emitir `interaction_required` y abrir visible sólo entonces;
6. persistir la nueva credencial atómicamente;
7. cerrar contexto y liberar ownership.

Validaciones mínimas antes de reemplazar Graph:

- request directa a `graph.microsoft.com`;
- audiencia compatible con Graph;
- cuenta y tenant compatibles con el snapshot vigente cuando ambos se conocen;
- procedencia esperada del flujo de Graph Explorer;
- scopes/permisos requeridos para los casos que LazyTeams usa;
- no aceptar un token de SharePoint, Substrate u otra cuenta sólo porque `/me` responde.

La validación del token no debe convertirse en una dependencia exclusiva de decodificar JWT. Los metadatos de adquisición son preferibles; la decodificación se limita a señales auxiliares y nunca trata el access token como formato contractual.

### Etapa 6 — Renovación durante el uso

Después de estabilizar el arranque:

- renovar de forma anticipada sólo credenciales próximas a vencer cuando exista un vencimiento confiable;
- si no existe vencimiento informado, renovar por respuesta real del proveedor o por una política acotada, no por una consulta duplicada global;
- pausar/replanificar después de suspensión o cambio de conectividad;
- no renovar indefinidamente ante permiso insuficiente;
- no repetir automáticamente envíos, uploads o cambios cuya respuesta se perdió;
- mantener drafts y operaciones en curso fuera de la ruta de reemplazo de credenciales.

### Etapa 7 — Fabric y validación final

Producto:

- Fabric no bloquea el arranque;
- la administración privada muestra una dependencia explícita;
- la captura se solicita sólo al usarla;
- el flujo manual actual se conserva como fallback.

Experimento de interceptación:

1. usar un contexto de prueba separado;
2. bloquear Service Workers;
3. identificar la request original de cambio de rol con método, ruta y payload, sin registrar secretos;
4. instalar route antes de navegar;
5. abortar la request objetivo antes de enviarla;
6. observar si aparece el token necesario en la request abortada o en una respuesta permitida;
7. verificar en auditoría del tenant que no cambió rol ni membresía;
8. repetir con reintentos, navegación nueva y múltiples páginas;
9. descartar la hipótesis si alguna escritura llega al servidor o si el token sólo aparece después de una escritura permitida.

## 5. Criterios de aceptación convertidos en pruebas

| Escenario | Prueba y evidencia |
|---|---|
| Credenciales vigentes | Lanzar la TUI con un `HOME` de prueba que tenga credenciales válidas; contar procesos de navegador y verificar cero. |
| Graph vencido con sesión recuperable | Perfil dedicado; access token vencido; observar `credential_ready` sin introducir credenciales manuales. |
| Graph listo y EDU fallando | Simular EDU 401/timeout; verificar equipos/chats renderizados y EDU en estado de error. |
| Cuenta sin Education | Respuesta de proveedor que indique no aplicabilidad; verificar que no hay loop ni espera indefinida. |
| Web y Notif vencidos | Provocar ambos estados; verificar un solo helper/lote y una sola navegación de adquisición. |
| Wi-Fi interrumpido | Cortar conectividad durante renovación; verificar que no se elimina el perfil ni se anuncia éxito. |
| Salida durante renovación | Enviar cancelación; verificar cierre del contexto y ausencia de proceso hijo huérfano. |
| Dos instancias | Ejecutar dos procesos con el mismo perfil; uno adquiere lock y el otro informa ocupado sin matar al primero. |
| Token de otra cuenta/permisos | Presentar una captura incompatible; verificar que el snapshot válido se conserva. |
| MFA/consentimiento | Forzar `interaction_required`; verificar un único flujo visible y un mensaje concreto. |
| Mensaje/upload en curso | Perder la respuesta y renovar; verificar que no se reenvía automáticamente ni se pierde el borrador. |
| Fabric sin escritura | Interceptar y abortar; verificar cero cambios en el tenant y token capturado sólo si el recorrido lo permite. |

## 6. Pruebas permanentes que sí justifican conservar

No conviene llenar la suite con tests que sólo comprueben asignación de campos. Las pruebas de mayor valor son:

- parser único: valores quoted/escapados, compatibilidad legacy y archivo incompleto;
- snapshot: reemplazo coherente y rechazo de cuenta/tenant/procedencia incompatibles;
- persistencia: archivo nunca queda truncado tras un fallo de escritura;
- lock: exclusión, liberación y propietario huérfano;
- clasificación: distinguir vencimiento, permiso, red y no aplicabilidad;
- transición UI: Graph listo carga equipos aunque EDU falle;
- deduplicación: Web/Notif produce un único trabajo;
- eventos: `credential_ready` habilita el módulo antes de `completed`;
- cancelación: no se reintenta una operación mutante ni queda helper registrado.

Los escenarios reales de renovación de Graph y Fabric no quedan demostrados por mocks; requieren la sesión Microsoft autorizada y evidencia de red/estado sin secretos.

### Protocolo de encargos al usuario para pruebas reales

Los agentes deben pedir al usuario que ejecute las pruebas interactivas: binarios, Firefox, sesión Microsoft, TUI y cualquier flujo que genere tráfico de tenant. Deben entregar un encargo acotado con laboratorio, comando, flags, escenario, acción visible y condición de finalización. No deben ejecutar ni inferir el resultado en lugar del usuario.

La devolución requerida es: comando y timestamps; código de salida; estado final del binario y del producto; pasos/interacción observados (login, MFA, consentimiento, pantalla inesperada); errores, timeout o cancelación; cierre y procesos huérfanos; ruta de log sanitizado; estados por capacidad para la TUI; confirmación de snapshot/`tokens.env` sin cambios; y auditoría antes/después reducida a `unchanged=true/false` y códigos clasificados. No se aceptan tokens, cookies, Authorization, claims, perfiles, URLs completas, payloads, contenido de página ni capturas sensibles.

En 7B la devolución debe afirmar por separado: route instalada antes de navegar, request exacta abortada, `requestfailed` sin `response`, ausencia de mutaciones desconocidas, auditoría sin cambios y estado `write_candidate_unverified` o `rejected`. Incluso una corrida exitosa no autoriza persistir el candidato: la asociación de la request requiere la evidencia no mutante de 7C.

La autorización debe ser explícita y específica. “Continuar”, “probar” o autorizar tests locales no habilita navegador, tenant ni tráfico Microsoft; debe mencionar expresamente esos recursos y acciones.

## 7. Orden concreto de implementación posterior

1. **Introducir snapshot y parser único.** Mantener temporalmente la API actual mediante adaptación interna sólo durante el corte; eliminarla cuando todos los callers migren.
2. **Implementar lock de perfil y lifecycle único.** Retirar el kill preventivo como mecanismo normal.
3. **Permitir arranque parcial y estados de capacidad.** Separar disponibilidad de módulos de `loading` global.
4. **Convertir `web`/`notif` en una adquisición única.** Persistir y publicar resultado coherente.
5. **Agregar eventos privados del helper.** Consumirlos desde la TUI y habilitar funciones incrementalmente.
6. **Eliminar sleeps obligatorios.** Mantener límites de fallo medibles.
7. **Cambiar Graph a headless-first.** Visible sólo en `interaction_required`, como optimización posterior.
8. **Agregar renovación durante uso.** Sólo después de estabilizar las transiciones anteriores.
9. **Investigar Fabric sin escritura.** Mantener fallback manual si no se demuestra seguridad.
10. **Ejecutar matriz de aceptación, actualizar README/seguridad y revisar la rama antes de integrar.**

## 8. Riesgos y decisiones que no deben ocultarse

- La persistencia de `localStorage` puede depender de `msal.cache.encryption` y de KMSI; no hay garantía de renovación silenciosa diaria.
- Los permisos de Graph Explorer son específicos del cliente y del tenant; otra credencial válida puede ser inútil para LazyTeams.
- Las APIs internas de Teams, ChatSvc, EDU, Spaces y Fabric pueden cambiar sin compatibilidad.
- Un 403/404 no significa universalmente token vencido.
- La interceptación de Playwright no cubre automáticamente Service Workers.
- Guardar más metadatos mejora validación, pero aumenta el material sensible persistido; deben limitarse a lo necesario y protegerse igual que `tokens.env`.
- Un lock mal diseñado puede bloquear recuperaciones legítimas; un kill global puede matar sesiones legítimas. Se elige exclusión explícita y recuperación comprobada.
- El helper como proceso hijo simplifica el aislamiento de Playwright, pero exige un protocolo de eventos y una cancelación correcta. No se debe fingir incrementalidad leyendo sólo el resultado final.

## 9. Resultado de la investigación

El plan es técnicamente abordable sin reemplazar la arquitectura híbrida ni introducir una aplicación OAuth nueva. El primer entregable de código debe ser el núcleo de arranque rápido: TUI inmediata, snapshots/ownership, capacidades parciales, coordinación incremental y eliminación de esperas artificiales. La medición de Graph Explorer puede continuar después como optimización; Fabric queda diferido.

Hasta demostrar la prueba real, no deben prometerse:

- cero interacción para todos los tenants;
- renovación silenciosa indefinida;
- captura automática segura de Fabric;
- equivalencia entre un token Graph cualquiera y el token/permisos de Graph Explorer.
