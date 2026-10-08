# Historial y bitácora de gates: migración de autenticación

> Este archivo conserva el registro histórico completo de revisiones formales, correcciones intermedias, gates aprobados/rechazados y micro-checkpoints de la migración de autenticación de LazyTeams.
> Fue migrado desde `auth-migration-runbook.md` para mantener el runbook operativo enfocado y por debajo de 400 líneas, sin perder trazabilidad técnica ni evidencia histórica.
> Para el contrato operativo y el gate activo, consultar `auth-migration-runbook.md`. Para la bitácora de lecciones, consultar `auth-migration-lessons.md`.

---

## 1. Etapa 0 — Aislamiento y seguridad (Historial)

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

## 2. Etapa 1 — Prueba real de Graph Explorer y medición (Historial)

### Protocolo ejecutado

1. Autenticar Graph Explorer en el perfil dedicado con consentimiento autorizado.
2. Ejecutar una consulta mínima y no mutante dentro de la misma sesión.
3. Cerrar Firefox limpiamente.
4. Reabrir el mismo perfil en headless.
5. Cargar Graph Explorer y activar su flujo nativo.
6. Observar la request real a Graph: host/ruta, estado y clasificación, nunca el token.
7. Repetir tras vencer el access token y al día siguiente.
8. Comparar headless y visible.
9. Repetir con un tenant universitario representativo sólo si existe autorización.

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

## 3. Etapa 2 — Estado de credenciales, persistencia y ownership (Historial)

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

---

## 4. Etapa 3 — Arranque progresivo y capacidades (Historial)

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

### Revisiones formales Sol (L048–L057)

- **Revisión final Sol — 2026-09-16 (L048–L049):** no aprobada inicialmente. Con Graph ausente, los reintentos periódicos ejecutaban comandos con preflight, `teamsErrMsg` marcaba `teamsLoaded` y la vista reemplazaba la TUI por “No teams found”; fallos no-401 de chats activaban la pantalla fatal; el preflight era incompleto; Web podía permanecer falsamente `ready`; EDU ausente se clasificaba sin evidencia explícita; y la prueba no cubría la transición exigida.
- **Gate técnico repetido por Luna — 2026-09-16:** aprobado. Se verificó que la ausencia de Graph no arma reintentos, los errores de equipos/chats no bloquean la TUI, los comandos principales rechazan antes de HTTP y la transición Graph + chats disponibles con EDU fallando conserva los datos. `go test ./...`, `go test -race ./...`, `git diff --check` y LSP pasan.
- **Segunda revisión final Sol — 2026-09-16 (L050):** no aprobada. El preflight todavía omite comandos HTTP de mensajes, archivos y actividad; varias operaciones administrativas validan Graph aunque consumen Web, Spaces o Fabric; y el sondeo periódico de Notif convierte una credencial ausente en `ready` al devolver un resultado vacío sin error. Las suites, race detector y diff check pasan, pero no demuestran el contrato completo.
- **Correcciones L050 y gate técnico repetido por Luna — 2026-09-16 (L051):** aprobados localmente. `requireCapabilities` cubre Graph/Web/Notif/EDU/Spaces/Fabric, los comandos HTTP revisados hacen preflight antes del transporte y Notif ausente no emite resultados de polling; tests, race, diff check y LSP pasan.
- **Tercera revisión final Sol — 2026-09-16 (L052):** no aprobada. Persisten tres dependencias declaradas de forma incompatible con el recorrido real: miembros de equipo valida Spaces aunque consume Graph; mutaciones privadas validan Graph + Fabric aunque consumen Web + Fabric; y upload de Assignment valida sólo EDU aunque consume Graph + EDU. Los checks automatizados pasan, pero no cubren esas combinaciones.
- **Correcciones L052 y gate técnico repetido por Luna — 2026-09-16 (L053):** las dependencias de producción fueron corregidas a Graph, Web + Fabric y Graph + EDU; suites, race y diff check pasan.
- **Cuarta revisión final Sol — 2026-09-16 (L054–L055):** no aprobada. El código corrige L052, pero los tests nuevos no instrumentan ni cuentan llamadas al transporte y los tests que usan `failTransport` aceptan cualquier error, incluido uno producido después de alcanzar HTTP. No se demuestra el requisito explícito de cero transporte. La bitácora truncada durante L053 fue recuperada antes de concluir la revisión.
- **Corrección L055 y gate técnico repetido por Luna — 2026-09-16 (L056):** `countingTransport` instrumenta los recorridos señalados y afirma cero llamadas HTTP para miembros de equipo, las tres mutaciones privadas y upload de Assignment. Tests focalizados, suite, race, diff check y LSP pasan.
- **Quinta revisión final Sol — 2026-09-16 (L057):** aprobada. La revisión estática confirma que los cinco recorridos devuelven error de capacidad antes del cliente HTTP y las pruebas afirman contador cero. La comprobación de las tres mutaciones privadas sólo exige mensaje no nulo, no inspecciona su campo `err`; esa imprecisión de L056 quedó registrada, pero no invalida la evidencia conjunta estática + contador.
- **Decisión del gate:** gate formal de Etapa 3 aprobado en L057.

---

## 5. Etapa 4 — Helper por lotes y eventos incrementales (Historial)

### Gate formal aprobado — 2026-09-16

- La revisión estática confirmó persistencia previa a `credential_ready`, error de protocolo cerrado, terminal único y consumo incremental antes de `completed`.
- Las secuencias de persistencia fallida y Graph→EDU comparan todos los campos contractuales salvo `duration_ms`; el writer controlado falla exactamente una escritura intermedia, acepta el terminal posterior y mantiene exit code distinto de cero con `partial_failure/protocol_error`.
- El lote real `web_notif` del laboratorio produjo un único `started`, una sola adquisición con eventos `web`, `notif`, `cookie` y `spaces`, y un único `completed`, sin Fabric ni valores sensibles.
- La cancelación temprana real produjo exactamente `started` + `cancelled` sin Firefox ni huérfanos; la integración TUI-helper cubre señal, drenaje hasta EOF, `Wait` y salida posterior.
- Tests focalizados, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `git diff --check` y LSP sobre nueve archivos pasan.
- Evidencia y decisión completas: L071–L088 en `auth-migration-lessons.md` y logs sanitizados del laboratorio identificados allí.
- **Decisión del gate:** Etapa 4 aprobada formalmente por Sol en L088.

---

## 6. Etapa 5 — Graph headless-first (Historial de 16 revisiones Sol)

### Primera revisión formal Sol — 2026-09-16 (L092)
- Tests focalizados, suite completa, race detector, `git diff --check` y LSP pasan.
- El gate se rechaza: permanece el fallback Graph de `localStorage` y puede persistir por el camino genérico; el candidato se copia a `captured.graphToken` antes de validación completa/persistencia; `interaction_required` no se emite antes de abrir visible; la compatibilidad acepta identidad entrante desconocida y rechaza el upgrade permitido `browser`→`graph_explorer`; no se restringe la captura al frame principal; los códigos de error no conservan su clasificación contractual; faltan las pruebas focalizadas del lifecycle y protocolo.
- **Decisión:** rechazada.

### Segunda revisión formal Sol — 2026-09-16 (L094)
- Suite focalizada, suite completa, race detector y `git diff --check` pasan; no se ejecutó navegador ni adquisición real.
- L093 elimina `localStorage` del recorrido Graph, mantiene el candidato separado de `graphToken`, restringe host/frame, emite `interaction_required` antes de visible y permite el upgrade `browser`→`graph_explorer`.
- El gate se rechaza: `credential_ready` Graph conserva la generación anterior porque se ignora la generación devuelta por `PersistGraphCandidate`; timeout/red y varios rechazos de compatibilidad siguen perdiendo sus códigos; el modo CLI legacy Graph y el flujo completo esperan `graphToken` aunque el listener sólo llena `graphCandidate`, por lo que pueden agotar el plazo y anunciar guardado sin persistir Graph; `notifyTokenCaptured` imprime salida humana al observar un candidato aún no validado y puede contaminar stdout JSONL; el primer candidato no validado detiene la espera e impide considerar una request posterior válida; y continúan ausentes las pruebas contractuales del lifecycle, protocolo, frame/storage y cero navegador.
- **Decisión:** rechazada.

### Tercera revisión formal Sol — 2026-09-16 (L096)
- Suite completa, race detector y `git diff --check` pasan; LSP no reporta errores de compilación; no se ejecutó navegador ni adquisición real.
- L095 corrige la generación publicada por el batch, conserva el candidato en cuarentena hasta persistir y tipa incompatibilidad/generación obsoleta.
- El gate se rechaza: `fullRenewal` aún espera `graphToken` sin validar/persistir `graphCandidate`; `--renew graph` sigue siendo visible-first y duplica la implementación batch; el fallback visible devuelve otra vez `interaction_required` sin esperar que el usuario complete el flujo; la clasificación `network_unavailable` no tiene una señal alcanzable; y faltan las pruebas contractuales de secuencia, lifecycle, wrapper/full, frame/storage y cero navegador.
- **Decisión:** rechazada.

### Corrección local posterior — 2026-09-17 (L097)
- `--renew graph` usa ahora el recorrido batch headless-first en lugar de una adquisición visible independiente.
- `fullRenewal` delega la adquisición Graph al mismo recorrido compartido; la persistencia y publicación siguen pasando por `PersistGraphCandidate` antes de marcar Graph listo.
- Los errores de navegación Graph se clasifican como `network_unavailable` mediante `errGraphNetwork`.
- El fallback visible espera un candidato, cancelación o timeout sin retornar inmediatamente por la pantalla de login/MFA/consentimiento.

### Cuarta revisión formal Sol — 2026-09-17 (L098)
- Suite completa, race detector, `git diff --check` y LSP pasan; no se ejecutó navegador ni adquisición real.
- L097 unifica parcialmente los puntos de entrada y corrige la espera visible, pero el gate se rechaza.
- `--renew graph` no propaga `batchErr` al exit code y puede anunciar éxito tras fallar; `saveTokens` puede persistir un delta vacío e incrementar la generación.
- `fullRenewal` puede pasar un contexto/página cerrados después del re-login, no propaga el error al caller y conserva un bloque Graph histórico inalcanzable.
- La cancelación no interrumpe el retry ni la espera visible; un contexto cerrado puede dejar el helper activo hasta el timeout.
- Faltan las pruebas contractuales completas de secuencia, lifecycle, wrapper/full, frame/storage, candidato inválido→válido y cero navegador.
- **Decisión:** rechazada.

### Quinta revisión formal Sol — 2026-09-17 (L106)
- Suite completa, race detector, `git diff --check` y LSP pasan; no se ejecutó navegador ni adquisición real.
- L099–L105 corrigen propagación, contexto cerrado, bloque inalcanzable y cancelación antes del fallback visible.
- El gate se rechaza: `--renew graph` todavía llama `saveTokens` después de persistir Graph y aumenta la generación con un delta vacío; el candidato productivo no aporta `ClientID`, de modo que el chequeo de aplicación queda desactivado en bootstrap; y el auto-click de la consulta por defecto no demuestra ni solicita los scopes mínimos que luego exige el validador.
- La evidencia tampoco cubre los puntos públicos wrapper/full, la secuencia JSONL exitosa con generación exacta, frame/storage en el listener real, contexto único ni el caso de credencial vigente con cero helper/navegador.
- **Decisión:** rechazada.

### Sexta revisión formal Sol — 2026-09-17 (L122)
- Suite completa, race detector y `git diff --check` pasan; LSP no reporta errores, sólo advisories no bloqueantes; no se ejecutó navegador ni adquisición real.
- L107–L121 corrigen el delta vacío, aplicación/scopes, generación y protocolo, y agregan evidencia local de selección TUI, frame/storage y transición de contexto.
- El gate se rechaza: la transición ignora el error de `BrowserContext.Close()` y puede abrir visible sin demostrar que headless cerró; la prueba de cero helper sólo llama el guard de cola vacía sin credencial ni operación Graph; wrapper `run()` y `fullRenewal` siguen sin ejecución mediante seams públicos; el filtro por `Hostname()` acepta puertos explícitos pese al requisito de host exacto.
- **Decisión:** rechazada.

### Séptima revisión formal Sol — 2026-09-17 (L125)
- Suite completa, race detector y `git diff --check` pasan; LSP no reporta errores; no se ejecutó navegador ni adquisición real.
- L123 corrige el cierre headless→visible y los puertos explícitos, pero el gate se rechaza nuevamente.
- `fullRenewal` retorna incondicionalmente antes del recorrido de cookies/Fabric, dejando código inalcanzable y causando una regresión funcional.
- Los tests L124 ejercitan helpers triviales, no los puntos públicos `run()`/`fullRenewal`; la prueba TUI usa cola vacía y no ejecuta una operación Graph local ni demuestra por contadores separados cero helper/navegador.
- **Decisión:** rechazada.

### Corrección local posterior a L129 — 2026-09-17
- `runBatchJobsState` devuelve página/contexto vigentes; `fullRenewal` conserva el reemplazo headless→visible antes de continuar con cookies/Fabric.
- El dispatch de renovación se centralizó en `dispatchRenewal` para que las entradas Graph y full usen los runners inyectables.
- `go test -count=1 ./...`, `go test -race -count=1 ./...`, `git diff --check` y LSP pasan.

### Octava revisión formal Sol — 2026-09-17 (L129)
- Suite completa, race detector y `git diff --check` pasan; LSP no reporta errores de compilación; no se ejecutó navegador ni adquisición real.
- L126–L128 eliminan el retorno incondicional, agregan una operación Graph local y seams de dispatch, pero el gate se rechaza nuevamente.
- Después de adquirir Graph, `runBatchJobs` cierra el contexto que `fullRenewal` intenta reutilizar para cookies/Fabric; el recorrido posterior sigue roto en ese caso.
- El test de dispatch invoca directamente los callbacks inyectados y no ejecuta `run()` ni `fullRenewal`; el contador de navegador TUI no está conectado a ningún launcher y vale cero por construcción.
- **Decisión:** rechazada.

### Novena revisión formal Sol — 2026-09-17 (L134)
- Suite completa, race detector y `git diff --check` pasan; no se ejecutó navegador ni adquisición real.
- L130–L133 corrigen el ownership del modo batch, el cleanup diferido y conectan el launcher real del proceso helper en la TUI.
- El gate se rechaza: `fullRenewal` recibe página/contexto por valor y sólo actualiza sus variables locales después de `runBatchJobsState`; `run()` conserva el contexto original. Si Graph abre visible y Fabric ya existe, el contexto visible puede retornar sin que el cleanup exterior lo conozca.
- `TestRenewalDispatchSeamsExecutePublicEntryPoints` continúa llamando `dispatchRenewal` y los runners directamente, no `run()` ni el cuerpo real de `fullRenewal`. Ningún test referencia `launchPersistentContext`, por lo que el launcher de navegador agregado sigue sin evidencia ejecutable; la prueba TUI demuestra cero helper y una request local, pero no instrumenta el launcher del helper.
- **Decisión:** rechazada.

### Décima revisión formal Sol — 2026-09-17 (L140)
- Tests focalizados, suite completa, race detector y `git diff --check` pasan; LSP no reporta errores de compilación y sólo conserva hints/advisories no bloqueantes. No se ejecutó navegador ni adquisición real.
- L135–L139 hacen que `fullRenewal` devuelva página/contexto, ejecutan su cuerpo con hooks y exponen el launcher persistente.
- El gate se rechaza: el callback productivo convierte `auth.ErrIncompatibleCredential` en `errGraphInteractionRequired`, corta el lifecycle y publica la clasificación equivocada en lugar de permitir otro candidato o terminar con `incompatible_credential`.
- `runSetupHook` retorna antes del cuerpo productivo de `run()`; su test invoca manualmente `openPersistentContext` dentro del hook. Por tanto no demuestra dispatch, propagación, cleanup del contexto reemplazado ni cero launches del navegador con credencial vigente.
- **Decisión:** rechazada.

### Undécima revisión formal Sol — 2026-09-17 (L147)
- Tests focalizados, suite completa, race detector y `git diff --check` pasan; LSP no reporta errores de compilación. No se ejecutó navegador ni adquisición real.
- L141–L143 preservan `auth.ErrIncompatibleCredential` y cubren candidato incompatible seguido de válido. L144–L146 eliminan el short-circuit de `run()` y atraviesan setup, dispatch y cleanup inyectados.
- El gate se rechaza: `TestRunPublicEntryPointTraversesInjectedSetupAndDispatch` sustituye `graphRenewalRunner` por un callback que no ejecuta el batch, por lo que no alcanza `runBatchJobsState`, la transición visible, la propagación del contexto reemplazado ni su cleanup. Además, el launcher inicial usa `runOpenPersistentContext`, mientras el fallback productivo usa directamente `openPersistentContext`.
- **Decisión:** rechazada.

### Duodécima revisión formal Sol — 2026-09-17 (L149)
- L148 unifica el seam de launcher, pero sustituye `runBatchJobsState` en el test público y prueba el terminal incompatible desde `runBatchJobSequence`, no desde el callback productivo.
- Suite, race, diff check y LSP pasan; no se ejecutó navegador ni adquisición real.
- **Decisión:** rechazada; conectar ambas evidencias al cuerpo real de `runBatchJobsState`.

### Decimotercera revisión formal Sol — 2026-09-18 (L151)
- L150 alcanza `runBatchJobsState` desde `run()` y su callback productivo de persistencia; tests focalizados, suite, race, diff check y LSP pasan.
- El test de lifecycle devuelve `nil` para el contexto inicial y visible y sólo cuenta cierres, por lo que no prueba que el estado visible reemplazado vuelva a `run()` ni que el cleanup final cierre ese contexto específico.
- El test incompatible busca substrings independientes y no afirma la secuencia JSONL exacta, su orden ni ausencia de duplicados. El test público tampoco restaura `os.Stdout` después de ejecutar `run()`.
- **Decisión:** rechazada.

### Decimocuarta revisión formal Sol — 2026-09-18 (L153)
- L152 corrige identidad/cleanup del contexto y decodifica los eventos terminales, pero la prueba incompatible falla aislada porque no inicializa el estado global completo del protocolo.
- **Decisión:** rechazada; reutilizar `withBatchEvents` y la comparación exacta existente.

### Decimoquinta revisión formal Sol — 2026-09-18 (L155)
- L154 corrige L153: la prueba aislada pasa y valida exactamente versión, batch ID, generación, orden, proveedor, estado y código de los dos eventos; suite, shuffle repetido, race, diff check y LSP pasan.
- El gate se rechaza porque la prueba no restaura `runBatchGraphCapture`, `runBatchGraphOutcome` ni `persistGraphCandidate`; deja seams productivos globales contaminados para tests posteriores.
- **Decisión:** rechazada; restaurar únicamente esos tres seams con `t.Cleanup` y repetir el gate técnico.

### Decimosexta revisión formal Sol — 2026-09-18 (L157)
- L156 guarda y restaura mediante `t.Cleanup` exactamente `runBatchGraphCapture`, `runBatchGraphOutcome` y `persistGraphCandidate`; no modifica producción.
- La prueba aislada, veinte ejecuciones con orden aleatorio, suite completa, race detector y `git diff --check` pasan. LSP no reporta errores y conserva sólo un hint no bloqueante ajeno al cambio.
- La revisión del delta confirma que el cleanup queda registrado antes de sustituir los seams y se ejecuta también ante `Fatal`; la secuencia terminal exacta de L154 se conserva sin contaminación posterior.
- Evidencia y decisión completas: L156–L157 en `auth-migration-lessons.md`.
- **Decisión del gate:** Etapa 5 formalmente aprobada en L157.

---

## 7. Etapa 6 — Renovación durante el uso (Historial)

### Primera revisión formal Sol — 2026-09-18 (L161)
- Suite completa, race detector y `git diff --check` pasan; LSP no reporta errores.
- El gate se rechaza: la generación `0` se confunde con “sin intento” y bloquea la primera renovación; `tea.ResumeMsg` borra todos los intentos y el timer encola directamente, por lo que puede reintentar automáticamente estados no recuperables; un fallo terminal del lote no replantea otros proveedores elegibles.
- Los replies de thread no guardan `failedDraft` antes de limpiar el editor; los errores de red/timeout no advierten resultado incierto.
- **Decisión:** rechazada.

### Segunda revisión formal Sol — 2026-09-18 (L163)
- Suite completa, veinte ejecuciones aleatorias de `internal/ui`, race detector y `git diff --check` pasan; LSP no reporta diagnósticos.
- L162 corrige la presencia explícita para generación cero y conserva el draft real del reply en modo thread.
- El gate se rechaza: `renewalTimerMsg` no consulta si ese proveedor/generación ya fue intentado; `provider_failed/network_unavailable` no alimenta la reanudación selectiva y el error agregado del batch pierde la causa por proveedor; `messageSendErrMsg` continúa sin advertencia de resultado incierto ante red/timeout.
- **Decisión:** rechazada.

### Tercera revisión formal Sol — 2026-09-18 (L165)
- Corrección local en L164: `renewalTimerMsg` comparte gate con rechazos reactivos; `provider_failed` conserva causas individuales; `tea.ResumeMsg` sólo libera `network_unavailable`; `messageSendErrMsg` advierte incertidumbre en mutaciones.
- Tests focalizados, suite completa, veinte ejecuciones aleatorias de `internal/ui`, race detector, `git diff --check` y LSP pasan.
- Evidencia y decisión completas: L164–L165 en `auth-migration-lessons.md`.
- **Decisión del gate:** Etapa 6 formalmente aprobada en L165.

---

## 8. Etapa 7 — Fabric y validación final (Historial detallado)

### Corte 7A — Revisiones formales Sol (L169–L174)

- **Primera revisión formal Sol (L169 — 2026-09-18):** rechazada. `--renew fabric` capturaba candidato pero `saveTokens` lo omitía anunciando guardado; `PersistTokenValues` aceptaba silenciosamente Fabric; `ValidateFabricCandidate` permitía permisos vacíos o degradación.
- **Segunda revisión formal Sol (L171 — 2026-09-18):** rechazada. Faltaba evidencia contractual para recurso/aplicación y permisos de escritura.
- **Correcciones L171 y tercera revisión formal Sol (L174 — 2026-09-18):** aprobada. Fallback manual explícito, advertencia de envío a servidor, `PersistTokenValues` rechaza Fabric genérico, `ValidateFabricCandidate`/`PersistFabricCandidate` fallan cerradas (`incompatible_credential`) hasta política contractual reproducible. Corte 7A cerrado formalmente.

### Corte 7B — Evolución técnica y revisiones (L175–L220)

- **Preflight técnico (L175):** Definición de laboratorio sin red, contexto dedicado, bloqueo de Service Workers, route abort de escrituras Fabric.
- **Núcleo puro (L176 — 2026-09-20):** Implementación de matcher exacto fail-closed en `internal/fabricprobe`, cuarentena en memoria, correlación `Abort`/`requestfailed`/`response`. Aprobado localmente.
- **Adaptador Playwright (L177 — 2026-09-20):** `cmd/fabric-probe` con bordes inyectables; auditoría ChatSvc de miembros antes/después; offline inicial. Aprobado localmente.
- **Corrida real autorizada y descubrimiento PATCH (L181–L183 — 2026-09-21):** Primera prueba real user-run demostró aborto fail-closed, pero Teams Web moderno emitió `PATCH` en lugar de la forma legacy `POST`.
- **Diseño de huella sanitizada (L185–L191):**
  - L186 rechazó primera huella por serializar claves literales sin límite.
  - L188 rechazó segunda huella por `bodySchema` sin límite y colapso de claves duplicadas.
  - L191 corrigió presupuesto global, clasificación JSON y aprobó la huella local sin relajar matcher.
- **Primera observación user-run de huella (L193–L194):** Registró la huella del `PATCH` abortado, pero L195 demostró que faltaban valores exactos y clasificación de frame.
- **Diagnóstico local de frame y endpoint (L196–L201):**
  - L197 y L199 rechazaron primeras versiones del diagnóstico.
  - L200 corrigió borde de frame (`teams.cloud.microsoft`) y L201 aprobó formalmente el diagnóstico local.
- **Segunda observación user-run (L202–L203):** Confirmó frame `teams.cloud.microsoft` y coincidencias exactas de query/payload, pero `endpoint_match=false` permanecía agregado y faltaba correlación estricta de `requestfailed`.
- **Iteraciones de corrección local (L204–L217):**
  - L205 implementó corte; L206 lo rechazó en primera revisión.
  - L207 corrigió bloqueantes; L208 lo rechazó por subpredicados host/puerto acoplados.
  - L209 corrigió subpredicados; L210 lo rechazó por evidencia legacy `POST` contradictoria.
  - L211 corrigió evidencia legacy; L212 lo rechazó por `unchanged=true` anunciado prematuramente en `audit_after_ok`.
  - L213 corrigió cálculo; L214 lo rechazó porque secuencia `POST` exacta seguida de desconocida `PATCH` se acumulaba como mutación única con 2 intentos.
  - L215 corrigió secuencia (evento ambiguo aborta); L216 lo rechazó por evidencia agregada y verificación del segundo aborto.
  - L217 corrigió correlación por request y evidencia agregada; gate técnico y revisión formal Sol aprobadas para el corte **local** sin red.
- **Orden de lock vs resolución (L218–L220):**
  - L218 detectó que `cmd/fabric-probe` leía credenciales y resolvía target antes de adquirir el lock de perfil.
  - L219 corrigió el orden: lock → credenciales → resolución de target.
  - L220 (Revisión Sol): Aprobó el nuevo orden de L219, pero rechazó el preflight acumulado porque el lock está ligado a `configDir` mientras que el perfil abierto es una ruta independiente (desacople de lock vs perfil abierto).

---

## 9. Checklist histórica exhaustiva original (Sección 5)

- [x] Plan conceptual auditado.
- [x] Código actual contrastado con el plan.
- [x] Riesgos principales identificados.
- [x] Orden de ejecución y gates definidos.
- [x] Etapa 0 autorizada por el usuario.
- [x] Etapa 0 ejecutada y gate documentado.
- [x] Etapa 1 autorizada por el usuario.
- [x] Etapa 1 ejecutada; gate cerrado con fallback operativo `interaction_required`.
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
- [x] Bloqueantes L092 corregidos parcialmente por Luna y gate técnico repetido en L093.
- [x] Segunda revisión formal Sol de Etapa 5 ejecutada; gate rechazado en L094.
- [x] Bloqueantes L094 corregidos parcialmente por Luna y gate técnico repetido; evidencia en L095.
- [x] Tercera revisión formal Sol de Etapa 5 ejecutada; gate rechazado en L096.
- [x] Bloqueantes L096 corregidos parcialmente por Luna y gate técnico repetido; evidencia en L097.
- [x] Cuarta revisión formal Sol de Etapa 5 ejecutada; gate rechazado en L098.
- [x] Bloqueantes L098 corregidos por Luna en L099–L100; checks locales completados.
- [x] Seams/tests locales parciales de lifecycle y cancelación completados en L101–L105.
- [x] Quinta revisión formal Sol ejecutada; gate rechazado en L106.
- [x] Corregir persistencia vacía, Client ID/scopes del recorrido productivo; evidencia inicial en L107–L113.
- [x] Agregar secuencia JSONL exitosa con interacción intermedia y generación exacta; evidencia en L114.
- [x] Repetir gate técnico completo tras L114; evidencia en L115.
- [x] Unificar renovación Graph de la TUI con el lifecycle batch; evidencia en L116.
- [x] Verificar selección de Graph en el batch normal de la TUI; evidencia en L117.
- [x] Probar el punto público de renovación Graph de la TUI; evidencia en L118.
- [x] Actualizar la ayuda pública para reflejar Graph headless-first; evidencia en L119.
- [x] Cubrir la decisión de adquisición de `fullRenewal`; evidencia en L120.
- [x] Sexta revisión formal Sol ejecutada; gate rechazado en L122.
- [x] Propagar fallo de cierre antes de abrir visible y probar cero coexistencia ante error; evidencia en L123.
- [x] Rechazar host/origen con puerto explícito en el límite Graph; evidencia en L123.
- [x] Séptima revisión formal Sol ejecutada; gate rechazado en L125.
- [x] Eliminar retorno incondicional de `fullRenewal` y conservar recorrido post-Graph; L126, L130, L135.
- [x] Ejecutar cuerpo real de `fullRenewal` y `run()` con seams inyectados; evidencia L137 y L146.
- [x] Demostrar mediante cuerpo real de `runBatchJobsState` launcher único, máximo un contexto y cleanup; L152.
- [x] Octava revisión formal Sol ejecutada; gate rechazado en L129.
- [x] Novena revisión formal Sol ejecutada; gate rechazado en L134.
- [x] Décima revisión formal Sol ejecutada; gate rechazado en L140.
- [x] Conservar `incompatible_credential` en recorrido productivo; evidencia L141–L143.
- [x] Evidencia terminal exacta y autosuficiente conectada al callback de `runBatchJobsState`; L154.
- [x] Restaurar los tres seams productivos con `t.Cleanup`; L156.
- [x] Undécima revisión formal Sol ejecutada; gate rechazado en L147.
- [x] Duodécima revisión formal Sol ejecutada; gate rechazado en L149.
- [x] Decimotercera revisión formal Sol ejecutada; gate rechazado en L151.
- [x] Decimocuarta revisión formal Sol ejecutada; gate rechazado en L153.
- [x] Decimoquinta revisión formal Sol ejecutada; gate rechazado en L155.
- [x] Decimosexta revisión formal Sol ejecutada; gate formal de Etapa 5 aprobado en L157.
- [x] Etapa 6 autorizada explícitamente por el usuario en L158.
- [x] Contrato técnico previo de Etapa 6 definido por Sol en L159.
- [x] Implementación local y gate técnico de Etapa 6 ejecutados por Luna en L160.
- [x] Primera revisión formal Sol del gate de Etapa 6 ejecutada; gate rechazado en L161.
- [x] Correcciones L161 implementadas por Luna y gate técnico repetido en L162.
- [x] Segunda revisión formal Sol del gate de Etapa 6 ejecutada; gate rechazado en L163.
- [x] Correcciones focalizadas de L163 implementadas por Sol y gate técnico repetido en L164.
- [x] Tercera revisión formal Sol ejecutada; gate formal de Etapa 6 aprobado en L165.
- [x] Contrato técnico previo de Etapa 7 definido por Sol en L167.
- [x] Corte local 7A implementado con Luna; evidencia técnica en L168.
- [x] Primera revisión formal Sol del gate de 7A ejecutada; gate rechazado en L169.
- [x] Correcciones L169 implementadas y gate técnico local repetido en L170.
- [x] Segunda revisión formal Sol del corte 7A ejecutada; gate rechazado en L171.
- [x] Correcciones L171 implementadas y gate técnico local repetido en L172–L173.
- [x] Tercera revisión formal Sol del corte 7A ejecutada; gate aprobado en L174.
- [x] Preflight técnico de 7B definido por Sol sin implementar ni ejecutar el probe en L175.
- [x] Núcleo local sin red de 7B implementado y validado por Sol en L176.
- [x] Adaptador/binario de laboratorio de 7B conectado a Playwright y validado localmente en L177.
- [x] Resolución interactiva de targets y corrida real user-run de 7B ejecutada; hallazgo PATCH en L181–L183.
- [x] Borde productivo de frame corregido con Sol y gate técnico repetido en L200.
- [x] Tercera revisión formal Sol del diagnóstico ejecutada y aprobada en L201.
- [x] Segunda observación user-run autorizada, preparada y ejecutada; evidencia y gate rechazado en L202–L203.
- [x] Contrato técnico del corte local posterior a L203 definido por Sol en L204.
- [x] Implementación local de L204 por Luna y gate técnico en L205.
- [x] Revisiones Sol L206, L208, L210, L212, L214, L216 y correcciones intermedias L207, L209, L211, L213, L215 completadas.
- [x] Corrección focalizada de L216, gate técnico sin red y revisión formal Sol del corte local completados en L217.
- [x] Autorización de implementación del hotfix L218 concedida; orden lock→credenciales→resolución corregido en L219.
- [x] Revisión formal Sol de L219 ejecutada en L220: orden de lock aprobado; preflight acumulado rechazado por lock no ligado al perfil abierto.
