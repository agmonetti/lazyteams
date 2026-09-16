# Runbook operativo: migración de autenticación

## Estado del documento

- **Estado:** operativo; Etapa 0 cerrada; Etapa 1 ejecutada y su gate no aprobado; Etapas 2, 3 y 4 corregidas, validadas y aprobadas por revisión Sol; Etapa 5 implementada localmente, pero su primera revisión formal Sol rechazó el gate en L092.
- **Migración:** núcleo de Etapas 2 y 3 implementado; el helper por lotes, protocolo incremental y lifecycle de Etapa 4 quedaron aprobados formalmente en L088; el corte local de Graph headless-first fue implementado y validado localmente en L091; su gate formal sigue pendiente.
- **Etapa actual:** Etapa 5 autorizada explícitamente el 2026-09-16 en L089. Sol cerró el contrato técnico previo en L090; la implementación y las pruebas corresponden a Luna. No se autorizó ninguna adquisición real nueva ni trabajo de Etapas 6–7.
- **Documentos fuente:** `plan.md` y `auth-migration-research.md`.
- **Alcance:** permitir abrir y empezar con consumo liviano, mejorar el arranque, la renovación y la coordinación de credenciales sin reemplazar la arquitectura híbrida actual.
- **Fuera de alcance inicial:** nueva aplicación OAuth, reescritura de la TUI, WebSockets, renderizado y captura automática de Fabric sin prueba de seguridad.

### Regla de versionado durante la migración

- Trabajar siempre en la rama `auth-fast-migration-research` o en una rama hija creada explícitamente para esta investigación.
- Hacer commits pequeños y frecuentes, cada uno con un cambio coherente y un mensaje descriptivo.
- Antes de cada commit revisar `git status`, el diff y las pruebas pertinentes; no mezclar cambios ajenos a la migración.
- Nunca commitear `tokens.env`, cookies, perfiles Firefox, cachés Playwright, logs sensibles ni ningún secreto.
- A fecha 2026-09-16 no hay commits propios de la migración; los cambios actuales permanecen en el working tree hasta su revisión.

> Este archivo es el contrato operativo de la migración. Cada etapa debe respetar su objetivo, sus invariantes, sus criterios de salida y sus evidencias. No se avanza por intuición ni porque compile: se avanza cuando el gate de la etapa quedó demostrado.

### Prioridad de producto

El criterio principal es que el usuario pueda ejecutar `lazyteams`, ver la TUI y usar la primera capacidad disponible sin esperar la autenticación completa. La renovación silenciosa de Graph es una optimización; `interaction_required` es un fallback válido. No se mantendrá un navegador, daemon o servicio permanente.

## Política de modelos

### Available models

| Model | Limit | Assigned work |
| --- | --- | --- |
| GPT-5.6 Sol | Limited | High-complexity reasoning: architecture, planning, deep debugging, security analysis, repository-wide reviews, ambiguous tasks, and final validation. |
| GPT-5.6 Luna | Limited | Fast execution: implementation, refactoring, tests, documentation edits, repetitive multi-file changes, hotfixes, and well-defined tasks. |
| gemini-3.8-flash (Gemini) | Unlimited | Mechanical tasks, repetitive code, JSON schemas, fixtures, examples, web research, CVE lookups, and boilerplate. |

### How model switching works

The active agent is one model and cannot switch itself. When a task requires a different model, the active agent must stop and tell the user explicitly before doing anything else.

The required format is:

```text
SWITCH REQUIRED

Model needed: [Sol / Luna / Gemini]

Reason: [one sentence]

Task to resume: [what to ask the next model to do]

Pending work this session: [what was completed so far]
```

The active agent must not attempt the task, summarize it or partially complete it after determining that a switch is required. It must stop and wait.

### When to stop and request a switch

Request **Sol** when:

- the task requires evaluating architectural tradeoffs;
- the task requires checking consistency across multiple architecture documents;
- the task requires deep debugging or root-cause analysis;
- the task involves security boundaries or threat modeling;
- the task requires repository-wide analysis;
- a decision is about to be made that is not already documented;
- the task is final validation or review of a substantial implementation;
- the task is ambiguous and requires significant reasoning before execution.

Request **Luna** when:

- implementing Go code from an already-defined plan;
- refactoring existing code without changing architecture;
- writing or updating tests;
- editing an existing document with clear expected changes;
- applying a focused hotfix with clear expected output;
- performing repetitive multi-file changes;
- performing a routine repository task with well-defined acceptance criteria.

Stay on **Gemini** when:

- writing JSON schemas;
- writing fixture examples;
- writing boilerplate Go structs with no logic;
- performing web research or CVE lookups;
- generating repetitive examples;
- performing another mechanical task that does not require cross-document reasoning.

For substantial tasks spanning reasoning and implementation:

1. Sol analyzes, plans and resolves ambiguity.
2. Luna implements the approved plan.
3. Sol performs final validation when the change is architecture-sensitive, security-sensitive or repository-wide.

When in doubt, stop and ask the user which model to use. Never guess.

## 1. Objetivo y resultado esperado

LazyTeams debe poder abrir la TUI con las credenciales disponibles, usar cada capacidad apenas su dependencia esté lista y pedir interacción sólo cuando Microsoft o el tenant la hagan necesaria.

El resultado no promete autenticación silenciosa indefinida. Debe distinguir entre:

- trabajo disponible ahora;
- renovación silenciosa posible;
- interacción requerida;
- permiso insuficiente;
- red no disponible;
- capacidad no aplicable;
- estado desconocido.

### Invariantes no negociables

1. Nunca imprimir ni persistir tokens, cookies, headers `Authorization`, claims completos o contenido sensible de respuestas.
2. Nunca reemplazar una credencial útil por otra de cuenta, tenant, recurso, procedencia o permisos incompatibles.
3. Nunca abrir dos navegadores sobre el mismo `browser-session/`.
4. Nunca matar un navegador legítimo con `pkill` global como mecanismo normal de coordinación.
5. Nunca bloquear equipos o mensajería esperando EDU, Spaces, Notif o Fabric.
6. Nunca repetir automáticamente envíos, uploads o cambios mutantes cuya respuesta se haya perdido.
7. Nunca anunciar éxito si la persistencia o la adquisición no terminó de forma coherente.
8. Nunca tratar un JWT decodificado como la única fuente contractual de expiración o identidad.
9. Los timeouts son límites de fallo; no son pasos normales del recorrido.
10. Fabric sólo puede automatizarse si se demuestra que ninguna escritura llega al servidor.

## 2. Arquitectura actual relevante

| Área | Ubicación | Situación observada |
| --- | --- | --- |
| Entrada | `main.go` | Mata procesos por ruta de perfil, exige Graph/Web/cookie y sólo después abre la TUI. |
| Credenciales | `internal/auth/auth.go` | Lee `tokens.env`, permite variables de entorno y actualmente exige credenciales parciales mínimas. |
| Cliente | `internal/graph/client.go` | Mantiene ocho credenciales mutables y recarga con un parser distinto. |
| Helper | `cmd/auth-helper/main.go` | Captura tokens mediante un perfil persistente de Firefox y usa sleeps temporizados. |
| TUI | `internal/ui/commands.go`, `update.go`, `update_data.go`, `model.go` | Renueva mediante proceso hijo, espera su finalización y recarga datos al vaciar la cola. |
| Perfil | `internal/helpers/ownership*.go`, `cmd/auth-helper/main.go` | Ownership mediante lock asesor del SO; no se usa `KillZombieBrowser()` como coordinación. |
| Persistencia | `internal/auth/auth.go` | Parser único, metadata versionada y reemplazo atómico de `tokens.env`; helper delega la persistencia. |

### Dependencias funcionales

| Capacidad | Dependencias principales | Comportamiento objetivo |
| --- | --- | --- |
| Equipos, canales, directorio | Graph | Disponible al quedar Graph listo. |
| Navegación y mensajería | Graph + Web | Disponible sin esperar EDU/Fabric. |
| Actividad | Notif | Se carga de manera independiente. |
| Imágenes inline | Web/AMS | No depende de Graph para la captura de medios. |
| Archivos de canales | Graph | Disponible con Graph; no repetir uploads ambiguos. |
| Assignments | EDU + cookie | `not_applicable` o error acotado; nunca loop infinito. |
| Administración general | Graph, Spaces o Web según operación | Informar la dependencia concreta. |
| Miembros de canales privados | Fabric + Graph/Web | Solicitar Fabric sólo al entrar en esa operación. |

## 3. Orden y gates de ejecución

```text
Etapa 0  Aislamiento y seguridad
   ↓
Etapa 1  Prueba real de Graph Explorer y medición
   ↓
Etapa 2  Snapshot de credenciales, parser único y ownership
   ↓
Etapa 3  Arranque progresivo y estados de capacidad
   ↓
Etapa 4  Helper por lotes y eventos incrementales
   ↓
Etapa 5  Graph headless-first
   ↓
Etapa 6  Renovación durante el uso
   ↓
Etapa 7  Fabric y validación final
```

Una etapa puede implementarse en commits pequeños, pero su gate debe quedar documentado antes de iniciar la siguiente. D002 bloquea sólo la optimización Graph headless, no el núcleo de arranque, snapshots, ownership, capacidades parciales ni coordinación incremental. Si una hipótesis falla, se registra en `auth-migration-lessons.md`, se actualiza este runbook y se elige el fallback explícito; no se oculta el fallo con reintentos indefinidos.

---

## Etapa 0 — Aislamiento y seguridad

### Objetivo

Preparar un entorno reproducible y seguro para investigar Graph Explorer sin exponer credenciales ni interferir con una sesión real.

### Acciones

- Trabajar en la rama de investigación o una rama hija.
- Usar un laboratorio persistente dedicado, fuera del repositorio, con `HOME`/directorio de configuración inyectable para pruebas automatizadas y manuales.
- Mantener en el laboratorio el probe, `runtime/home`, perfil Firefox, caché Playwright y logs sanitizados.
- Permitir Firefox visible para que el usuario complete interacciones autorizadas.
- No copiar `tokens.env`, `browser-session/` ni `prefs.json` al repositorio.
- Confirmar que el experimento nunca llama `KillZombieBrowser()` sobre un perfil que no posee.
- Definir logging sanitizado: estados, tiempos, hosts/rutas permitidos, códigos HTTP y longitudes; nunca secretos.
- Registrar versión de binario, navegador, Playwright y sistema operativo.

### Gate de salida

- Entorno aislado creado.
- Perfil experimental identificado y autorizado.
- Logging revisado para demostrar ausencia de secretos.
- Procedimiento de limpieza definido.
- Sin cambios de código funcional todavía.

### Evidencia requerida

- Ruta temporal o identificador no sensible del entorno.
- Lista de eventos que se registrarán.
- Confirmación de que el perfil productivo no fue usado.
- Entrada correspondiente en `auth-migration-lessons.md`.

### Gate documentado — 2026-09-15

- Entorno `stage0-VhVrpE` creado fuera del repositorio en `/tmp/lazyteams-auth-stage0.VhVrpE`, con permisos `0700`.
- `HOME`, `USERPROFILE`, configuración, perfil Firefox, caché Playwright y logs quedaron aislados.
- Perfil experimental identificado; el perfil productivo no se abrió, leyó ni copió.
- Logging permitido limitado a eventos, estados, tiempos, host/ruta normalizados, códigos HTTP y longitudes opcionales. `--debug` actual quedó excluido porque puede exponer URLs y contenido de página.
- Limpieza definida: cierre limpio, eliminación exclusiva de la raíz temporal y confirmación posterior; nunca `pkill` global.
- Versiones registradas en L006. El navegador efectivo de Etapa 1 se registrará después de instalarlo dentro de la caché aislada y antes de navegar.
- No se creó probe, no se abrió Graph Explorer y no se modificó código funcional.
- Evidencia completa: L004–L007 en `auth-migration-lessons.md` y `/tmp/lazyteams-auth-stage0.VhVrpE/environment.txt`.
- **Decisión del gate:** Etapa 0 aprobada; Etapa 1 no autorizada.

### Procedimiento de laboratorio persistente — decisión transversal 2026-09-15

- `/tmp` queda como ubicación histórica del experimento ya ejecutado, no como requisito operativo.
- Las nuevas pruebas usarán un directorio persistente fuera del repositorio (por ejemplo `~/lazyteams-auth-lab/`) con permisos `0700`.
- El perfil Firefox, `HOME`, caché Playwright, `tokens.env` y cookies permanecerán dentro de la raíz protegida del laboratorio y fuera del repositorio.
- Un wrapper configurará internamente `HOME`, `USERPROFILE`, `PLAYWRIGHT_BROWSERS_PATH` y `LAZYTEAMS_PROBE_PROFILE`.
- Crear, mover o copiar el perfil y ejecutar una nueva adquisición siguen requiriendo autorización explícita de la etapa correspondiente.

---

## Etapa 1 — Prueba real de Graph Explorer y medición

### Objetivo

Determinar empíricamente si la caché MSAL, la sesión SSO o ambas permiten renovar Graph sin interacción después de cerrar y reabrir Firefox.

### Estados que deben distinguirse

`authenticated`, `account_picker`, `login`, `mfa`, `consent`, `interaction_required`, `unknown`.

### Protocolo

1. Autenticar Graph Explorer en el perfil dedicado con consentimiento autorizado.
2. Ejecutar una consulta mínima y no mutante dentro de la misma sesión.
3. Cerrar Firefox limpiamente.
4. Reabrir el mismo perfil en headless.
5. Cargar Graph Explorer y activar su flujo nativo.
6. Observar la request real a Graph: host/ruta, estado y clasificación, nunca el token.
7. Repetir tras vencer el access token y al día siguiente.
8. Comparar headless y visible.
9. Repetir con un tenant universitario representativo sólo si existe autorización.

### Qué medir

- navegación y duración por etapa;
- sleeps actuales frente a tiempo de servidor;
- primera request Graph observada;
- estado de autenticación;
- momento de disponibilidad del token y longitud solamente;
- cierre limpio, cancelación y motivo de salida.

### Gate de salida

Debe existir una conclusión separada para:

- access token todavía válido;
- renovación desde caché MSAL;
- recuperación por SSO;
- selector de cuenta;
- MFA/consentimiento obligatorio;
- sesión irrecuperable.

### Decisión

Si Graph Explorer no permite renovación silenciosa en un escenario, el producto debe emitir `interaction_required` y abrir visible sólo cuando corresponda. No se debe diseñar una promesa de cero interacción universal.

### Resultado actualizado — 2026-09-15

- Visible: consulta no mutante `GET /v1.0/me` observada en `graph.microsoft.com`; credencial presente (longitud únicamente: `3280`); estado `authenticated`; cierre limpio.
- Headless tras el cierre y reapertura del mismo perfil: el probe corregido clasificó `login`; el control nativo de consulta se activó, pero no se observó request Graph dentro del límite de 15 segundos; cierre limpio.
- Conclusiones separadas: access token/adquisición visible confirmada; renovación desde caché MSAL no demostrada; recuperación SSO no demostrada; selector de cuenta no observado; MFA no observado; consentimiento no observado; sesión irrecuperable no demostrada.
- Corrección de evidencia: el `unknown` de L009 fue inconcluso porque los selectores anteriores nunca emitieron `query_triggered`; L010 documenta esa corrección y L012 documenta la repetición headless.
- Repetición temporal headless a las `2026-09-15T19:09:31Z`: volvió a clasificar `login`, activó el control nativo y no observó request Graph durante 15 segundos; cierre limpio. Evidencia sanitizada en `/tmp/lazyteams-auth-stage0.VhVrpE/logs/stage1-headless-temporal-20260915T190931Z.jsonl`.
- El escenario de vencimiento natural no pudo aislarse: el perfil ya clasificó `login` y no existe un access token observable ni un vencimiento informado que permita demostrar que el token visible anterior venció naturalmente. No se alteró el reloj, el perfil ni ninguna credencial.
- Repetición headless después de la espera supervisada de `3605 s`: a las `2026-09-15T20:29:54Z` clasificó `login`, activó el control nativo y no observó request Graph durante `15 s`; cierre limpio a las `2026-09-15T20:30:10Z`. Evidencia sanitizada en `/home/agus-hy/lazyteams-auth-lab/runtime/logs/stage1-headless-after-natural-wait-20260915T202944Z.jsonl`.
- La espera temporal no demuestra vencimiento natural de forma contractual porque la adquisición no informó `ExpiresAt`; tampoco permite aislar caché MSAL, SSO, selector, MFA, consentimiento o sesión irrecuperable.
- **Gate:** Etapa 1 ejecutada, pero no supera su gate. El escenario posterior a la espera nominal vuelve a mostrar `login` sin request Graph; D002 queda resuelta únicamente como fallback operativo `interaction_required`, sin afirmar renovación headless imposible ni universal. La ejecución de cualquier etapa posterior requiere autorización explícita; D002 no bloquea el diseño del núcleo de arranque rápido.

---

## Etapa 2 — Estado de credenciales, persistencia y ownership

### Objetivo

Evitar credenciales mezcladas, archivos truncados y competencia entre helpers.

### Estado de autorización

Etapa 2 fue autorizada explícitamente por el usuario el 2026-09-15. El gate técnico fue ejecutado y repetido. La revisión Sol del 2026-09-16 aprobó la etapa después de corregir compatibilidad de credenciales, inmutabilidad, cancelación y completar la evidencia con Firefox/helper real.

### Contrato técnico previo a ejecución

- `internal/auth` será la única capa que parsea, fusiona y persiste credenciales. `internal/graph` y el helper dejarán de interpretar `tokens.env`.
- `tokens.env` seguirá siendo el archivo canónico durante el corte. Las ocho claves legacy se conservarán y un único valor reservado `LAZYTEAMS_CREDENTIAL_METADATA`, serializado como JSON y escapado con el formato existente, contendrá versión de esquema, generación y metadatos por proveedor. Así secretos y metadatos cambian mediante un solo `rename`.
- El snapshot será inmutable después de publicarse y tendrá campos fijos para Graph, Web, Notif, EDU, cookie, EDU cookie, Spaces y Fabric; no habrá mapas ni parsing en el camino de cada request.
- Cada credencial podrá registrar `AcquiredAt`, `ExpiresAt` opcional, identificadores de cuenta y tenant, origen, recurso, aplicación y permisos conocidos. Un campo desconocido queda vacío; nunca se inventa desde un JWT ni se usa un JWT como prueba contractual única.
- La generación persistida será monotónica. Un delta declarará generación base y proveedores cambiados; una generación obsoleta se rechazará antes de escribir.
- Si el snapshot vigente conoce cuenta, tenant o permisos, un reemplazo debe aportar metadatos compatibles; desconocido no borra conocido. Recurso, origen y aplicación se validan en el límite de adquisición. Una incompatibilidad rechaza el delta completo y conserva el snapshot anterior.
- Los overrides por entorno seguirán prevaleciendo sólo en memoria. Se marcarán con origen `environment`, perderán metadatos de adquisición que ya no puedan atribuirse y nunca se copiarán al archivo.
- `graph.Client` publicará el snapshot completo con `atomic.Pointer`; cada request cargará exactamente un puntero antes de seleccionar token y cookie. Los ocho campos mutables y el parser de `ReloadTokens()` se eliminarán al migrar todos los callers.
- La escritura usará temporal en el mismo directorio, modo `0600`, `Sync`, cierre, reemplazo atómico y sincronización del directorio donde el sistema lo soporte. Un error previo al reemplazo deja intacto el archivo anterior.
- El ownership usará un lock asesor exclusivo del sistema operativo mantenido por un descriptor abierto, no la existencia del archivo. El registro protegido incluirá PID, identificador aleatorio de ownership y fecha de adquisición. Un proceso caído libera el lock en el kernel; sólo después de adquirirlo se reemplaza metadata huérfana.
- El lock vivirá fuera de `browser-session/`, cubrirá toda operación del helper que pueda tocar sesión o credenciales y se liberará sólo después de cerrar contexto, Playwright y procesos hijos. Nunca se borrarán locks globales de la caché Playwright ni se usará `pkill` como recuperación normal.
- La señal de cancelación solicitará cierre mediante un único controlador. `main` no llamará `os.Exit` desde goroutines; los `defer` de navegador y lock deberán ejecutarse antes de devolver el código de salida.

### Corte de implementación ejecutado

1. Introducir el modelo, parser único, merge compatible y persistencia atómica en `internal/auth`.
2. Migrar `graph.Client`, todos los requests, `main.go`, la TUI, el helper y las pruebas al snapshot; eliminar el parser y los campos legacy del cliente.
3. Reemplazar `KillZombieBrowser()` por ownership y cancelación estructurada; eliminar sus callers y la limpieza global de locks.
4. Ejecutar el gate completo con configuración y perfil de prueba aislados. No usar credenciales reales para las pruebas locales de concurrencia, persistencia o cancelación.

### Gate de salida

- Dos helpers concurrentes no compiten por el perfil; el segundo informa `profile_in_use` sin matar ni modificar al primero.
- Una cancelación y una terminación forzada liberan el ownership y no dejan Firefox ni helper huérfanos.
- Una lectura concurrente no observa campos de generaciones mezcladas.
- Un fallo de escritura antes del reemplazo no trunca ni modifica `tokens.env`.
- Un delta obsoleto y capturas de cuenta, tenant, recurso, aplicación o permisos incompatibles conservan byte por byte el snapshot válido.
- El parser único conserva valores quoted/escapados, archivos legacy e `EDU_COOKIE`; helper y recarga producen el mismo snapshot.
- `go test -race` sobre auth, graph, helpers y helper no reporta carreras en publicación o lectura de credenciales.

### Gate técnico ejecutado — 2026-09-15

- `go test ./...`: aprobado.
- `go test -race ./...`: aprobado en todos los paquetes.
- `internal/helpers/fs_test.go`: dos procesos sobre un directorio aislado; el segundo recibe `profile_in_use` y, tras terminar forzadamente el primero, el lock vuelve a estar disponible.
- `internal/auth/auth_test.go`: parser legacy/quoted, `EDU_COOKIE`, merge parcial, generación obsoleta, metadata incompatible y override de entorno sólo en memoria.
- `internal/graph/auth_check_test.go`: publicación concurrente mediante `atomic.Pointer` sin generaciones mezcladas.
- `GOOS=windows GOARCH=amd64 go test -c` para `internal/helpers`: aprobado.
- No se usaron credenciales reales, no se ejecutó adquisición y no se inició Etapa 3.
- **Revisión Sol — 2026-09-16:** no aprobada. El camino real del helper no aporta metadata suficiente para validar cuenta/tenant/recurso/aplicación/permisos; la comparación de permisos acepta degradaciones; el snapshot publicado sigue siendo mutable por alias; y la cancelación sólo cierra el contexto inicial, no los contextos visibles que lo reemplazan. Además, persisten salidas con `os.Exit` después de adquirir recursos y falta la evidencia real de ausencia de huérfanos y de fallo de escritura previo al reemplazo.
- **Estado:** gate aprobado por revisión Sol el 2026-09-16; no iniciar Etapa 3 sin autorización explícita.

### Gate técnico repetido — 2026-09-16

- Se corrigió el cleanup del helper: el único `os.Exit` restante está en `main`, después de que `run()` ejecuta sus `defer`.
- La cancelación usa un registro único de contextos y cierra el contexto activo inicial y los contextos visibles reabiertos para Graph, Teams y Fabric.
- `go test ./...`, `go test -race ./...`, el escenario focalizado de ownership, `git diff --check` y el diagnóstico LSP pasan.
- Se ejecutó un recorrido aislado de limpieza de sesión/tokens con `HOME` temporal, sin navegador ni credenciales.
- Evidencia real completada: interrupción ordenada con Firefox real y terminación forzada del helper con Firefox activo; en ambos casos no quedaron procesos helper/Firefox residuales. La prueba `TestPersistCredentialSetFailureBeforeRenamePreservesSnapshot` confirmó que un fallo antes de `rename` conserva `tokens.env` byte por byte. `atomicWrite` ahora propaga errores de `Sync` y `Close` del directorio cuando están disponibles.
- Referencias sanitizadas: `/home/agus-hy/lazyteams-auth-lab/runtime/logs/stage2-helper-term-20260916.txt`, `/home/agus-hy/lazyteams-auth-lab/runtime/logs/stage2-helper-sigkill-20260916.txt`, `auth-migration-lessons.md` L043.
- **Revisión Sol — 2026-09-16:** aprobada. La evidencia de SIGTERM conserva credenciales existentes, devuelve fallo y no anuncia guardado; SIGKILL con Firefox activo no dejó procesos residuales; el fallo previo a `rename` conservó `tokens.env` byte por byte; los logs sanitizados no contienen secretos.
- **Decisión del gate:** Etapa 2 aprobada formalmente. Esto no autoriza Etapa 3 ni adquisiciones reales; se requiere autorización explícita independiente.

### Evidencia requerida antes de Etapa 3

- Pruebas focalizadas del parser, merge, publicación concurrente, persistencia y lock.
- Escenario ejecutado de dos procesos sobre un perfil aislado.
- Escenario ejecutado de cancelación y terminación forzada con ausencia comprobada de procesos huérfanos.
- Registro sanitizado del gate y entrada correspondiente en `auth-migration-lessons.md`.
- Revisión Sol de los cambios por afectar arquitectura, concurrencia y límites de seguridad.

---

## Etapa 3 — Arranque progresivo y capacidades

### Objetivo

Permitir abrir la TUI con credenciales parciales y habilitar módulos independientemente.

### Estado de autorización

Etapa 3 fue autorizada explícitamente por el usuario el 2026-09-16 con Sol activo. Sol cerró el contrato técnico; la implementación y las pruebas corresponden a Luna. No se autorizan adquisiciones reales ni trabajo de Etapas 4–7.

### Contrato técnico aprobado

- `auth.LoadCredentialSet()` mantiene el snapshot parcial actual; `main.go` deja de exigir Graph, Web y cookie y permite un nombre de usuario vacío hasta obtener Graph.
- La TUI mantiene estado explícito por proveedor, separado de `loading`: `ready`, `missing`, `expired_or_revoked`, `interaction_required`, `permission_denied`, `network_unavailable`, `not_applicable`, `unknown`.
- El estado inicial se deriva sólo de presencia de credenciales; `ready` significa credencial disponible, no éxito de red. Los resultados reales reclasifican el proveedor sin confundir ausencia con red.
- Un preflight común en `internal/ui/commands.go` declara dependencias por comando y devuelve un error tipado antes de construir o enviar una request si falta alguna. No se agrega un segundo cliente HTTP ni un transporte que infiera proveedores por URL.
- El arranque no ejecuta validaciones duplicadas globales: inicia sólo cargas cuyas dependencias están presentes. Graph habilita perfil, equipos y chats; Notif actividad; EDU más EDU cookie o cookie habilitan Assignments. Web queda disponible para operaciones de mensajería una vez que Graph haya dado navegación.
- Los errores iniciales quedan aislados por módulo; no se usa `m.err` como pantalla fatal para una falla de Graph, EDU o Notif. Los errores ajenos a autenticación conservan su comportamiento actual salvo donde bloqueen otra capacidad.
- La clasificación mínima usa ausencia explícita, 401, 403 y errores de red; respuestas no contractuales quedan `unknown`. `not_applicable` e `interaction_required` sólo se asignan ante evidencia explícita, nunca por heurística de 403/404.
- Fabric permanece bajo demanda y fuera del arranque; no se modifica su adquisición.

### Secuencia

- Abrir TUI con lo disponible; identidad opcional hasta Graph.
- Graph listo carga perfil, equipos y chats.
- Web listo habilita mensajería, reacciones, imágenes y operaciones correspondientes.
- Notif listo carga actividad.
- EDU listo carga Assignments o queda `not_applicable`.
- Fabric sólo se solicita al usar administración privada.
- Cada comando valida su dependencia antes de enviar requests.
- Separar estados por módulo de `loading` global.

### Gate de salida

Con EDU fallando, equipos y chats se renderizan. Una capacidad faltante produce estado concreto, no un error de red ambiguo ni bloqueo global.

### Gate técnico ejecutado — 2026-09-16

- `main.go` permite iniciar con snapshot parcial y sin navegador/adquisición.
- `Model` publica estados independientes para Graph, Web, Notif y EDU; la pantalla inicial los muestra.
- Las cargas iniciales sólo se programan cuando existe su dependencia; EDU ausente queda `missing` hasta evidencia explícita de no aplicabilidad y no mantiene una vista en `loading`.
- Preflight evita requests con credenciales Graph, Notif, EDU, Web, Spaces o Fabric ausentes en los comandos cubiertos.
- El error de equipos queda aislado; no usa `m.err` como pantalla fatal global.
- `go test ./...`: aprobado.
- `go test -race ./...`: aprobado.
- `git diff --check`: aprobado.
- `internal/ui/stage3_test.go`: verifica Graph disponible + EDU no aplicable y que el preflight detiene Assignments antes de HTTP.
- No se ejecutaron adquisiciones reales, Graph headless, eventos incrementales, renovación durante uso ni Fabric.
- **Revisión final Sol — 2026-09-16:** no aprobada inicialmente. Con Graph ausente, los reintentos periódicos ejecutaban comandos con preflight, `teamsErrMsg` marcaba `teamsLoaded` y la vista reemplazaba la TUI por “No teams found”; fallos no-401 de chats activaban la pantalla fatal; el preflight era incompleto; Web podía permanecer falsamente `ready`; EDU ausente se clasificaba sin evidencia explícita; y la prueba no cubría la transición exigida. Correcciones y hallazgos completos: L048–L049.
- **Gate técnico repetido por Luna — 2026-09-16:** aprobado. Se verificó que la ausencia de Graph no arma reintentos, los errores de equipos/chats no bloquean la TUI, los comandos principales rechazan antes de HTTP y la transición Graph + chats disponibles con EDU fallando conserva los datos. `go test ./...`, `go test -race ./...`, `git diff --check` y LSP pasan.
- **Segunda revisión final Sol — 2026-09-16:** no aprobada. El preflight todavía omite comandos HTTP de mensajes, archivos y actividad; varias operaciones administrativas validan Graph aunque consumen Web, Spaces o Fabric; y el sondeo periódico de Notif convierte una credencial ausente en `ready` al devolver un resultado vacío sin error. Las suites, race detector y diff check pasan, pero no demuestran el contrato completo. Hallazgos y evidencia: L050.
- **Correcciones L050 y gate técnico repetido por Luna — 2026-09-16:** aprobados localmente. `requireCapabilities` cubre Graph/Web/Notif/EDU/Spaces/Fabric, los comandos HTTP revisados hacen preflight antes del transporte y Notif ausente no emite resultados de polling; tests, race, diff check y LSP pasan. Evidencia: L051.
- **Tercera revisión final Sol — 2026-09-16:** no aprobada. Persisten tres dependencias declaradas de forma incompatible con el recorrido real: miembros de equipo valida Spaces aunque consume Graph; mutaciones privadas validan Graph + Fabric aunque consumen Web + Fabric; y upload de Assignment valida sólo EDU aunque consume Graph + EDU. Los checks automatizados pasan, pero no cubren esas combinaciones. Evidencia: L052.
- **Correcciones L052 y gate técnico repetido por Luna — 2026-09-16:** las dependencias de producción fueron corregidas a Graph, Web + Fabric y Graph + EDU; suites, race y diff check pasan. Evidencia: L053.
- **Cuarta revisión final Sol — 2026-09-16:** no aprobada. El código corrige L052, pero los tests nuevos no instrumentan ni cuentan llamadas al transporte y los tests que usan `failTransport` aceptan cualquier error, incluido uno producido después de alcanzar HTTP. No se demuestra el requisito explícito de cero transporte. La bitácora truncada durante L053 fue recuperada antes de concluir la revisión. Evidencia: L054–L055.
- **Corrección L055 y gate técnico repetido por Luna — 2026-09-16:** `countingTransport` instrumenta los recorridos señalados y afirma cero llamadas HTTP para miembros de equipo, las tres mutaciones privadas y upload de Assignment. Tests focalizados, suite, race, diff check y LSP pasan. Evidencia: L056.
- **Quinta revisión final Sol — 2026-09-16:** aprobada. La revisión estática confirma que los cinco recorridos devuelven error de capacidad antes del cliente HTTP y las pruebas afirman contador cero. La comprobación de las tres mutaciones privadas sólo exige mensaje no nulo, no inspecciona su campo `err`; esa imprecisión de L056 quedó registrada, pero no invalida la evidencia conjunta estática + contador. Evidencia: L057.
- **Estado:** gate formal de Etapa 3 aprobado; Etapa 4 permanece bloqueada hasta autorización explícita.

---

## Etapa 4 — Helper por lotes y eventos incrementales

### Objetivo

Capturar una vez por lote, publicar resultados utilizables antes del final y eliminar esperas ciegas.

### Estado de autorización

El contrato técnico previo quedó definido por Sol el 2026-09-16 en L058 y la Etapa 4 fue autorizada explícitamente en L059. La implementación y varias rondas de corrección corresponden a Luna; después del rechazo de L086, L087 completó la evidencia contractual y la undécima revisión formal Sol aprobó el gate en L088. Etapa 5 y cualquier adquisición nueva siguen bloqueadas hasta autorización explícita independiente.

### Contrato técnico aprobado previo a ejecución

#### Invocación y alcance

- La TUI invocará un único helper por lote con `lazyteams-auth --events --renew-batch <trabajos>`, donde `<trabajos>` es una lista separada por comas. El modo CLI existente conserva `--renew`; nunca se reutiliza stdout humano como protocolo.
- Los trabajos admitidos son `graph`, `web_notif` y `edu`. Cualquier otro valor se rechaza antes de abrir Playwright. `web` y `notif` siempre se normalizan y deduplican como `web_notif`; una sola navegación puede publicar ambos proveedores y `spaces`/`cookie` si aparecen incidentalmente.
- `fabric` no entra en el lote normal. Sólo puede solicitarse por el flujo explícito existente y continúa fuera de esta etapa.
- Un lote contiene los trabajos pendientes al iniciarse. Una solicitud que aparezca después se deduplica contra el lote activo o queda para el lote siguiente; no se agrega un protocolo dúplex por stdin.
- Etapa 4 no implementa Graph headless-first: prepara los eventos y la coordinación, pero el cambio de estrategia Graph pertenece a Etapa 5. Mientras tanto conserva el recorrido Graph vigente y debe poder emitir `interaction_required` cuando exista evidencia explícita.

#### Protocolo IPC

En modo de eventos, stdout contiene exclusivamente JSONL UTF-8: un objeto completo por línea. La versión inicial es `1`. La salida humana queda fuera de stdout; la TUI no pasa `--debug`, porque el debug actual no es sanitizado.

Cada evento contiene obligatoriamente:

```json
{"version":1,"event":"started","batch_id":"opaque-random-id","provider":"batch","generation":0,"state":"running","duration_ms":0,"error_code":""}
```

Campos y valores permitidos:

- `event`: `started`, `credential_ready`, `interaction_required`, `provider_failed`, `completed` o `cancelled`;
- `provider`: `batch`, `graph`, `web`, `notif`, `edu`, `cookie`, `spaces` o `fabric`;
- `generation`: última generación persistida conocida; comienza en la generación cargada al abrir el lote y sólo vale `0` cuando todavía no existe snapshot;
- `state`: `running`, `ready`, `expired_or_revoked`, `interaction_required`, `permission_denied`, `network_unavailable`, `not_applicable`, `unknown`, `completed`, `partial_failure` o `cancelled`;
- `duration_ms`: tiempo monotónico desde el inicio del lote, expresado como entero no negativo;
- `error_code`: uno de `""`, `interaction_required`, `permission_denied`, `network_unavailable`, `not_applicable`, `persistence_failed`, `incompatible_credential`, `stale_generation`, `profile_in_use`, `timeout`, `cancelled`, `protocol_error` o `acquisition_failed`. Nunca contiene errores crudos de librerías, URLs, DOM, identidad ni contenido de respuesta.

`batch_id` es diagnóstico y correlación, no autoridad de seguridad. No se incluyen tokens, cookies, headers, claims, rutas locales, query strings ni texto de páginas. Un único encoder serializa eventos para evitar líneas intercaladas.

#### Orden y semántica

- `started` es exactamente el primer evento y usa `provider=batch`.
- Una captura se valida y persiste mediante `internal/auth` antes de emitir cualquier `credential_ready`. El evento lleva exactamente la generación ya reemplazada atómicamente en `tokens.env`.
- Una persistencia coherente puede producir varios `credential_ready` con la misma generación —por ejemplo `web`, `notif`, `cookie` y `spaces`—, pero nunca un evento por una credencial vacía o sólo observada en memoria.
- `interaction_required` no anuncia éxito ni cambia la generación. Sólo se emite ante pantalla o respuesta explícita; no se infiere de un timeout.
- `provider_failed` es terminal para ese proveedor dentro del lote, pero no impide publicar éxitos de otros proveedores. `not_applicable` requiere evidencia explícita.
- Existe exactamente un evento terminal del lote: `completed` o `cancelled`, ambos con `provider=batch`. `completed` usa estado `completed` si todos los trabajos solicitados terminaron satisfactoriamente o `partial_failure` si alguno falló.
- El exit code `0` exige `completed` sin fallos de trabajos solicitados. Fallo parcial, stream inválido, persistencia fallida o cancelación terminan con código distinto de cero. La TUI usa los eventos como fuente incremental y `Wait` sólo para cerrar el lifecycle y validar el resultado terminal.

#### Consumo en la TUI

- La TUI mantiene un único proceso y un único lector de stdout por lote. Un scanner publica un evento Bubble Tea por vez; `Update` programa la lectura siguiente mientras el proceso continúa vivo.
- Al recibir `credential_ready`, la TUI recarga el snapshot desde `internal/auth` inmediatamente, comprueba que la generación cargada sea al menos la anunciada, actualiza sólo las capacidades publicadas y dispara sus cargas sin esperar `completed`.
- Graph dispara perfil/equipos/chats; Web habilita mensajería; Notif dispara actividad; EDU dispara Assignments. `cookie` y `spaces` actualizan disponibilidad sin bloquear el núcleo.
- JSON malformado, versión desconocida, proveedor/estado inválido, generación regresiva o EOF sin evento terminal fallan de forma cerrada: no habilitan capacidades ni anuncian éxito. Cada línea se limita a 64 KiB para evitar consumo de memoria no controlado.
- El modelo conserva el handle del proceso hasta que `Wait` termina. La salida normal drena stdout antes de limpiar el handle; no se inicia otro helper mientras el anterior siga vivo.
- Al salir o cancelar, la TUI envía la señal ordenada existente, sigue drenando eventos hasta `cancelled`/EOF y espera el proceso. Sólo después del grace period acotado puede forzar la terminación. Nunca reintenta automáticamente una operación mutante.

#### Coordinación y señales

- El orden del lote prioriza el núcleo solicitado: Graph, luego `web_notif`, luego EDU. La ausencia o fallo de EDU no demora la publicación de Graph/Web/Notif ya persistidos.
- Spaces se aprovecha si aparece durante `web_notif`, sin convertirse en condición de éxito de chat.
- Se reemplazan sleeps usados como pasos normales por señales observables ya disponibles: navegación completada, request relevante, pantalla conocida, captura validada o cierre. Los timeouts quedan únicamente como límites de fallo y no producen `ready`.
- La persistencia incremental no borra capturas anteriores: cada checkpoint extrae sólo los valores nuevos y fusiona ese delta contra la generación vigente mediante `internal/auth`; no vuelve a persistir todo el acumulador en cada evento. Un error de generación o compatibilidad produce `provider_failed` y conserva el snapshot anterior.

### Gate de salida

Antes de solicitar aprobación formal deben quedar documentadas estas evidencias:

- un helper falso emite `credential_ready`, permanece vivo y la TUI recarga/habilita el módulo antes de `completed`;
- `web` y `notif` solicitados juntos crean un solo proceso, una sola navegación de adquisición y eventos para ambos proveedores;
- persistencia fallida o stream inválido no emiten ni aplican `credential_ready`;
- fallos de EDU quedan aislados después de éxitos del núcleo;
- la cancelación drena el stream, termina el helper y no deja contexto, Firefox ni proceso huérfano;
- tests focalizados del codec/orden, integración TUI-helper, suite completa, race detector, `git diff --check` y LSP pasan;
- cualquier recorrido real se ejecuta sólo en el laboratorio aislado autorizado, con logs sanitizados y sin Fabric ni Graph headless de Etapa 5.

La revisión Sol debe comprobar el orden persistencia→evento, la ausencia de secretos, cero doble adquisición Web/Notif, habilitación anterior a `completed` y cierre completo del lifecycle.

### Gate formal aprobado — 2026-09-16

- La revisión estática confirmó persistencia previa a `credential_ready`, error de protocolo cerrado, terminal único y consumo incremental antes de `completed`.
- Las secuencias de persistencia fallida y Graph→EDU comparan todos los campos contractuales salvo `duration_ms`; el writer controlado falla exactamente una escritura intermedia, acepta el terminal posterior y mantiene exit code distinto de cero con `partial_failure/protocol_error`.
- El lote real `web_notif` del laboratorio produjo un único `started`, una sola adquisición con eventos `web`, `notif`, `cookie` y `spaces`, y un único `completed`, sin Fabric ni valores sensibles.
- La cancelación temprana real produjo exactamente `started` + `cancelled` sin Firefox ni huérfanos; la integración TUI-helper cubre señal, drenaje hasta EOF, `Wait` y salida posterior.
- Tests focalizados, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `git diff --check` y LSP sobre nueve archivos pasan.
- Evidencia y decisión completas: L071–L088 en `auth-migration-lessons.md` y logs sanitizados del laboratorio identificados allí.
- **Decisión del gate:** Etapa 4 aprobada formalmente por Sol. Esto no autoriza Etapa 5, Graph headless-first, Fabric ni nuevas adquisiciones; se requiere autorización explícita independiente.

---

## Etapa 5 — Graph headless-first

### Objetivo

Usar credencial vigente y sesión recuperable antes de abrir una ventana visible.

### Estado de autorización

Etapa 5 fue autorizada explícitamente por el usuario el 2026-09-16 con Sol activo. Sol cerró el contrato técnico previo en L090. El corte local fue implementado y validado por Luna en L091, pero la primera revisión formal Sol rechazó el gate en L092 por incumplimientos de cuarentena, procedencia, compatibilidad, protocolo y evidencia. No se autorizan adquisiciones reales nuevas ni trabajo de Etapas 6–7.

### Hallazgos previos que condicionan el corte

- El arranque normal ya conserva el camino de cero navegador: la TUI sólo encola renovación Graph después de un rechazo real; Etapa 5 no agregará una validación global ni abrirá el helper preventivamente.
- El batch actual fuerza visible antes de intentar Graph. `tryExtractGraphTokenViaGraphExplorer` además acepta un fallback de `localStorage` y el interceptor publica el primer Bearer Graph antes de validar identidad, aplicación o permisos.
- Graph Explorer solicita por defecto `openid profile User.Read`; ejecutar `/me` no demuestra que el token cubra equipos, canales y chats. Un token capturado en esa request sólo es aceptable si sus permisos observables satisfacen el mínimo del producto y no degradan los permisos conocidos del snapshot vigente.
- El protocolo de Etapa 4 admite `interaction_required` seguido posteriormente por `credential_ready` para el mismo proveedor; esa transición será explícita y no se tratará como fallo terminal del proveedor.

### Contrato técnico aprobado previo a ejecución

#### Entrada y cero navegador

- Una credencial Graph se usa normalmente hasta que una petición real la rechaza o exista un `ExpiresAt` contractual confiable. No se decodifica `exp` para iniciar renovaciones y no se lanza un helper sólo para comprobar `/me`.
- El gate de “credencial vigente” se demostrará ejecutando el arranque y operaciones Graph exitosas con helper inyectado/contado en cero, no añadiendo un nuevo probe de red.
- Una renovación Graph usa siempre el modo batch JSONL de Etapa 4. El modo CLI legacy puede conservarse como wrapper, pero no tendrá una segunda implementación de adquisición.

#### Recorrido headless y fallback visible

1. Adquirir ownership del perfil y abrir un único contexto persistente headless.
2. Navegar al Graph Explorer oficial y activar su flujo nativo; la única captura aceptada proviene de una request HTTPS directa a `graph.microsoft.com` iniciada desde una página oficial de Graph Explorer.
3. Mantener el candidato en cuarentena: no copiarlo a `captured.graphToken`, no persistirlo y no emitir `credential_ready` antes de validarlo.
4. Si se observa login, selector de cuenta, MFA o consentimiento mediante URL/controles conocidos, emitir `interaction_required` sin cambiar generación.
5. Cerrar completamente el contexto headless y recién después reabrir el mismo perfil visible dentro del mismo helper y lock. Nunca coexistirán dos contextos sobre `browser-session/`.
6. Continuar el flujo visible hasta captura válida, cancelación o timeout. Si el lote contiene trabajos posteriores, éstos reutilizan el único contexto vigente, visible o headless según el recorrido alcanzado.
7. Un timeout o estado desconocido no abre visible por heurística y no se convierte en `ready`.

#### Validación del candidato Graph

Antes de reemplazar el snapshot deben cumplirse conjuntamente:

- URL parseada con esquema `https`, host exacto `graph.microsoft.com` y sin aceptar coincidencias por substring;
- procedencia de una página oficial de Graph Explorer y aplicación coincidente con el `ClientId` público expuesto por esa ejecución cuando pueda observarse;
- audiencia Graph (`https://graph.microsoft.com` o `00000003-0000-0000-c000-000000000000`);
- si el snapshot conoce cuenta, tenant o aplicación, el candidato debe aportar el mismo valor; metadata desconocida del candidato no alcanza para reemplazar metadata conocida;
- los permisos conocidos del snapshot vigente deben conservarse; además, para un bootstrap sin baseline se exige el núcleo de arranque: `User.Read`, `Team.ReadBasic.All`, `Channel.ReadBasic.All` y alguna de `Chat.ReadBasic`, `Chat.Read` o `Chat.ReadWrite`;
- tokens opacos o metadata incompleta pueden seguir usándose si ya están guardados, pero un candidato nuevo que no permita demostrar estas compatibilidades no reemplaza el snapshot;
- la decodificación JWT es una señal auxiliar para audiencia, identidad, aplicación y scopes; la procedencia y el host se obtienen del evento de adquisición. `/me` por sí solo nunca valida el reemplazo.

La procedencia específica `graph_explorer` podrá reemplazar metadata legacy genérica `browser`, pero una adquisición genérica no podrá degradar posteriormente esa procedencia específica. La compatibilidad se implementará una sola vez en `internal/auth`, sin duplicar parsing en el helper.

#### Persistencia y eventos

- La validación construye un `auth.Credential` explícito y se persiste mediante `PersistDelta` contra la generación cargada; `PersistTokenValues` no será el camino Graph de Etapa 5.
- Sólo después del reemplazo atómico se copia/publica el resultado y se emite `credential_ready` con la generación persistida.
- Cuenta, tenant, recurso, aplicación, procedencia o permisos incompatibles producen `provider_failed` con `incompatible_credential`; generación obsoleta produce `stale_generation`; red y timeout conservan sus códigos específicos.
- `interaction_required` es un estado intermedio cuando el helper puede continuar visible. Si el usuario cancela, el lote termina `cancelled`; si la interacción no concluye, termina con fallo parcial, nunca con éxito falso.
- Se elimina el fallback Graph que extrae tokens desde `localStorage`; la evidencia de adquisición será la request real del flujo nativo.

#### Alcance excluido

- No se añade renovación anticipada ni scheduling por expiración: pertenece a Etapa 6.
- No se modifica Fabric ni se automatizan escrituras.
- No se introduce app OAuth propia, refresh token directo, daemon, navegador permanente ni dependencia en internals globales de MSAL.

### Gate de salida

Antes de solicitar aprobación formal deben quedar documentadas estas evidencias:

- credencial Graph aceptada por operaciones reales simuladas/locales: cero procesos de helper o navegador;
- sesión recuperable: un único contexto headless, captura validada, persistencia previa y `credential_ready` sin apertura visible;
- interacción explícita: secuencia `started` → `interaction_required(graph)` → apertura visible única → `credential_ready(graph)` → terminal coherente, sin dos contextos simultáneos;
- candidato de otra cuenta, tenant, aplicación, recurso o con downgrade de permisos: snapshot byte por byte intacto y `incompatible_credential`;
- token capturado desde host, frame o `localStorage` no autorizado: nunca se persiste ni habilita Graph;
- timeout, red y cancelación: clasificación concreta, terminal coherente y ausencia de helper/Firefox huérfano;
- tests focalizados de validación, transición headless→visible y orden persistencia→evento; suite completa, race detector, `git diff --check` y LSP aprobados;
- cualquier recorrido real se ejecuta sólo con autorización adicional en el laboratorio aislado y logs sanitizados.

La revisión Sol debe comprobar especialmente que el camino headless precede al visible, que un candidato permanece en cuarentena hasta validación/persistencia, que no se degradan permisos o identidad y que una credencial vigente no inicia navegador.

### Primera revisión formal Sol — 2026-09-16

- Tests focalizados, suite completa, race detector, `git diff --check` y LSP pasan.
- El gate se rechaza: permanece el fallback Graph de `localStorage` y puede persistir por el camino genérico; el candidato se copia a `captured.graphToken` antes de validación completa/persistencia; `interaction_required` no se emite antes de abrir visible; la compatibilidad acepta identidad entrante desconocida y rechaza el upgrade permitido `browser`→`graph_explorer`; no se restringe la captura al frame principal; los códigos de error no conservan su clasificación contractual; faltan las pruebas focalizadas del lifecycle y protocolo.
- Evidencia y acciones correctivas completas: L092 en `auth-migration-lessons.md`.
- **Decisión del gate:** Etapa 5 rechazada en primera revisión formal. Corregir con Luna y repetir el gate técnico antes de otra revisión Sol. No ejecutar adquisición real ni Etapas 6–7.

---

## Etapa 6 — Renovación durante el uso

### Objetivo

Evitar que una expiración habitual obligue a salir de la TUI.

### Reglas

- Renovar anticipadamente sólo con vencimiento confiable.
- Sin vencimiento confiable, reaccionar a rechazo real del proveedor o política acotada; no lanzar consultas duplicadas globales.
- Replanificar después de suspensión o cambio de conectividad.
- No reintentar indefinidamente permisos insuficientes.
- No repetir mutaciones cuya respuesta se perdió.
- Mantener drafts y operaciones en curso fuera de la ruta de reemplazo de credenciales.

### Gate de salida

La expiración normal actualiza la credencial y recupera la capacidad sin reinicio; red interrumpida conserva sesión y no anuncia éxito falso; mensajes/uploads no se duplican.

---

## Etapa 7 — Fabric y validación final

### Producto

Fabric no bloquea el arranque. La administración privada muestra su dependencia y conserva el flujo manual como fallback.

### Experimento de interceptación

- Contexto separado.
- Service Workers bloqueados.
- Route instalada antes de navegar.
- Coincidencia sólo con la escritura objetivo.
- Abort antes del servidor.
- Repetición con reintentos, nueva navegación y múltiples páginas.
- Verificación de auditoría del tenant.

Se descarta la hipótesis si cualquier escritura llega al servidor o si el token aparece sólo después de permitirla.

### Gate de salida

Sólo afirmar captura automática si existe evidencia de cero cambios en tenant, cobertura de reintentos y token realmente asociado a la operación. De lo contrario, mantener adquisición explícita.

---

## 4. Matriz de aceptación final

| Escenario | Evidencia obligatoria |
| --- | --- |
| Credenciales vigentes | TUI usable y cero procesos de navegador. |
| Graph vencido, sesión recuperable | `credential_ready` sin credenciales manuales. |
| Graph listo, EDU falla | Equipos/chats cargados; EDU aislado en error. |
| Cuenta sin Education | `not_applicable`, sin espera indefinida. |
| Web + Notif vencidos | Un único lote/adquisición. |
| Wi-Fi interrumpido | Perfil conservado; no éxito falso. |
| Salida durante renovación | Sin helper ni Firefox huérfano. |
| Dos instancias | Lock; la segunda no mata a la primera. |
| Cuenta/permisos incompatibles | Snapshot anterior conservado. |
| MFA/consentimiento | Un flujo visible y mensaje claro. |
| Mensaje/upload en curso | Sin duplicación y draft conservado. |
| Fabric sin escritura | Cero cambios auditados en tenant. |

## 5. Política de evidencia y cambios

Para cada etapa registrar en `auth-migration-lessons.md`:

1. fecha y etapa;
2. hipótesis o decisión;
3. entorno y configuración no sensible;
4. procedimiento exacto;
5. resultado observado;
6. evidencia (comando, log sanitizado, screenshot o referencia);
7. impacto en el diseño;
8. siguiente acción;

- [x] Plan conceptual auditado.
- [x] Código actual contrastado con el plan.
- [x] Riesgos principales identificados.
- [x] Orden de ejecución y gates definidos.
- [x] Etapa 0 autorizada por el usuario.
- [x] Etapa 0 ejecutada y gate documentado.
- [x] Etapa 1 autorizada por el usuario.
- [ ] Etapa 1 ejecutada y gate aprobado; se cerró sin aprobación y con fallback operativo documentado.
- [x] Diseño previo de Etapa 2 documentado.
- [x] Etapa 2 autorizada explícitamente por el usuario.
- [x] Etapa 2 corregida, evidencia completada y gate aprobado por Sol.
- [x] Etapa 3 autorizada explícitamente por el usuario.
- [x] Contrato técnico de Etapa 3 aprobado por Sol.
- [x] Etapa 3 implementada por Luna y gate técnico documentado.
- [x] Revisión final Sol del diff y gate de Etapa 3 ejecutada; gate rechazado inicialmente en L048.
- [x] Correcciones de Etapa 3 implementadas por Luna y gate técnico repetido en L049.
- [x] Nueva revisión final Sol ejecutada; gate rechazado en L050.
- [x] Correcciones de L050 implementadas por Luna y gate técnico repetido; evidencia en L051.
- [x] Tercera revisión final Sol ejecutada; gate rechazado en L052.
- [x] Correcciones de L052 implementadas y gate técnico repetido por Luna; evidencia en L053.
- [x] Cuarta revisión final Sol ejecutada; código corregido pero evidencia de cero transporte rechazada en L055.
- [x] Tests de cero transporte corregidos y gate técnico repetido por Luna.
- [x] Quinta revisión final Sol ejecutada; gate formal de Etapa 3 aprobado en L057.
- [x] Contrato técnico previo de Etapa 4 definido por Sol en L058.
- [x] Etapa 4 autorizada explícitamente por el usuario.
- [x] Implementación local de Etapa 4 y validaciones iniciales documentadas en L060.
- [x] Primera revisión Sol de Etapa 4 ejecutada; gate rechazado en L062.
- [x] Bloqueantes L062 corregidos parcialmente por Luna y checks locales repetidos; evidencia en L063.
- [x] Segunda revisión Sol de Etapa 4 ejecutada; gate rechazado en L065.
- [x] Bloqueantes L065 corregidos parcialmente por Luna y checks locales repetidos; evidencia en L066.
- [x] Tercera revisión Sol de Etapa 4 ejecutada; gate y evidencia de integración rechazados en L067.
- [x] Bloqueantes locales L067 corregidos parcialmente y gate técnico repetido con Luna; evidencia en L068.
- [x] Cuarta revisión Sol ejecutada; gate rechazado y bloqueantes restantes documentados en L069.
- [x] Bloqueantes L069 corregidos y gate técnico repetido con Luna; evidencia en L070.
- [ ] Evidencia Firefox/lifecycle real de Etapa 4 completada según el gate; L074 conserva la evidencia parcial de L071–L073 pero exige cancelación temprana estructurada e integración TUI.
- [x] Quinta revisión formal Sol de Etapa 4 ejecutada; gate rechazado en L074.
- [x] Cancelación temprana, integración local TUI-helper y evidencia real aislada documentadas en L075–L077.
- [x] Sexta revisión formal Sol de Etapa 4 ejecutada; gate rechazado en L078.
- [x] Correcciones locales de L078 documentadas por Luna en L079.
- [x] Séptima revisión formal Sol de Etapa 4 ejecutada; gate rechazado en L080.
- [x] Correcciones locales de L080 documentadas por Luna en L081.
- [x] Octava revisión formal Sol de Etapa 4 ejecutada; gate rechazado en L082.
- [x] Bloqueantes de L082 corregidos por Luna y gate técnico repetido; evidencia en L083.
- [x] Novena revisión formal Sol de Etapa 4 ejecutada; gate rechazado en L084.
- [x] Bloqueantes funcionales de L084 corregidos por Luna y gate técnico repetido; evidencia en L085.
- [x] Décima revisión formal Sol de Etapa 4 ejecutada; gate rechazado por evidencia incompleta en L086.
- [x] Correcciones de evidencia de L086 implementadas por Luna y checks locales repetidos; evidencia en L087.
- [x] Undécima revisión formal Sol ejecutada; gate formal de Etapa 4 aprobado en L088.
- [x] Etapa 5 autorizada explícitamente por el usuario en L089.
- [x] Contrato técnico de Etapa 5 aprobado por Sol en L090.
- [x] Implementación local de Etapa 5 y checks técnicos documentados por Luna en L091.
- [x] Primera revisión formal Sol de Etapa 5 ejecutada; gate rechazado en L092.
- [ ] Bloqueantes L092 pendientes de corrección por Luna y repetición del gate técnico.

**Próximo paso permitido:** cambiar a Luna para corregir los bloqueantes L092 y repetir únicamente el gate técnico local de Etapa 5. No ejecutar adquisiciones reales nuevas sin autorización adicional; no iniciar Etapas 6–7 ni Fabric.
