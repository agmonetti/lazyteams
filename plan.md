# Plan de autenticación rápida para LazyTeams

## Versionado de la investigación

Todo el trabajo de esta migración se mantiene, por ahora, en la rama `auth-fast-migration-research` o una rama hija explícitamente creada. Se deben hacer commits pequeños y frecuentes por cambio coherente, revisando antes `git status`, el diff y las pruebas relevantes. Nunca se commitean tokens, cookies, perfiles, cachés ni logs sensibles. A fecha 2026-09-16 no hay commits propios de la migración; los cambios existentes siguen sin commit hasta su revisión.

## Conclusión

**Sí: hay margen concreto para que el uso habitual sea “abrir y empezar”, sin rehacer toda la autenticación ni perder la compatibilidad con tenants universitarios. Éste es el objetivo principal; la autenticación silenciosa universal no lo es.**

Mi recomendación es **conservar Graph Explorer y la sesión real de Teams**, pero cambiar cómo se administran:

1. Reutilizar credenciales vigentes sin abrir Firefox.
2. Intentar renovar Graph silenciosamente antes de pedir interacción.
3. Habilitar cada función apenas estén disponibles sus credenciales, sin esperar las demás.
4. Reutilizar un único navegador durante cada lote de renovaciones.
5. Sacar la captura interactiva de Fabric del arranque normal.

**No prometería “todos los tokens siempre listos sin interacción”.** MFA, consentimiento, revocación y políticas de sesión siguen dependiendo de Microsoft y del tenant. Fabric, además, tiene un problema específico de adquisición que merece una investigación separada.

No modifiqué código ni documentación. Contrasté los flujos locales con la documentación oficial del proyecto, revisé el código público de Graph Explorer y abrí su versión desplegada en un navegador headless aislado, sin iniciar sesión ni acceder a credenciales.

---

## 1. Qué entendí del proyecto y qué conservaría

El proyecto actual es **LazyTeams**, módulo `github.com/agmonetti/lazyteams`, con Go 1.26.6. El historial de msTTui explica las decisiones, pero varias descripciones quedaron atrás respecto del código actual.

La arquitectura relevante es:

| Capa | Responsabilidad |
|---|---|
| TUI, Bubble Tea | Estado, navegación, composición, eventos y resultados asíncronos. |
| `internal/graph` | Microsoft Graph **y** las APIs internas de Teams, EDU y medios. |
| `internal/auth` | Lectura de credenciales y datos del token. |
| `cmd/auth-helper` | Navegador, sesión Microsoft y captura de credenciales. |
| Persistencia | Tokens, perfil de Firefox y preferencias locales. |

La aplicación no tiene una única dependencia de autenticación:

- Graph sostiene el listado de equipos, canales y chats, presencia, directorio y archivos.
- ChatSvc sostiene mensajes, hilos, reacciones, lectura y notificaciones.
- AMS permite imágenes inline sin depender de los permisos de archivos de OneDrive.
- EDU sostiene Assignments.
- Spaces y Fabric cubren distintas operaciones administrativas.

**No reemplazaría esa arquitectura por una aplicación OAuth nueva como requisito general.** Cambiar el método de login no concede los permisos que el tenant niega, ni convierte un token de otra aplicación en el token de Graph Explorer con los permisos que ya funcionan.

Tampoco mezclaría esta iniciativa con WebSockets, renderizado o una reescritura general de la TUI.

---

## 2. Por qué hoy tarda: problemas concretos de coordinación

La TUI **ya tiene ejecución asíncrona**: `Model.Init()` lanza validación y cargas con `tea.Batch`. Por eso, “pasarlo a background” no es una solución nueva.

El problema está en qué se espera, qué se renueva y cuándo se aprovecha el resultado.

### Hallazgos principales

| Hallazgo | Evidencia | Consecuencia |
|---|---|---|
| El helper completo empieza con una estructura de captura vacía. | `cmd/auth-helper/main.go:443` | No decide primero qué credenciales guardadas puede reutilizar; vuelve a recorrer la captura. |
| El recorrido introduce demoras temporizadas antes de activar funciones. | `main.go:1314–1428` del helper | Notificaciones se fuerzan después de 20 s y EDU después de 40 s. El loop tiene un plazo de 90 s. |
| Graph se trata como un paso posterior al recorrido de Teams. | Helper `main.go:1113–1195` | Una credencial esencial queda detrás de capturas que no son necesarias para abrir un chat. |
| Renovar Graph siempre vuelve a abrir Firefox visible. | Helper `main.go:528–583` | No existe un intento previo de renovación headless en esa rama. |
| `web` y `notif` renuevan juntos, pero la cola los trata como trabajos distintos. | Helper `main.go:514–516`; UI `update.go:1213–1246` | Si ambos entran en la cola, pueden ejecutarse dos recorridos que se superponen en lo que capturan. |
| Parte de la recuperación espera a vaciar la cola completa. | `internal/ui/update.go:217–235` | Graph puede estar renovado y, aun así, equipos/perfil seguir esperando mientras se procesa EDU. |
| El arranque rechaza credenciales parciales. | `internal/auth/auth.go:87–95` | Si falta Graph, Web o Cookie, ni siquiera se abre una TUI que pueda guiar y completar el proceso. |

También hay dos problemas de confiabilidad que afectan directamente la espera:

- **Captura incompleta no siempre significa error del helper.** `renewalErr` se utiliza para EDU, pero otras ramas pueden terminar guardando una captura parcial y saliendo sin error. La TUI interpreta principalmente el resultado del proceso.
- **“Token saludable” hoy es una clasificación muy limitada.** `CheckAllTokens()` devuelve faltantes/401 para cuatro tipos; no representa permisos insuficientes, estado desconocido por falta de red o funcionalidades no aplicables.

Los tiempos anteriores son **esperas y plazos del código, no un benchmark de la cuenta**. Pero muestran que parte de los minutos no proviene necesariamente del servidor.

---

## 3. Graph: el hallazgo más importante

### Graph Explorer sí tiene renovación silenciosa

Revisé su código público y contrasté señales con el bundle desplegado:

- Usa **MSAL Browser 4.5.1**.
- Configura **`localStorage`**.
- Su capa de autenticación utiliza **`acquireTokenSilent()`**.
- Su fallback interactivo utiliza `prompt: 'select_account'`.

Fuentes: [configuración MSAL de Graph Explorer](https://github.com/microsoftgraph/microsoft-graph-explorer-v4/blob/48604f7b000d60220a8eb00836024fee3c1bb118/src/modules/authentication/msal-app.ts#L15-L24) y [flujo de adquisición](https://github.com/microsoftgraph/microsoft-graph-explorer-v4/blob/48604f7b000d60220a8eb00836024fee3c1bb118/src/modules/authentication/AuthenticationWrapper.ts#L168-L244).

**Esto no demuestra que el tenant permita todas las renovaciones silenciosas. Sí demuestra que abrir Graph visible en cada renovación no es una necesidad impuesta por la implementación de Graph Explorer. Actualmente es una decisión del helper.**

### Guardar el perfil no garantiza conservar toda la autenticación

Microsoft documenta un cambio relevante desde MSAL v4:

> La caché de autenticación en `localStorage` puede estar cifrada con una clave dependiente de una cookie de sesión, `msal.cache.encryption`. Sin “Keep me signed in”, cerrar el navegador puede hacer que esa caché deje de ser recuperable.

[Documentación de caché MSAL](https://learn.microsoft.com/en-us/entra/msal/javascript/browser/caching#localstorage-notes).

Eso obliga a separar tres cosas:

1. **Access token guardado:** sirve hasta su vencimiento o revocación.
2. **Caché MSAL de Graph Explorer:** puede permitir renovarlo.
3. **Sesión Microsoft/SSO:** puede permitir recuperar autenticación sin volver a escribir credenciales, incluso si se perdió la caché de la aplicación.

**[INFERENCE] Esa dependencia de la cookie es una causa plausible de los logins repetidos, no una causa confirmada en el perfil.**

Además, el helper tiene caminos que eliminan `browser-session/` cuando interpreta un cierre o interrupción como captura incompleta (`main.go:448–490`). Eso puede destruir una sesión aprovechable por un problema parcial.

### Qué investigaría primero

Un experimento específico, después de aprobar la implementación:

1. Autenticar Graph Explorer en el perfil dedicado.
2. Comprobar adquisición sin interacción dentro de esa sesión.
3. Cerrar Firefox limpiamente.
4. Reabrir **headless**, con el mismo perfil.
5. Distinguir:
   - cuenta recuperada y renovación silenciosa;
   - sesión SSO recuperable;
   - selector de cuenta;
   - credenciales/MFA requeridos;
   - consentimiento faltante.
6. Repetir cuando el access token haya vencido y en una sesión de otro día.

No asumiría que `window.msalInstance` existe: en la página desplegada no encontré una instancia de autenticación expuesta globalmente. **La primera opción sería utilizar el flujo nativo de Graph Explorer y capturar su resultado, no depender de internals de su bundle.**

Tampoco propondría “guardar `sessionStorage` y listo”: Graph Explorer ya configura `localStorage`, y el problema puede estar en otro nivel.

---

## 4. Propuesta: disponibilidad por función, no una barrera de “todos los tokens”

### A. Una sola entrada para el usuario

El usuario ejecuta:

```text
lazyteams
```

La aplicación:

- abre la TUI;
- carga las credenciales disponibles;
- utiliza las que sirven;
- inicia el helper cuando realmente hace falta;
- muestra una acción de login sólo si la recuperación silenciosa no alcanza.

`lazyteams-auth` sigue existiendo como herramienta de diagnóstico y autenticación explícita. **No debería ser un paso manual obligatorio antes de cada uso.**

### B. Definir qué necesita cada función

| Función | Dependencias principales |
|---|---|
| Listar equipos, canales y DMs | Graph. |
| Abrir un chat desde esas listas y enviar mensajes | Graph para navegación/identidad; Web para mensajería. |
| Actividad | Notif, con reutilización del Web token ya contemplada por el proyecto. |
| Archivos de canales | Graph. |
| Imágenes inline | Web/AMS. |
| Assignments | EDU y las cookies que requiera ese flujo; ciertas operaciones de archivos también utilizan Graph. |
| Administración de equipos/canales | Graph y/o Spaces según la operación. |
| Gestión de miembros de canales privados | Fabric, con dependencias de resolución en Web/Graph. |

**Consecuencia:** esperar Fabric o EDU no debería impedir utilizar equipos y mensajes.

No significa eliminar funciones. Significa prepararlas independientemente y, cuando corresponda, precargarlas después de habilitar el uso principal.

Para un caso educativo, **EDU tendría prioridad inmediatamente después del núcleo de mensajería**, no quedaría abandonado hasta que el usuario lo descubra manualmente.

### C. Arranque con credenciales vigentes: ningún navegador

La primera decisión debe ser local.

Guardar junto a la credencial, cuando esté disponible:

- vencimiento informado por la adquisición;
- cuenta y tenant;
- recurso/aplicación de procedencia;
- permisos concedidos;
- generación de la credencial.

Preferir esos metadatos a depender de decodificar todos los tokens como JWT. Microsoft recomienda tratar los access tokens como opacos y utilizar los metadatos de adquisición; esto también evita inventar un vencimiento para Fabric/JWE.

Un token no vencido **todavía puede estar revocado**. Por eso:

- se utiliza para la petición real que la aplicación necesita;
- si el servidor lo rechaza por autenticación, se renueva;
- no se agrega una ronda obligatoria de consultas duplicadas antes de mostrar datos.

### D. Renovación incremental

El helper debe entregar resultados aprovechables antes de terminar todo el lote:

```text
Graph ready       → cargar perfil, equipos y chats
Web ready         → habilitar mensajería
Notifications ready → cargar actividad
EDU ready         → cargar tareas
```

La TUI no debería esperar la salida del proceso para enterarse de que una credencial esencial ya está disponible.

Implementación conservadora:

- un helper por lote;
- persistencia atómica de cada conjunto coherente de credenciales;
- eventos privados de disponibilidad hacia la TUI;
- sin tokens en logs;
- cierre del navegador al terminar.

No hace falta empezar con un servicio permanente ni un demonio instalado en el sistema.

### Laboratorio de trabajo

El aislamiento no depende de que la raíz viva en `/tmp`. Para facilitar la interacción y conservar el estado entre reinicios, las pruebas autorizadas usarán un laboratorio persistente fuera del repositorio, por ejemplo `~/lazyteams-auth-lab/`, con una raíz protegida (`0700`) y esta separación:

```text
lazyteams-auth-lab/
├── probe/
├── runtime/home/
├── runtime/browser-session/
├── runtime/playwright/
└── runtime/logs/
```

Un wrapper configurará las rutas de `HOME`, `USERPROFILE`, Playwright y el perfil. El usuario podrá interactuar con Firefox visible cuando el flujo lo requiera. Los tokens, cookies y perfiles nunca se versionan; `/tmp` queda reservado para artefactos efímeros o evidencia histórica. Crear el laboratorio o ejecutar una nueva adquisición no autoriza por sí mismo ninguna etapa.

### E. Eliminar esperas ciegas

Cambiar condiciones basadas en tiempo por señales:

- Teams terminó de autenticar;
- apareció la navegación necesaria;
- se capturó una credencial;
- terminó la adquisición;
- apareció una pantalla que requiere intervención.

**Los timeouts quedan como límites de fallo, no como pasos obligatorios.**

Web y Notif deben gestionarse como un grupo de adquisición. Spaces se aprovecha cuando aparece durante ese recorrido, sin bloquear la mensajería por su ausencia.

### F. Graph silencioso primero, sin degradar permisos

Orden propuesto:

1. Credencial vigente.
2. Recuperación de la sesión de Graph Explorer en headless.
3. Adquisición nativa silenciosa.
4. Recuperación SSO cuando sea posible.
5. Navegador visible sólo para la intervención realmente necesaria.

La identidad del recurso no alcanza: **un token válido para Graph no garantiza tener los permisos que necesita LazyTeams**.

Hoy el interceptor principal comprueba audiencia y destino, pero no garantiza por sí solo procedencia de Graph Explorer y cobertura de permisos; otras ramas son todavía menos restrictivas.

La propuesta debe conservar la procedencia y los permisos conocidos. **Nunca reemplazar un token útil por otro de SharePoint, otra cuenta o un token con permisos insuficientes sólo porque `/me` responde.**

---

## 5. Fabric: cómo investigaría una mejora sin efectos secundarios

Actualmente se pide intentar cambiar un rol para capturar el token.

**Eso no es una operación inocua en todos los casos.** Si hay otros propietarios y la modificación es válida, puede realizarse. No la automatizaría silenciosamente.

Mi propuesta tiene dos partes:

### En el producto

- Fabric no bloquea el arranque.
- Se conserva mientras sea aprovechable.
- Su adquisición se solicita al utilizar una operación que lo necesita.
- La UI diferencia “mensajería disponible” de “administración privada requiere autenticación”.

### Investigación específica

**[INFERENCE] Una vía a probar es interceptar la solicitud original de Teams que ya contiene el token de escritura y abortarla antes de enviarla al servidor.**

Esto permitiría estudiar la adquisición sin ejecutar el cambio de rol. Playwright ofrece interceptación y aborto de solicitudes, pero tiene limitaciones con Service Workers: hay que demostrar que el bloqueo cubre realmente el recorrido utilizado.

[Contrato de interceptación de Playwright](https://playwright.dev/docs/api/class-browsercontext#browser-context-route).

Condiciones para aceptar esa solución:

- ninguna modificación llega al servidor;
- no cambia ningún rol ni membresía;
- el token corresponde realmente a la operación necesaria;
- no depende de dejar pasar primero una escritura para obtener un challenge;
- los reintentos tampoco dejan escapar la operación.

**Si esto no se demuestra, Fabric conserva un flujo explícito.** No vendería su captura automática como resuelta.

---

## 6. Fiabilidad necesaria para que la mejora no sea sólo cosmética

### Un propietario del perfil de Firefox

Hoy tanto la TUI como el helper llaman a `KillZombieBrowser()`. Eso no es un mecanismo de exclusión mutua: lanzar otro proceso puede interferir con uno legítimamente activo.

Antes de reutilizar el navegador por lotes:

- lock de aplicación para el perfil;
- ownership del proceso;
- un único camino de cancelación;
- recuperación de huérfanos sólo cuando se comprobó que lo son.

No lanzar varios Firefox sobre el mismo `browser-session/`.

### Actualización coherente de credenciales

`ReloadTokens()` modifica campos del cliente mientras existen comandos HTTP asíncronos.

La nueva coordinación debería usar **snapshots coherentes de credenciales**, con reemplazo sincronizado y lectura estable por petición. También unificar el parsing de arranque y recarga.

Esto evita mezclar:

- token nuevo con datos de identidad anteriores;
- credenciales de cuentas distintas;
- resultados tardíos de una generación anterior.

### Errores diferenciados

Como mínimo:

| Estado | Respuesta |
|---|---|
| Vencimiento/revocación recuperable | Renovar la credencial correspondiente. |
| Interacción necesaria | Mostrar la acción concreta. |
| Permiso insuficiente | Explicar qué función está bloqueada; no renovar indefinidamente. |
| Fallo de red | Conservar sesión y credenciales; reintento acotado. |
| Función no aplicable a la cuenta | No iniciar capturas inútiles. |

Un 403 o 404 no es universalmente “token vencido”. Los comportamientos particulares de Fabric o ChatSvc deben quedar identificados por proveedor y contexto.

Tampoco se debe repetir automáticamente un envío, upload o entrega cuya respuesta se perdió: eso puede duplicar una operación ya ejecutada.

---

## 7. Alternativas consideradas

| Alternativa | Evaluación |
|---|---|
| **Mejorar el helper existente y Graph silencioso** | **Primera elección.** Conserva las rutas y permisos que ya funcionan. |
| Renovar directamente desde Go con refresh tokens | No lo descartaría como experimento posterior, pero no es “guardar un refresh token y listo”: están vinculados al usuario/cliente, tienen reglas de SPA y siguen sujetos a revocación. No sería la base inicial. |
| Mantener Firefox abierto permanentemente | Reduce arranques y conserva estado en memoria, pero contradice el objetivo de bajo consumo. Como máximo, modo opcional medido. |
| Extensión del navegador con conexión local | Puede aprovechar una sesión que el usuario ya utiliza. Agrega instalación, permisos sensibles, soporte por navegador y posibles restricciones corporativas. Alternativa, no requisito. |
| Eliminar Graph y obtener todo de APIs internas | Investigación mucho más amplia: directorio, archivos, equipos y permisos siguen existiendo. No es el primer camino para corregir la latencia. |
| Nueva app OAuth, Device Code o “auto-consent” | No resuelve de forma general las restricciones reportadas. El consentimiento existente no se transfiere automáticamente a otro cliente. |

Microsoft también aclara que los refresh tokens de SPA tienen una ventana limitada y que la adquisición silenciosa nunca está garantizada frente a políticas o revocaciones: [refresh tokens](https://learn.microsoft.com/en-us/entra/identity-platform/refresh-tokens), [renovación MSAL](https://learn.microsoft.com/en-us/entra/msal/javascript/browser/token-lifetimes), [consentimiento](https://learn.microsoft.com/en-us/entra/identity-platform/permissions-consent-overview).

---

## 8. Plan de ejecución propuesto

| Etapa | Alcance | Criterio de salida |
|---|---|---|
| **1. Prueba de Graph y medición** | Instrumentar tiempos por etapa, sin secretos. Probar caché, cierre/reapertura, renovación silenciosa y SSO en el perfil dedicado. | Saber qué provoca interacción y cuánto cuesta cada recorrido. No confundir hipótesis con causas. |
| **2. Estado de credenciales y ownership** | `internal/auth`, cliente HTTP, lifecycle del helper. Metadatos, snapshots, errores tipados, lock y persistencia atómica. | Sin mezclas de cuentas, lecturas parciales ni helpers compitiendo. |
| **3. Arranque progresivo** | `main.go`, `model.go`, `commands.go`, `update.go`, `update_data.go`. Permitir entrada con credenciales parciales y recargar por función disponible. | Un fallo de EDU/Fabric no impide usar chats disponibles. |
| **4. Captura eficiente** | Helper por lotes, eventos de disponibilidad, Web/Notif agrupados, señales en lugar de sleeps, Graph silencioso primero. | No recapturar credenciales vigentes ni esperar el final del lote para aprovecharlas. |
| **5. Renovación durante el uso** | Adelantar renovación de credenciales necesarias próximas a vencer; replanificar tras suspensión y cambios de conectividad. | La expiración habitual no obliga a salir de la TUI. |
| **6. Fabric y validación final** | Experimento sin escrituras, pruebas reales por escenario y SO, documentación actualizada. | No afirmar captura automática de escritura hasta demostrarla. |

### Escenarios de aceptación

- Arranque con credenciales vigentes: **cero procesos de navegador**.
- Graph vencido con sesión recuperable: renovación sin credenciales manuales.
- Graph disponible mientras EDU falla: equipos y chats se cargan igualmente.
- Cuenta sin Education: no se espera indefinidamente Assignments.
- Web y Notif vencidos: una sola adquisición conjunta.
- Wi-Fi interrumpido: no se elimina el perfil ni se anuncia éxito falso.
- Salir durante una renovación: no queda Firefox huérfano.
- Dos instancias: no se matan los navegadores entre sí.
- Token capturado de otra cuenta o con permisos insuficientes: no reemplaza silenciosamente el conjunto válido.
- MFA/consentimiento realmente necesarios: una indicación clara y un único flujo interactivo.
- Renovación con mensajes/uploads en curso: sin duplicación ni pérdida del borrador.

El proyecto ya tiene tests y CI; aprovecharía esa base para las transiciones y errores. **La renovación real de Graph y Fabric necesita además verificación con la sesión Microsoft: los mocks no la demuestran.**

### Pruebas reales encargadas al usuario

Los agentes deben encargar al usuario toda prueba que ejecute un binario, abra Firefox, use una sesión Microsoft o genere tráfico contra un tenant. El encargo debe nombrar el escenario, el laboratorio, el comando y la acción visible exacta; el agente no debe asumir que una prueba local equivale a una prueba del producto.

El usuario debe devolver, sin secretos: comando/flags, timestamps, código de salida, estado final, pantallas o interacción requerida descritas en texto, cierre del navegador/helper y procesos huérfanos, ruta del log sanitizado, errores y timeouts clasificados, confirmación de que `tokens.env` no cambió y, cuando corresponda, auditoría antes/después con `unchanged=true/false`. Nunca debe devolver tokens, cookies, headers, claims, perfiles, URLs completas, payloads ni capturas con datos sensibles.

Para Fabric 7B debe informar además: si la request objetivo fue abortada, si hubo `requestfailed` sin `response`, si apareció una mutación no cubierta, si la auditoría quedó sin cambios y si el estado final fue `write_candidate_unverified` o `rejected`. El candidato no se persiste. Para TUI/renovación debe informar el estado de cada capacidad, el error visible y si la capacidad se recuperó sin reiniciar ni repetir mutaciones.

Una autorización de implementación o de tests sin red no habilita esta prueba. La autorización debe mencionar explícitamente el binario o producto, navegador/sesión real, laboratorio y tráfico Microsoft/tenant.

---

## 9. Qué experiencia buscaría

**Objetivos de diseño, no tiempos medidos ni garantías:**

| Situación | Objetivo |
|---|---|
| Credenciales vigentes | TUI inmediata; datos en los primeros segundos. |
| Access token vencido, sesión recuperable | Renovación en decenas de segundos, sin interacción innecesaria. |
| EDU pendiente | Mensajería utilizable mientras se completa. |
| Primer uso | Login y consentimiento explicados; sin minutos adicionales de esperas artificiales. |
| Sesión revocada/MFA exigida | Interacción inevitable, sin obligar a reiniciar la aplicación. |
| Fabric sin credencial de escritura | Administración privada pendiente; resto disponible. |

Hay una diferencia que conviene explicitar: **“ya puedo trabajar” y “absolutamente todas las funciones están autenticadas” no son el mismo hito.** Propongo optimizar ambos, pero no ocultar el segundo detrás de un mensaje engañoso de “todo listo”.

**El primer valor entregable debe ser el núcleo de arranque rápido:** TUI inmediata, capacidades parciales, snapshots coherentes, eliminación de esperas artificiales y deduplicación de renovaciones. Graph headless queda como optimización posterior y Fabric como investigación diferida; `interaction_required` es un fallback válido.
