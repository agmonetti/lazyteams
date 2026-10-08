# Runbook operativo: migración de autenticación

## Estado del documento

- **Estado:** Operativo. Etapas 0 a 6 cerradas y aprobadas formalmente. Corte 7A aprobado. En Etapa 7B (probe aislado de Fabric en laboratorio).
- **Etapa actual:** **7B**. El núcleo puro y el adaptador `cmd/fabric-probe` están implementados y verificados sin red. Las observaciones reales user-run revelaron mutaciones `PATCH` y confirmaron frame `teams.cloud.microsoft`. L217 aprobó la correlación por request y evidencia de aborto fail-closed. L219 corrigió el orden de lock (lock → credenciales → resolución de target).
- **Bloqueante activo / Próximo paso (L220):** La revisión Sol L220 aprobó el orden de lock de L219, pero rechazó el preflight acumulado porque el lock está ligado a `configDir` mientras que el perfil Firefox es una ruta independiente (desacople lock vs. perfil). El paso inmediato es vincular el lock a la ruta real del perfil abierto, probar dos configuraciones que apunten al mismo perfil y repetir el gate técnico sin red y revisión formal Sol.
- **Salvaguardas estrictas:** Los agentes **nunca** ejecutan el probe real ni abren navegadores/tráfico real de Microsoft. No preparar ni ejecutar otra observación real hasta aprobar el preflight y contar con autorización user-run explícita. `PATCH`, captura final, promoción, persistencia y 7C siguen bloqueados.
- **Historial de revisiones:** Para la bitácora exhaustiva de revisiones formales pasadas (L000–L220) y micro-checkpoints, consultar `auth-migration-runbook-history.md`. Para lecciones aprendidas, consultar `auth-migration-lessons.md`.
- **Documentos fuente:** `plan.md` y `auth-migration-research.md`.

### Regla de versionado durante la migración

- Trabajar en la rama `auth-fast-migration-research` o rama hija dedicada.
- Commits pequeños, frecuentes y con diff revisado; nunca commitear `tokens.env`, cookies, perfiles Firefox, cachés Playwright ni secretos.

### Prioridad de producto

El criterio principal es que el usuario pueda ejecutar `lazyteams`, ver la TUI y usar la primera capacidad disponible sin esperar la autenticación completa. La renovación silenciosa de Graph es una optimización; `interaction_required` es un fallback válido. No se mantendrá un navegador, daemon o servicio permanente.

---

## Política de modelos

### Available models

| Model | Limit | Assigned work |
| --- | --- | --- |
| GPT-5.6 Sol | Limited | High-complexity reasoning: architecture, planning, deep debugging, security analysis, repository-wide reviews, ambiguous tasks, and final validation. |
| GPT-5.6 Luna | Limited | Fast execution: implementation, refactoring, tests, documentation edits, repetitive multi-file changes, hotfixes, and well-defined tasks. |
| gemini-3.8-flash (Gemini) | Unlimited | Mechanical tasks, repetitive code, JSON schemas, fixtures, examples, web research, CVE lookups, and boilerplate. |

### How model switching works

The active agent is one model and cannot switch itself. When a task requires a different model, the active agent must stop and tell the user explicitly before doing anything else:

```text
SWITCH REQUIRED
Model needed: [Sol / Luna / Gemini]
Reason: [one sentence]
Task to resume: [what to ask the next model to do]
Pending work this session: [what was completed so far]
```

### When to stop and request a switch

Request **Sol** when: evaluating architectural tradeoffs; checking cross-document consistency; deep debugging/root-cause analysis; security boundaries or threat modeling; repository-wide analysis; documenting new decisions; final validation or review of substantial implementation; or ambiguous tasks.

Request **Luna** when: implementing Go code from an approved plan; refactoring without architectural changes; writing/updating tests; documentation edits with clear targets; focused hotfixes; repetitive multi-file changes; or routine repository tasks with well-defined acceptance criteria.

Stay on **Gemini** when: JSON schemas; fixture examples; boilerplate structs without logic; web research / CVE lookups; repetitive mechanical tasks.

---

## 1. Objetivo y resultado esperado

LazyTeams debe abrir la TUI con las credenciales disponibles, usar cada capacidad apenas su dependencia esté lista y pedir interacción sólo cuando Microsoft o el tenant la hagan necesaria.

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

---

## 2. Arquitectura actual relevante

| Área | Ubicación | Situación observada |
| --- | --- | --- |
| Entrada | `main.go` | Inicia con credenciales parciales; no bloquea la TUI si faltan secundarias. |
| Credenciales | `internal/auth/auth.go` | Parser único, metadata versionada y reemplazo atómico de `tokens.env`. |
| Cliente | `internal/graph/client.go` | Snapshot inmutable publicado mediante `atomic.Pointer`. |
| Helper | `cmd/auth-helper/main.go` | Helper por lotes con protocolo IPC JSONL y eventos incrementales. |
| TUI | `internal/ui/` | Manejo de capacidades parciales, preflight de comandos y lifecycle batch. |
| Perfil | `internal/helpers/ownership*.go` | Ownership mediante lock asesor exclusivo del SO; sin `pkill`. |
| Fabric Probe | `cmd/fabric-probe`, `internal/fabricprobe` | Laboratorio aislado, matcher fail-closed, abort de mutaciones y auditoría. |

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
| Miembros de canales privados | Fabric + Web | Solicitar Fabric sólo al entrar en esa operación. |

---

## 3. Orden y estado de las etapas

```text
Etapa 0 [Cerrada] ──> Etapa 1 [Cerrada/Fallback] ──> Etapa 2 [Cerrada] ──> Etapa 3 [Cerrada]
   ↓
Etapa 4 [Cerrada] ──> Etapa 5 [Cerrada] ──> Etapa 6 [Cerrada] ──> Etapa 7 [ACTIVA: 7B]
```

### Resumen ejecutivo de etapas completadas (0 a 6)

* **Etapa 0 — Aislamiento y seguridad:** Entorno seguro en laboratorio persistente (`~/lazyteams-auth-lab/`, permisos `0700`), logging sanitizado sin secretos y perfil productivo protegido. *Aprobada el 2026-09-15 (L004–L007).*
* **Etapa 1 — Medición Graph Explorer:** Sesión headless no renovó silenciosamente en el laboratorio; D002 cerrada con fallback operativo `interaction_required`. *Cerrada el 2026-09-15 (L009–L012).*
* **Etapa 2 — Snapshot, persistencia y ownership:** `internal/auth` único punto de parsing/escritura; reemplazo atómico `0600`; lock asesor de SO contra colisiones; sin `pkill`. *Aprobada por Sol el 2026-09-16 (L043).*
* **Etapa 3 — Arranque progresivo y capacidades:** TUI abre de inmediato con credenciales parciales; estados por proveedor; preflight evita requests HTTP si falta capacidad; aislamiento de fallos EDU/Graph. *Aprobada por Sol el 2026-09-16 (L057).*
* **Etapa 4 — Helper por lotes y eventos incrementales:** Comando `--renew-batch`; protocolo IPC JSONL en stdout (`version:1`); actualización incremental de la TUI antes de `completed`; deduplicación `web_notif`. *Aprobada por Sol el 2026-09-16 (L088).*
* **Etapa 5 — Graph headless-first:** Renovación silenciosa con cuarentena de candidatos; rechazo de `localStorage`; host exacto sin puerto; fallback visible con context replacement; sin procesos huérfanos. *Aprobada por Sol el 2026-09-18 tras 16 revisiones (L157).*
* **Etapa 6 — Renovación durante el uso:** Timer reactivo para expiración contractual (margen 5 min); retries acotados a `network_unavailable`; conservación de drafts ante error; mutaciones inciertas no se reintentan automáticamente. *Aprobada por Sol el 2026-09-18 (L165).*

*(Detalle completo de cada revisión formal intermedia en `auth-migration-runbook-history.md`).*

---

## Etapa 7 — Fabric y validación final (ETAPA ACTIVA)

### Estado de autorización

- **Corte 7A (Endurecimiento local):** Aprobado por Sol en L174. Fabric excluido de bootstrap, batch y auto-renovación; `PersistTokenValues` rechaza Fabric genérico; persistencia Fabric específica falla cerrada (`incompatible_credential`).
- **Corte 7B (Probe aislado de laboratorio):** Núcleo puro (`internal/fabricprobe`) y adaptador (`cmd/fabric-probe`) aprobados localmente sin red (L176–L177). Hallazgo real de método `PATCH` (L183). Diagnóstico de frame/endpoint y mitigación fail-closed de eventos ambiguos aprobados localmente (L217). Orden lock→credenciales→resolución corregido y aprobado en L220.

### Contrato técnico activo de 7B

1. **Aislamiento y red:** El probe corre únicamente en laboratorio `0700` con lock exclusivo y Service Workers bloqueados.
2. **Matcher y abort fail-closed:** Intercepta requests `/fabric/`; permite `GET`, `HEAD`, `OPTIONS`; **aborta toda mutación**. Si una mutación desconocida ocurre tras un intento elegible, clasifica `ambiguous_event` y aborta.
3. **Auditoría inmutable:** Compara existencia y rol de miembro en ChatSvc antes y después mediante `GET`. Requiere `unchanged=true` y verificación byte por byte de `tokens.env`.
4. **Estado mínimo:** `prepared` → `route_installed` → `audit_before_ok` → `target_aborted` → `request_failed_confirmed` → `context_closed` → `audit_after_ok` → `write_candidate_unverified`. Fallos terminan en `rejected`.
5. **Cuarentena:** El candidato nunca se persiste automáticamente en 7B.

### Encargo obligatorio al usuario para pruebas reales

Los agentes **tienen terminantemente prohibido** ejecutar pruebas interactivas que abran navegadores o interactúen con Microsoft. Las pruebas reales deben ser encargadas explícitamente al usuario, quien reporta únicamente:
- Comando, timestamps y exit code.
- Apertura/cierre limpio y ausencia de huérfanos.
- Confirmación de aborto de request y ausencia de `response`.
- Auditoría antes/después (`unchanged=true`) y `tokens.env` idéntico.
- Log sanitizado (sin tokens, cookies, URLs completas ni IDs).

### Bloqueante activo (L220) y próximo paso de implementación

* **Problema detectado (L220):** `cmd/fabric-probe` asocia el lock a `configDir`, pero el perfil de Firefox se abre desde una ruta independiente (`profileDir`). Dos ejecuciones con configuraciones distintas pero mismo perfil podrían ejecutarse concurrentemente.
* **Acción requerida:**
  1. Vincular la adquisición del lock a la ruta canónica del perfil abierto (`profileDir`).
  2. Implementar pruebas unitarias/integración sin red que demuestren que dos configuraciones apuntando al mismo perfil respetan el lock.
  3. Repetir el gate técnico local sin red y someter a revisión formal Sol.
  4. Mantener bloqueadas las corridas reales hasta resolver el preflight y recibir autorización explícita.

### Corte 7C — Promoción o fallback (Bloqueado)

- Promoción a credencial utilizable sólo si existe prueba contractual no mutante de permiso de escritura y compatibilidad estricta. De lo contrario, permanece como `write_candidate_unverified` y fallback manual.

### Gate de salida de Etapa 7

- Cero capturas incidentales de Fabric en arranque o batch.
- Requests de lectura nunca producen candidatos ni tocan `tokens.env`.
- Route instalada antes de navegar; Service Workers bloqueados; mutaciones abortadas con `requestfailed` sin `response`.
- Auditoría y snapshot demuestran cero cambios (`unchanged=true`).
- Revisión formal Sol aprobada.

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

---

## 5. Política de evidencia y checklist ejecutiva

Toda acción debe registrarse en `auth-migration-lessons.md` siguiendo la plantilla LNNN.

- [x] **Etapa 0:** Aislamiento y seguridad del laboratorio documentados (L004–L007).
- [x] **Etapa 1:** Prueba Graph Explorer ejecutada; fallback `interaction_required` documentado (L009–L012).
- [x] **Etapa 2:** Snapshot atómico, parser único y lock asesor del SO aprobados por Sol (L043).
- [x] **Etapa 3:** Arranque progresivo, desacople de capacidades y preflight aprobados por Sol (L057).
- [x] **Etapa 4:** Helper por lotes `--renew-batch` y eventos JSONL aprobados por Sol (L088).
- [x] **Etapa 5:** Renovación Graph headless-first y fallback visible aprobados por Sol (L157).
- [x] **Etapa 6:** Renovación durante el uso, drafts y manejo de suspensión aprobados por Sol (L165).
- [x] **Corte 7A:** Endurecimiento local y persistencia fail-closed aprobados por Sol (L174).
- [x] **Corte 7B (Núcleo y diagnóstico):** Probe local, detección PATCH y mitigación aprobados por Sol (L177, L201, L217).
- [x] **Corte 7B (Orden de lock):** Lock previo a credenciales/target aprobado por Sol (L219, L220).
- [ ] **Corte 7B (Preflight pendiente):** Vincular lock a ruta de perfil (L220) y validar concurrencia.
- [ ] **Corte 7B (Observación real):** Corrida user-run autorizada tras preflight aprobado.
- [ ] **Corte 7C:** Promoción o fallback definitivo de Fabric.
- [ ] **Cierre de migración:** Matriz de aceptación validada end-to-end por Sol.
