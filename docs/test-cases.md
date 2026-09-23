# Casos de prueba - Vexillum

Documento vivo. La Capa 1 está a nivel de caso concreto (input, acción, resultado esperado): se prueba tal cual. Las Capas 2 a 4 están como criterios de aceptación de alto nivel: qué tiene que ser cierto para dar la capa por terminada. Cuando llegues a construir cada una de esas capas, bajás esos criterios a casos concretos con el mismo formato que la Capa 1.

Convención: cada caso tiene un id (`L1-01`), una precondición, una acción y un resultado esperado. Un caso pasa solo si el resultado esperado se cumple exacto.

---

## CAPA 1 - CLI base (casos concretos)

### `vexillum init`

**L1-01 - init en proyecto limpio**
Precondición: directorio que es un repo git, sin scaffold previo de vexillum, `~/.vexillum/` no existe.
Acción: correr `vexillum init`.
Esperado: se crea `~/.vexillum/`; se crea el scaffold local del proyecto (config local + `.claude/rules/vexillum.md` de producto); nunca lee ni escribe `AGENTS.md`/`CLAUDE.md` del proyecto, existan o no; salida confirma qué se creó; exit code 0.

**L1-02 - init es idempotente**
Precondición: un proyecto donde ya se corrió `init` con éxito.
Acción: correr `vexillum init` de nuevo.
Esperado: no se duplica ni se corrompe nada; la salida informa que ya estaba inicializado; no se sobrescribe ningún archivo sin aviso; exit code 0.

**L1-03 - init no sobrescribe cambios del usuario**
Precondición: un proyecto inicializado donde el usuario editó a mano `.claude/rules/vexillum.md`.
Acción: correr `vexillum init` de nuevo.
Esperado: `.claude/rules/vexillum.md` editado NO se pisa silenciosamente; si init quisiera regenerarlo, avisa y pide confirmación o lo deja intacto; exit code 0.

**L1-04 - init crea `~/.vexillum/` si falta pero el proyecto ya estaba**
Precondición: proyecto con scaffold local presente, pero `~/.vexillum/` borrado a mano.
Acción: correr `vexillum init`.
Esperado: recrea `~/.vexillum/` sin tocar el scaffold local existente; exit code 0.

**L1-04b - init sana un `.claude/rules/vexillum.md` faltante en un proyecto ya inicializado**
Precondición: proyecto ya inicializado (tiene `.vexillum/config.json`) pero sin `.claude/rules/vexillum.md` - por ejemplo borrado a mano, o inicializado con una versión de `vexillum` anterior a la migración a `.claude/rules/` (ver `binnacle/`, sesión de migración de `AGENTS.md`/`CLAUDE.md` a `.claude/rules/vexillum.md`: el viejo esquema dependía de que el proyecto no tuviera ya su propio `AGENTS.md`/`CLAUDE.md` con contenido ajeno, lo cual fallaba en proyectos reales - verificado en vivo contra `nutrione-api` y `nondeterministic`).
Acción: correr `vexillum init` de nuevo.
Esperado: crea el `.claude/rules/vexillum.md` faltante sin tocar `.vexillum/config.json` más que el hash; nunca toca `AGENTS.md`/`CLAUDE.md`, existan o no; la salida indica que se restauró un archivo faltante; exit code 0.

**L1-04c - `vexillum init --global` scaffoldea el rule file una sola vez para toda la máquina**
Precondición: `~/.claude/rules/vexillum.md` no existe.
Acción: correr `vexillum init --global`.
Esperado: no requiere estar en un repo git; crea `~/.claude/rules/vexillum.md` (mismo contenido que el local) y registra su hash en `~/.vexillum/config.json` (no en el `.vexillum/config.json` de ningún proyecto); no crea ningún scaffold con forma de proyecto bajo `~/`; exit code 0. Opt-in explícito, nunca el comportamiento por defecto de `vexillum init`.

**L1-05 - init fuera de un repo git**
Precondición: directorio que NO es repo git.
Acción: correr `vexillum init`.
Esperado: mensaje claro de que el directorio no es un repo git; decisión explícita del comportamiento (falla con mensaje, o inicializa pero advierte que las capas de worktrees lo van a requerir). El comportamiento elegido queda documentado y es consistente; el exit code refleja la decisión (0 si inicializa con warning, distinto de 0 si se decide fallar).

**L1-06 - init sin permisos de escritura en HOME**
Precondición: `~/.vexillum/` no se puede crear (permisos).
Acción: correr `vexillum init`.
Esperado: falla con mensaje claro que nombra el problema de permisos y la ruta; no deja estado a medias; exit code distinto de 0.

### `vexillum doctor`

**L1-07 - doctor con entorno completo**
Precondición: Claude Code en PATH, herdr en PATH, tmux en PATH, `~/.vexillum/` existe y es escribible, proyecto inicializado, directorio es repo git.
Acción: correr `vexillum doctor`.
Esperado: una línea por chequeo, todas en estado ok; resumen final "entorno listo"; no modifica nada; exit code 0.

**L1-08 - doctor con Claude Code faltante**
Precondición: Claude Code NO está en PATH; el resto ok.
Acción: correr `vexillum doctor`.
Esperado: la línea de Claude Code marca faltante; el resto ok; resumen final indica qué falta; exit code distinto de 0.

**L1-09 - doctor con herdr faltante**
Precondición: herdr NO está en PATH; el resto ok.
Acción: correr `vexillum doctor`.
Esperado: la línea de herdr marca faltante; resumen indica qué falta; exit code distinto de 0.

**L1-10 - doctor con tmux faltante**
Precondición: tmux NO está en PATH; el resto ok.
Acción: correr `vexillum doctor`.
Esperado: la línea de tmux marca faltante (backend de control); el resumen distingue que es el backend de control, no el primario; exit code refleja la política elegida (si tmux es opcional en v1, puede ser warning con exit 0; si se decide obligatorio, exit distinto de 0). La política queda documentada y es consistente.

**L1-11 - doctor en proyecto no inicializado**
Precondición: repo git sin scaffold de vexillum; binarios presentes.
Acción: correr `vexillum doctor`.
Esperado: la línea de "proyecto inicializado" marca faltante y sugiere correr `init`; exit code distinto de 0.

**L1-12 - doctor no modifica nada**
Precondición: cualquier entorno.
Acción: correr `vexillum doctor` y comparar el estado del disco antes y después.
Esperado: ningún archivo creado, modificado ni borrado por doctor; es estrictamente de lectura.

**L1-13 - doctor fuera de un repo git**
Precondición: directorio que no es repo git.
Acción: correr `vexillum doctor`.
Esperado: la línea de git marca que no es repo; resumen lo refleja; exit code según política (coherente con L1-05).

### Transversales Capa 1

**L1-14 - comando desconocido**
Acción: correr `vexillum finish-my-taxes`.
Esperado: mensaje de comando desconocido, referencia a `--help`; exit code distinto de 0.

**L1-15 - help**
Acción: correr `vexillum --help` y `vexillum <cmd> --help`.
Esperado: lista los comandos disponibles (`init`, `doctor`) con descripción; el help por comando explica qué hace; exit code 0.

---

## CAPA 2 - Estado en disco (casos concretos)

Struct `state.Task` (mission o scout), serializado a JSON en `~/.vexillum/tasks/<id>.json`. Sin workers todavía: se crea y se lee a mano (vía tests), invocado por el autor.

**L2-01 - guardar una tarea nueva**
Precondición: no hay tarea previa con ese id.
Acción: crear una `Task` con `state.New` y persistirla con `state.Save`.
Esperado: se crea `~/.vexillum/tasks/<id>.json`; el archivo es JSON válido.

**L2-02 - round-trip fiel (mission y scout)**
Precondición: una tarea creada y guardada.
Acción: `state.Load` sobre el id recién guardado.
Esperado: el struct reconstruido es idéntico al original, campo a campo, tanto para mission como para scout.

**L2-03 - archivo corrupto**
Precondición: un archivo `<id>.json` en `~/.vexillum/tasks/` con contenido que no es JSON válido.
Acción: `state.Load` sobre ese id.
Esperado: error claro, no panic, no datos basura.

**L2-04 - archivo incompleto**
Precondición: un archivo JSON válido pero sin `id` o `kind`.
Acción: `state.Load` sobre ese id.
Esperado: error claro que indica que la tarea está incompleta.

**L2-05 - versión de esquema no soportada**
Precondición: un archivo JSON válido y completo, pero con `schema_version` distinto al soportado.
Acción: `state.Load` sobre ese id.
Esperado: error claro que nombra la versión encontrada y la esperada; no se interpreta como datos válidos.

**L2-06 - escritura atómica**
Precondición: ninguna.
Acción: `state.Save` de una tarea.
Esperado: no queda ningún archivo temporal huérfano en `~/.vexillum/tasks/` tras una escritura exitosa; el único archivo presente es `<id>.json`.

**L2-07 - listar todas las tareas**
Precondición: varias tareas guardadas, mezcla de mission y scout.
Acción: `state.List` sobre `~/.vexillum/`.
Esperado: devuelve todas las tareas guardadas, sin omitir ni duplicar ninguna. Si no hay ninguna tarea guardada (o `tasks/` no existe), devuelve una lista vacía, no error.

**L2-08 - listar con un archivo corrupto**
Precondición: una tarea válida guardada y, además, un archivo corrupto en `~/.vexillum/tasks/`.
Acción: `state.List`.
Esperado: falla con un error claro que nombra el archivo problemático; no ignora la corrupción silenciosamente ni devuelve una lista parcial sin avisar.

## CAPA 3 - Un proceso (casos concretos)

Camp = un slot de un pool de worktrees reutilizables por proyecto (`internal/camp`, inspirado en treehouse). Soldier = el proceso que corre dentro de un camp (`internal/soldier`), con comando inyectable - en esta capa se prueba con comandos de test, no con el binario `claude` real (ver `docs/prd-v1.md`, Capa 3). Sin CLI todavía: se ejercita a mano, vía tests.

**L3-01 - crear un camp aislado**
Precondición: un proyecto git limpio, sin camps previos.
Acción: `camp.Acquire` para una tarea nueva.
Esperado: se crea un worktree en su propia rama (`vexillum/<task-id>`); el working tree del proyecto (rama actual, archivos) no se toca.

**L3-02 - lanzar el soldier y capturar salida + exit code (éxito)**
Precondición: un camp adquirido.
Acción: `soldier.Run` con un comando que termina con exit 0.
Esperado: el `Task` devuelto y el persistido en disco quedan en `status=done`, con la salida capturada y `exit_code=0`; se registra el camp asignado (`camp_slot`, `camp_path`, `camp_branch`).

**L3-03 - detectar y reflejar un fallo del proceso**
Precondición: un camp adquirido.
Acción: `soldier.Run` con un comando que termina con exit distinto de 0.
Esperado: `status=failed`, `exit_code` refleja el código real; `Run` no devuelve error de Go (es un resultado esperado del soldier, no una falla de vexillum); el camp queda con dueño claro en el pool (no huérfano), aunque no se libera automáticamente.

**L3-04 - escritura "running" antes de arrancar**
Precondición: un camp adquirido.
Acción: `soldier.Run` con un comando que tarda un momento en terminar; se lee el estado desde otro punto mientras el proceso sigue corriendo.
Esperado: el estado leído en pleno vuelo muestra `status=running`; si vexillum muriera en ese instante, el estado en disco no miente (queda "running", no "pending" ni un "done" falso).

**L3-05 - Release nunca destruye un camp sucio**
Precondición: un camp adquirido con cambios sin commitear.
Acción: `camp.Release`.
Esperado: falla con un error claro; el camp no se toca.

**L3-06 - Release nunca devuelve al pool un camp no aterrizado**
Precondición: un camp adquirido con commits que todavía no están mergeados en la rama base del proyecto.
Acción: `camp.Release`.
Esperado: falla con un error claro; el slot sigue leased.

**L3-07 - reuso del pool**
Precondición: un camp liberado (limpio y aterrizado).
Acción: `camp.Acquire` para una tarea nueva.
Esperado: reutiliza el mismo slot y el mismo directorio de worktree (no crea uno nuevo); dependencias/build cache del worktree quedan intactos porque nunca se borró.

**L3-08 - un slot leased nunca se reutiliza**
Precondición: un camp adquirido y todavía sin liberar.
Acción: `camp.Acquire` para una segunda tarea, sobre el mismo proyecto.
Esperado: se crea un slot distinto; el slot en uso no se toca.

**L3-09 - verificación de dueño del slot**
Precondición: un camp adquirido por la tarea A.
Acción: `camp.Release` invocado con el id de una tarea B distinta.
Esperado: falla con un error claro que nombra al dueño real; no libera el slot.

**L3-10 - aterrizar (Land) un camp limpio**
Precondición: un camp adquirido, con cambios commiteados, sin divergencia respecto a la rama base del proyecto.
Acción: `camp.Land`.
Esperado: el checkout principal del proyecto queda fast-forwardeado a la rama del camp (mismo HEAD); no crea merge commit, no fuerza nada.

**L3-11 - Land se niega ante una rama divergida**
Precondición: un camp adquirido y commiteado, pero la rama base del proyecto avanzó con commits propios después de que se creó el camp (ya no es fast-forward).
Acción: `camp.Land`.
Esperado: falla con un error claro que dice que hay divergencia y sugiere rebasear; el checkout del proyecto queda intacto (no mergea, no fuerza, no rebasea por su cuenta).

**L3-12 - Land se niega a tocar un checkout sucio**
Precondición: el checkout principal del proyecto (no el camp) tiene cambios sin commitear.
Acción: `camp.Land`.
Esperado: falla con un error claro; no toca el checkout del proyecto.

## CAPA 4 - Concurrencia

Capa grande, partida en pasos verificables (ver `docs/prd-v1.md`, sección Capa 4). Paso 1 - soldier real en un pane de herdr, todavía secuencial - tiene casos concretos abajo. Los pasos 2 a 5 (N en paralelo, sentinel, restart-proof, aislamiento de fallos) quedan como criterios de alto nivel hasta que se construyan.

### Paso 1 - soldier en un pane real de herdr (casos concretos)

`internal/herdr` (wrapper del CLI de herdr) + `soldier.RunInHerdr` (`internal/soldier`). Reemplaza el `os/exec` headless de la Capa 3 por una sesión interactiva real, corriendo con `--dangerously-skip-permissions` (decisión revisada: ver `docs/prd-v1.md`, sección Capa 4) - el control de seguridad real es la aprobación humana en `camp.Land` antes de aterrizar, no un bloqueo por permiso a mitad de tarea. `blocked` sigue siendo un estado posible (una pregunta genuina del agente), solo que ahora es la excepción. Sin CLI todavía, sin paralelismo todavía - un soldier a la vez, invocado a mano.

**L4-01 - correr un soldier de punta a punta**
Precondición: un camp adquirido, un workspace de herdr válido.
Acción: `soldier.RunInHerdr`.
Esperado: crea un tab+pane en el workspace dado (vía `herdr tab create`), arranca el agente, manda el prompt, y persiste el `Task` con `status=done`, los ids de herdr (`herdr_workspace_id`, `herdr_tab_id`, `herdr_pane_id`, `herdr_agent_name`) y el transcript capturado.

**L4-02 - escritura "running" antes de arrancar el agente**
Precondición: un camp adquirido.
Acción: `soldier.RunInHerdr`, inspeccionando el estado justo antes de que se llame `agent start`.
Esperado: el `Task` ya está persistido en `status=running` con el camp y los ids de herdr asignados, antes de que el agente arranque.

**L4-03 - reflejar un bloqueo**
Precondición: un camp adquirido; el agente termina su turno en estado `blocked` (necesita aprobación o input).
Acción: `soldier.RunInHerdr`.
Esperado: `Task.Status = blocked`, sin error de Go (es un resultado esperado, el sentinel lo va a escalar más adelante).

**L4-04 - diálogo de confianza en un camp nuevo**
Precondición: el agente queda `agent_not_ready` al arrancar, con el diálogo de "¿confiás en esta carpeta?" en pantalla.
Acción: `soldier.RunInHerdr`.
Esperado: reconoce el diálogo específico, lo descarta (`down`, `enter`), y sigue normalmente. Un bloqueo de arranque no reconocido (cualquier otro diálogo) **no** se adivina: falla con error claro, sin mandar teclas a ciegas.

**L4-05 - fallo al crear el pane**
Precondición: `herdr tab create` falla (workspace inválido, por ejemplo).
Acción: `soldier.RunInHerdr`.
Esperado: error de Go real; no se persiste ningún estado a medias (nunca se llegó a guardar `running`).

**L4-06 - cerrar el pane al liberar un camp aterrizado**
Precondición: un camp adquirido, con cambios commiteados y ya mergeados en la rama base (aterrizado).
Acción: `soldier.ReleaseInHerdr`.
Esperado: libera el slot del pool (`camp.Release`) **y**, recién ahí, cierra el tab de herdr - mismo momento, no antes.

**L4-07 - el pane queda abierto si el camp no se puede liberar**
Precondición: un camp adquirido con cambios sin commitear (dirty).
Acción: `soldier.ReleaseInHerdr`.
Esperado: falla (mismo motivo que `camp.Release` solo); el tab de herdr no se cierra - sigue habiendo algo para inspeccionar.

**L4-08 - choque de nombre de agente**
Precondición: dos tareas cuyos prompts generan el mismo slug (`vx-<slug>`), lanzadas en paralelo; la primera ya tiene ese nombre en uso en herdr.
Acción: `soldier.RunInHerdr` para la segunda tarea.
Esperado: `agent start` con el nombre candidato falla con `agent_name_taken`; se reintenta una vez con un nombre desambiguado (`vx-<slug>-<sufijo del id>`); ese es el nombre que se usa para el resto de la interacción y el que queda persistido en `Task.HerdrAgentName` - no falla toda la corrida.

### CLI real: `vexillum dispatch` / `land` / `release`

Reemplaza el binario descartable `tmp-demo/soldier-demo` (borrado). Mismo mecanismo que paso 1, ahora como subcomandos reales del binario (`internal/cli/dispatch.go`), con `projectDir`/`vexillumHome` resueltos igual que `init`/`doctor` (directorio actual / `~/.vexillum`), no pasados a mano.

**L4-09 - dispatch se niega en un proyecto no inicializado**
Precondición: repo git sin scaffold de vexillum (`.vexillum/config.json` ausente).
Acción: `vexillum dispatch "<prompt>"`.
Esperado: falla con mensaje claro que sugiere `vexillum init`; no crea ningún camp.

**L4-10 - dispatch de punta a punta**
Precondición: proyecto inicializado.
Acción: `vexillum dispatch "<prompt>" [--kind mission|scout]` (default `mission`).
Esperado: crea la tarea, adquiere el camp, corre `soldier.RunInHerdr`, imprime `task_id`, `status`, ids de herdr, y el transcript; exit code refleja si `RunInHerdr` tuvo un error real (no un `blocked`/`done` normal).

**L4-11 - land y release reales**
Precondición: una tarea despachada con `dispatch`, con commits landeables.
Acción: `vexillum land <task-id>` y después `vexillum release <task-id>`.
Esperado: ambos resuelven el camp desde `task.CampSlot` (vía `camp.Resolve`) y delegan en `camp.Land` / `soldier.ReleaseInHerdr` - mismas garantías de seguridad ya probadas en esos paquetes.

## Paso 2 - sentinel (casos concretos)

`internal/sentinel`: polling (no `events.subscribe` - mismo fallback que `firstmate` documenta como "permanente", ver `docs/prd-v1.md`), no daemon con push real. `vexillum sentinel` corre el loop en foreground; `vexillum sentinel drain` es lo que llama el hook `Stop` de Claude Code (agregado por `vexillum init` a `.claude/settings.json`).

**L4-12 - detectar una transición y registrar una wake**
Precondición: una tarea con `status=running` y `herdr_agent_name` asignado; el `agent_status` real en herdr cambió (ej. a `done`).
Acción: `sentinel.Tick`.
Esperado: persiste el nuevo status en el `Task`; registra una `Wake` sin ack en `~/.vexillum/wakes/<id>.json`.

**L4-13 - sin transición, sin wake**
Precondición: una tarea `running` cuyo `agent_status` real sigue siendo `working` (todavía no asentó).
Acción: `sentinel.Tick`.
Esperado: no registra ninguna wake, no toca el `Task`. (`working` mapea a `StatusRunning`, no a fallo - bug encontrado y corregido en esta misma sesión.)

**L4-14 - una wake se entrega una sola vez**
Precondición: una wake sin ack ya registrada.
Acción: `sentinel.Drain` dos veces seguidas.
Esperado: la primera devuelve la wake y la marca ack; la segunda no devuelve nada.

**L4-15 - drain sin wakes pendientes**
Precondición: ninguna wake sin ack.
Acción: `vexillum sentinel drain`.
Esperado: imprime `{}` (formato exacto que un hook `Stop` interpreta como "dejar terminar el turno normalmente").

**L4-16 - drain con wakes pendientes bloquea el `Stop`**
Precondición: al menos una wake sin ack.
Acción: `vexillum sentinel drain`.
Esperado: imprime `{"decision":"block","reason":"..."}` nombrando la tarea y la transición - el hook `Stop` de Claude Code impide que el turno termine e inyecta ese motivo como contexto.

**L4-17 - lock de instancia única**
Precondición: un `vexillum sentinel` ya corriendo (o un lock huérfano de un proceso muerto).
Acción: `sentinel.AcquireLock` de nuevo.
Esperado: se niega si el proceso dueño del lock sigue vivo (nombra el pid); si el pid ya no existe, reclama el lock sin problema (no queda bloqueado para siempre por un proceso muerto).

**L4-18 - `vexillum init` agrega el hook `Stop`**
Precondición: proyecto sin `.claude/settings.json`, o con uno existente con otros hooks/settings.
Acción: `vexillum init` (o `ensureSentinelHook` directamente).
Esperado: agrega el hook de `vexillum sentinel drain` sin pisar nada existente; correrlo de nuevo no duplica el hook; un `settings.json` con JSON inválido se deja intacto y se reporta como error, nunca se sobrescribe a ciegas.

**L4-19 - `dispatch` no se autoreporta para trabajo real**
Precondición: un soldier real que tarda más que el sondeo rápido (`quickSettleTimeoutMS`, 15s).
Acción: `vexillum dispatch`.
Esperado: vuelve tras el sondeo corto con `status=running`, no bloquea hasta que el soldier termina; el sentinel (auto-arrancado si no había uno corriendo) es quien detecta y persiste la transición final más tarde. Verificado en vivo (ver binnacle `20260920-sentinel-rediseno-async.md` y `20260920-bugs-primera-prueba-en-vivo.md`).

**L4-20 - el sentinel no confunde "recién arrancado" con "terminado"**
Precondición: un soldier recién despachado, con el sentinel corriendo en paralelo.
Acción: `sentinel.Tick` sondea el estado en vivo casi al mismo tiempo que `dispatch` somete el prompt.
Esperado: ignora cualquier tarea `running` cuyo `UpdatedAt` sea más reciente que `settleGracePeriod` (8s) - un `idle` leído en esa ventana (el agente todavía no arrancó a procesar el prompt) nunca se confunde con un asentamiento real. Bug real encontrado y arreglado en vivo (ver binnacle `20260920-bugs-primera-prueba-en-vivo.md`).

**L4-21 - despachar desde adentro de un camp se rechaza**
Precondición: el directorio de trabajo actual es un camp (`projectDir` está bajo `vexillumHome`).
Acción: `vexillum dispatch`/`init`/`upgrade`/`land`/`release`/`sentinel`.
Esperado: falla con un mensaje claro en vez de crear un pool de camps fantasma bajo el hash de la ruta del camp. Bug real encontrado y arreglado en vivo.

**L4-22 - un agente genuinamente desaparecido marca la tarea `interrupted`, no la deja `running` para siempre**
Precondición: una tarea `running` cuyo agente de herdr fue destruido de verdad (pane cerrado a mano, herdr reiniciado) - `AgentStatus` devuelve `agent_not_found`, no un error transitorio.
Acción: `sentinel.Tick`, repetido a lo largo de `notFoundConfirmWindow` (10s).
Esperado: la primera observación de `agent_not_found` solo registra el momento (`AgentNotFoundSince`), sin tocar el estado - evita condenar a una tarea por un hipo pasajero de herdr. Si sigue sin encontrarse pasado el período de confirmación, la tarea pasa a `state.StatusInterrupted`, se registra una wake, y se anota en `Output` que el pane desapareció. Si el agente se resuelve bien en el medio (era un hipo, no una desaparición real), la marca se limpia. Verificado en vivo: se cerró el pane de una mission real a mano, y quedó `interrupted` exactamente 10s después de la primera observación (ver binnacle `20260920-restart-proof-agente-desaparecido.md`).

**L4-23 - un `agent prompt` "atascado" se reintenta, no se trata como fallo**
Precondición: `agent prompt --wait` devuelve `agent_prompt_stalled` (herdr no observó `working`/`blocked` en su propia ventana interna de ~5s) - observado en vivo justo después de arrancar un agente recién creado, con el prompt nunca inyectado en el pane (confirmado leyendo la transcripción, vacía).
Acción: `soldier.RunInHerdr`.
Esperado: reintenta el envío del prompt hasta `promptStalledMaxAttempts` (3) antes de fallar - un reintento simple lo resolvió al instante en la prueba en vivo. Solo falla la tarea si sigue atascado después de agotar los reintentos.

**L4-24 - el nombre de fallback por colisión nunca excede el límite de herdr**
Precondición: el nombre candidato del agente ya está en el límite de 32 caracteres de herdr, y colisiona con un agente vivo (`agent_name_taken`).
Acción: `soldier.RunInHerdr` (vía `disambiguatedName`).
Esperado: el nombre de fallback (candidato + sufijo) se trunca para nunca superar 32 caracteres, en vez de que herdr lo rechace con `invalid_agent_name` - encontrado en vivo con el prompt "Write a Python module implementing a simple binary search tree..." (candidato de 32 caracteres exactos).

**L4-25 - una lectura de estado fallida no tumba el resto del tick**
Precondición: una tarea `running` cuyo `AgentStatus` falla con un error transitorio genérico (no `agent_not_found`, no un error estructurado de herdr).
Acción: `sentinel.Tick` sobre una lista con esa tarea (y, en el caso real, otras tareas junto a ella).
Esperado: la tarea con la lectura fallida se saltea (sin wake, sin cambio de estado) - `Tick` no devuelve error ni aborta el resto del loop; cualquier otra tarea en la misma corrida se procesa igual.

**L4-26 - el pane se cierra mientras se somete el prompt**
Precondición: `AgentPrompt` devuelve `agent_not_running` - confirmado en el changelog de herdr (`Cellar/herdr/0.9.0/CHANGELOG.md`): código que devuelve una llamada `--wait` específicamente cuando el pane objetivo se cierra mientras se espera. Encontrado en vivo en el primer intento de una prueba real de commander, antes de que nadie tocara nada.
Acción: `soldier.RunInHerdr`.
Esperado: falla la tarea (no tiene sentido reintentar - el pane está genuinamente cerrado, a diferencia de `agent_prompt_stalled`) con un mensaje explícito y accionable ("... safe to redispatch") en vez del string crudo de herdr. `sentinel.Tick` no necesita el mismo tratamiento: usa `AgentStatus` (sin `--wait`), que para un pane realmente cerrado siempre da `agent_not_found` (verificado matando el proceso subyacente a mano), nunca `agent_not_running` - los dos códigos están escopeados a formas de llamada distintas.

**L4-27 - `camp.Acquire` concurrente nunca colisiona un slot**
Precondición: N tareas dispatchadas al mismo tiempo (N procesos `vexillum dispatch` reales, o N llamadas concurrentes a `camp.Acquire` sobre el mismo pool).
Acción: N `camp.Acquire` en simultáneo.
Esperado: cada una obtiene un slot y un worktree genuinamente distintos; `pool.json` termina con exactamente N slots, cada uno arrendado por una sola tarea. Bug real encontrado: sin lock, `Acquire` hace un read-modify-write sin exclusión mutua sobre `pool.json` (leer, calcular el próximo slot libre, escribir) - dos llamadas concurrentes pueden leer el mismo estado antes de que ninguna escriba, calcular el mismo número de slot, y correr `git worktree add` en la misma ruta. Reproducido 100% de las veces con 8 llamadas concurrentes antes del fix (`internal/camp/camp_test.go`); arreglado con un `flock` exclusivo (`lockPool`) alrededor de la sección crítica. Verificado también con `vexillum dispatch` real: 3 procesos genuinamente concurrentes, cada uno con su slot/camp/rama/commit propios, sin colisión.

**L4-28 - `camp.Release` concurrente no pierde actualizaciones**
Precondición: N camps arrendados, liberados todos al mismo tiempo.
Acción: N `camp.Release` en simultáneo.
Esperado: los N terminan sin error, y `pool.json` refleja los N slots correctamente liberados - ninguna escritura se pisa con otra por la misma falta de exclusión mutua que L4-27. Mismo `lockPool` cubre este caso.

**L4-29 - un soldier que falla no afecta a sus hermanos**
Precondición: tres tareas `running` en el mismo `Tick`: una se asienta normal, otra tiene su agente confirmado desaparecido (pasado `notFoundConfirmWindow`), otra sigue genuinamente trabajando.
Acción: `sentinel.Tick` sobre las tres juntas.
Esperado: cada una termina en el estado que le corresponde de forma completamente independiente - la asentada en `done`, la desaparecida en `interrupted`, la que sigue trabajando se mantiene `running` - sin que ninguna interfiera con las otras. Verificado en vivo con el mecanismo real: 3 `vexillum dispatch` genuinamente concurrentes (con colisión de nombre real entre las tres, desambiguada correctamente), el pane de una cerrado a mano a propósito nada más arrancar. Resultado: la interrumpida marcó `interrupted` a los 10s exactos (igual que L4-22), sus archivos quedaron escritos pero sin comitear (el trabajo físico no se pierde, solo falta el commit); las otras dos comitearon limpio y se pudieron aterrizar/liberar sin ninguna interferencia de la tercera - cada camp se evalúa de forma completamente independiente.

## Pendiente

Con L4-27 a L4-29, los pasos 3 (N en paralelo), 4 (restart-proof) y 5 (aislamiento de fallos) de Capa 4 quedan formalizados y verificados en vivo. Lo único que sigue sin implementar de toda la Capa 4:

- ~~**Resumption real de una tarea `interrupted`**~~ - cerrado en v2 Parte A (ver `## V2 - PARTE A` abajo), pero como **re-dispatch, no resumption**: se descarta el camp del soldier muerto y se relanza desde el prompt original; no se retoma trabajo parcial ni sesión de agente. Decisión de alcance explícita del PRD v2 (A.2), no una implementación parcial del pendiente original.
- **Push real vía `events.subscribe`** en vez de solo polling - decisión consciente de quedarse con el fallback de polling como mecanismo permanente (ver binnacle `20260920-sentinel-paso2.md`), no un pendiente real.

## V2 - PARTE A (saneamiento)

### A.1 - Anclaje del contrato con herdr

**A1-01 - herdr reporta una versión 0.9.x soportada**
Precondición: `herdr` en PATH, `herdr --version` imprime `herdr 0.9.x`.
Acción: `vexillum doctor`.
Esperado: línea `[ok] herdr version - 0.9.x` (la versión detectada, siempre reportada, no solo cuando falla); exit code 0.

**A1-02 - herdr reporta una versión fuera de 0.9.x**
Precondición: `herdr --version` imprime una versión que no empieza con `0.9.`.
Acción: `vexillum doctor`.
Esperado: línea `[warn] herdr version - <versión> ...` nombrando que el contrato se verificó contra 0.9.x; **exit code 0** (advertencia, no fallo bloqueante - así lo pide el PRD explícitamente).

**A1-03 - `herdr --version` falla o da salida no parseable**
Precondición: `herdr --version` sale con error o imprime algo que no matchea el formato esperado.
Acción: `vexillum doctor`.
Esperado: línea `[warn] herdr version - could not determine herdr version ...`; exit code 0.

**A1-04 - herdr no está instalado**
Precondición: `herdr` no está en PATH.
Acción: `vexillum doctor`.
Esperado: solo la línea existente `[missing] herdr` de `checkBinary` - ninguna línea separada de "herdr version" (no se duplica la ausencia).

**Limitación conocida y aceptada** (documentada también en el código, `herdr.SupportedVersionPrefix`): esto detecta que la versión cambió, no que el parseo de `internal/herdr` sigue siendo correcto contra la versión nueva. Es un detector de cambio, no una verificación de contrato - el PRD v2 descarta explícitamente un test de contrato contra herdr real en CI para esta versión (sobreingeniería para uso personal).

### A.2 - Re-dispatch de tareas interrupted

**A2-01 - `camp.Discard` descarta un camp sucio y sin aterrizar**
Precondición: un camp arrendado con un commit sin aterrizar y cambios sin comitear encima.
Acción: `camp.Discard(camp, taskID)`.
Esperado: worktree vuelve a estar limpio; la rama del camp se borra (`git branch -D`); el slot vuelve al pool libre; un `Acquire` posterior con el mismo task id puede reusar ese slot, y el commit descartado ya no está presente.

**A2-02 - `camp.Discard` refuse un dueño equivocado**
Precondición: un camp arrendado por `task-1`.
Acción: `camp.Discard(camp, "task-2")`.
Esperado: error explícito, el slot sigue arrendado por `task-1` sin tocar nada - misma guarda que ya tiene `Release`, no se relaja para el camino destructivo.

**A2-03 - `vexillum redispatch` refuse una tarea que no está interrupted**
Precondición: una tarea en `done` (o cualquier estado que no sea `interrupted`).
Acción: `vexillum redispatch <task-id>`.
Esperado: error explícito nombrando el estado actual y qué hacer en cambio (aterrizar/liberar una `done`, no re-despachar); exit code distinto de 0; la tarea y su camp quedan sin tocar.

**A2-04 - `vexillum redispatch` sobre una tarea interrupted, caso feliz**
Precondición: una tarea `interrupted` con un camp real que tiene un commit sin aterrizar.
Acción: `vexillum redispatch <task-id>`.
Esperado: mismo task id (no se crea uno nuevo); `Redispatches` pasa de 0 a 1; `AgentNotFoundSince` queda en cero (crítico: si no se limpia, el sentinel puede volver a marcar `interrupted` la tarea recién re-despachada en el primer hipo de lectura, superando de inmediato la ventana de confirmación ya vencida); camp viejo descartado (commit sin aterrizar ya no presente); camp nuevo creado y soldier corriendo, exactamente como un `vexillum dispatch` fresco; termina en el mismo estado final que un lanzamiento que salió bien a la primera.

**A2-05 - `vexillum redispatch` no se bloquea si el pane viejo ya no se puede cerrar**
Precondición: una tarea `interrupted` cuyo `TabClose` sobre el pane viejo falla (el caso normal: ya está muerto, por eso está interrupted).
Acción: `vexillum redispatch <task-id>`.
Esperado: el re-dispatch igual termina bien - el cierre del pane viejo es best-effort, nunca bloquea el camino de recuperación.

**Nota de alcance, verificada contra el código real**: una tarea `interrupted` siempre tiene `CampSlot` ya asignado antes de poder llegar a ese estado (`sentinel.Tick` solo marca `interrupted` una tarea con `HerdrAgentName` no vacío, y `RunInHerdr` setea `CampSlot`/`HerdrAgentName` en el mismo bloque, antes del primer `state.Save`) - así que `runRedispatch` no necesita un camino de "tarea interrupted sin camp" para el caso real, solo se protege defensivamente si `CampSlot` es cero.

## V2 - PARTE B (integraciones AXI)

### B.1 - quota-axi

Investigación hecha antes de implementar (no supuesta): `github.com/kunchenguid/quota-axi` es un repo real; su propio README recomienda `npx skills add kunchenguid/quota-axi --skill quota-axi -g`. El mecanismo real de `npx skills add` (repo `vercel-labs/skills`) deja el skill instalado como `<destino>/.claude/skills/<nombre>/SKILL.md` - global (`-g`) en `~/.claude/skills/`, project-level (sin el flag) en `<proyecto>/.claude/skills/`. Sin manifest ni lockfile: la única forma de detectar instalación es un stat directo a ese archivo.

**B1-01 - quota-axi instalado globalmente**
Precondición: `~/.claude/skills/quota-axi/SKILL.md` existe; no hay nada a nivel proyecto.
Acción: `vexillum doctor`.
Esperado: línea `[installed] quota-axi` en la sección "AXIs".

**B1-02 - quota-axi instalado a nivel proyecto**
Precondición: `<proyecto>/.claude/skills/quota-axi/SKILL.md` existe; no hay nada global.
Acción: `vexillum doctor`.
Esperado: también `[installed] quota-axi` - cualquiera de las dos ubicaciones alcanza, a `doctor` no le importa cuál.

**B1-03 - quota-axi no instalado en ningún lado**
Precondición: ni `~/.claude/skills/quota-axi/` ni `<proyecto>/.claude/skills/quota-axi/` existen.
Acción: `vexillum doctor`.
Esperado: `[not installed] quota-axi - install with: npx skills add kunchenguid/quota-axi --skill quota-axi -g` - el comando exacto recomendado por el propio README de quota-axi.

**B1-04 - el estado de AXIs nunca afecta el exit code**
Precondición: entorno sano (todo lo demás en `ok`), probado con y sin quota-axi instalado.
Acción: `vexillum doctor` en ambos casos.
Esperado: exit code 0 en los dos, resumen "Environment ready" sin cambios - la sección AXIs es puramente informativa, ni siquiera al nivel de `warn` que tiene A.1.

**Alcance de B.1, según el PRD**: esto es todo lo que entra al binario. No hay lógica de cuota en Go, no hay gate de admisión de tareas, no hay instrucción sobre quota-axi escrita en `productAgentsMD` - una vez instalado, el propio `SKILL.md` le enseña al agente a usarlo. `doctor` solo reporta presencia, nunca instala.

### B.2 - lavish-axi

Investigación hecha antes de implementar: `github.com/kunchenguid/lavish-axi` es un repo real (3.8k estrellas, MIT). Su propio README recomienda `npx skills add kunchenguid/lavish-axi --skill lavish` - **sin** `-g` (a diferencia de quota-axi). El valor de `--skill` es `lavish`, no `lavish-axi` - el nombre de carpeta del skill no tiene por qué coincidir con el nombre del repo. Esto confirmó que no convenía asumir que el patrón de B.1 se repetía tal cual; `axiSkill` ganó un campo `Global` para que cada AXI declare su propio comando recomendado.

**B2-01 - lavish instalado (a nivel proyecto, su ubicación recomendada)**
Precondición: `<proyecto>/.claude/skills/lavish/SKILL.md` existe.
Acción: `vexillum doctor`.
Esperado: línea `[installed] lavish` en la sección "AXIs" (el nombre reportado es `lavish`, no `lavish-axi`).

**B2-02 - lavish no instalado**
Precondición: ni `~/.claude/skills/lavish/` ni `<proyecto>/.claude/skills/lavish/` existen.
Acción: `vexillum doctor`.
Esperado: `[not installed] lavish - install with: npx skills add kunchenguid/lavish-axi --skill lavish` - sin `-g`, a diferencia de la línea de quota-axi.

**Alcance de B.2, según el PRD**: igual que B.1 - solo `doctor` reporta presencia. Nada de lógica de lavish en el binario; el commander lo corre bajo demanda cuando el general se lo pide, aprendido de su propio `SKILL.md`.

### B.3 - chrome-devtools-axi

Investigación real: `github.com/kunchenguid/chrome-devtools-axi` recomienda `npx skills add kunchenguid/chrome-devtools-axi --skill chrome-devtools-axi -g` (mismo patrón que quota-axi: `--skill` = nombre del repo, con `-g`). Arquitectura: un proceso "bridge" persistente por sesión (`CHROME_DEVTOOLS_AXI_SESSION=<nombre>`), con estado en `~/.chrome-devtools-axi/sessions/<nombre>/bridge.pid`, cerrado con `CHROME_DEVTOOLS_AXI_SESSION=<nombre> chrome-devtools-axi stop`. A diferencia de B.1/B.2, este es "el único AXI" con acople real al core (PRD): un browser vivo por soldier no muere solo cuando su pane de herdr se cierra, y eso extiende el criterio de terminado de A.2.

**Verificado en la máquina real**: `herdr tab create --help` expone `--env <KEY=VALUE>` ("Set an environment variable for the launched process"), y acepta múltiples `--env` sin error de parseo. Esto es lo que permite namespacear el browser de cada soldier por su propio task id desde que arranca el pane, sin que el soldier tenga que hacer nada especial.

**B3-01 - chrome-devtools-axi instalado**
Precondición: `~/.claude/skills/chrome-devtools-axi/SKILL.md` existe.
Acción: `vexillum doctor`.
Esperado: `[installed] chrome-devtools-axi`.

**B3-02 - chrome-devtools-axi no instalado**
Precondición: no existe en ninguna ubicación.
Acción: `vexillum doctor`.
Esperado: `[not installed] chrome-devtools-axi - install with: npx skills add kunchenguid/chrome-devtools-axi --skill chrome-devtools-axi -g`.

**B3-03 - `soldier.RunInHerdr` namespacea el browser del soldier por task id**
Precondición: un dispatch (fresco o redispatch) cualquiera.
Acción: `RunInHerdr`.
Esperado: `CreateTab` recibe `CHROME_DEVTOOLS_AXI_SESSION=vx-<task-id>` como env - siempre, sin importar si la mission termina usando un browser o no (costo nulo si no lo usa).

**B3-04 - `stopOrphanBrowser` no invoca nada si nunca hubo un bridge**
Precondición: `~/.chrome-devtools-axi/sessions/vx-<task-id>/bridge.pid` no existe.
Acción: `DiscardInHerdr` (vía `redispatch`).
Esperado: cero invocaciones de `npx` - ni red, ni proceso lanzado. Caso común: la mayoría de las missions no usan browser.

**B3-05 - `stopOrphanBrowser` cierra un bridge real cuando hay evidencia**
Precondición: `~/.chrome-devtools-axi/sessions/vx-<task-id>/bridge.pid` existe (evidencia de que el soldier sí abrió un browser).
Acción: `DiscardInHerdr` (vía `redispatch`).
Esperado: se invoca `npx -y chrome-devtools-axi stop` con `CHROME_DEVTOOLS_AXI_SESSION=vx-<task-id>` en el entorno - scoped exactamente a la sesión de esa tarea, best-effort (un fallo acá no bloquea el redispatch, mismo criterio que ya rige para `TabClose`).

**Bug encontrado y arreglado (sin caso de prueba previo que lo cubriera)**: `ReleaseInHerdr` (el camino no destructivo, `vexillum release`) llamaba a `camp.Release` y a `TabClose`, pero nunca a `stopOrphanBrowser` - a diferencia de `DiscardInHerdr`, que sí lo hacía. Un soldier con browser que terminaba bien (camp limpio y aterrizado, release normal) dejaba el bridge de chrome-devtools-axi vivo indefinidamente; solo un soldier que terminaba mal (redispatch tras `interrupted`) tenía su browser limpiado. B3-06/B3-07 cubren el camino que faltaba.

**B3-06 - `ReleaseInHerdr` stopea el browser huérfano del soldier**
Precondición: camp limpio y aterrizado; `~/.chrome-devtools-axi/sessions/vx-<task-id>/bridge.pid` existe.
Acción: `ReleaseInHerdr` (vía `vexillum release`), solo después de que `camp.Release` haya tenido éxito.
Esperado: se invoca `npx -y chrome-devtools-axi stop` con `CHROME_DEVTOOLS_AXI_SESSION=vx-<task-id>` en el entorno, antes de `TabClose` - mismo scoping y criterio best-effort que B3-05.

**B3-07 - `ReleaseInHerdr` no invoca nada si nunca hubo un bridge**
Precondición: camp limpio y aterrizado; `~/.chrome-devtools-axi/sessions/vx-<task-id>/bridge.pid` no existe.
Acción: `ReleaseInHerdr` (vía `vexillum release`).
Esperado: cero invocaciones de `npx`. Caso común: la mayoría de las missions no usan browser.

**B3-08 - un `camp.Release` rechazado no toca el browser ni el pane**
Precondición: camp sucio (rechaza `camp.Release`); `~/.chrome-devtools-axi/sessions/vx-<task-id>/bridge.pid` existe.
Acción: `ReleaseInHerdr` (vía `vexillum release`).
Esperado: `ReleaseInHerdr` devuelve el error de `camp.Release` sin invocar `npx` ni `TabClose` - ni el browser ni el pane se tocan cuando el camp mismo no se puede devolver al pool.

**Alcance de B.3, según el PRD**: sin browser por defecto en ninguna mission - el soldier decide usarlo, vexillum solo le da el namespacing gratis vía el env var. Nada de lógica de scraping/automatización en el binario Go.

**Diferido a la tanda de pruebas en vivo**: confirmar con un browser real que `chrome-devtools-axi stop` efectivamente mata el proceso (los tests de arriba verifican que se invoca correctamente el comando correcto con el env correcto, no que un browser real muere - eso requiere el AXI instalado y un soldier real usándolo).

### B.4 - no-mistakes

Investigación real (vía GitHub API, no supuesta): `no-mistakes` es su propio repo standalone (`kunchenguid/no-mistakes`), no una feature interna de firstmate. Se instala por `curl` (no es un Agent Skill vía `npx skills add`). Pone un git remote local delante del real; `git push no-mistakes <branch>` corre su propio pipeline y abre el PR él mismo, sin gh-axi. Diseño completo, incluida la decisión abierta del PRD ya cerrada, en `docs/no-mistakes.md`.

**B4-01 - `no-mistakes` no instalado**
Precondición: el binario `no-mistakes` no está en PATH.
Acción: `vexillum doctor`.
Esperado: `[missing] no-mistakes - not found in PATH (optional...)`; exit code sin cambios (opcional, igual que los AXIs).

**B4-02 - `no-mistakes` instalado pero el proyecto no está gateado**
Precondición: el binario está en PATH, pero el proyecto nunca corrió `no-mistakes init` (no existe el remote git `no-mistakes`).
Acción: `vexillum doctor`.
Esperado: `[missing] no-mistakes - installed, but this project hasn't run 'no-mistakes init' yet - 'vexillum ship' will do this automatically the first time`.

**B4-03 - `no-mistakes` instalado y el proyecto gateado**
Precondición: el binario está en PATH y el proyecto tiene el remote `no-mistakes` configurado.
Acción: `vexillum doctor`.
Esperado: `[ok] no-mistakes - installed and this project is gated`; exit code 0.

**B4-04 - `vexillum ship` empuja una mission terminada a través del gate**
Precondición: una tarea `done`, camp con un commit real, proyecto con el remote `no-mistakes` apuntando a un bare repo real (sin necesitar el binario `no-mistakes` real ni GitHub real - el test usa un bare repo local).
Acción: `vexillum ship <task-id>`.
Esperado: `git push no-mistakes <camp-branch>` sale bien, exit code 0, salida confirma el push y dice cómo seguir el pipeline (`no-mistakes axi status` / la TUI) sin que vexillum lo supervise.

**B4-05 - `vexillum ship` refuse una tarea que no está done**
Precondición: una tarea `running` (o cualquier estado que no sea `done`).
Acción: `vexillum ship <task-id>`.
Esperado: error explícito nombrando el estado; exit code distinto de 0; nada se pushea.

**B4-06 - `vexillum ship` refuse un scout**
Precondición: una tarea `KindScout` en estado `done`.
Acción: `vexillum ship <task-id>`.
Esperado: error explícito ("a scout should never have committed anything to ship"); exit code distinto de 0.

**B4-07 - `vexillum ship` refuse cuando `no-mistakes` no está instalado**
Precondición: una tarea `done` válida, proyecto no gateado, y `no-mistakes` no está en PATH.
Acción: `vexillum ship <task-id>`.
Esperado: error explícito ("'no-mistakes' is not installed") ofreciendo `vexillum land` como alternativa; exit code distinto de 0; nada se pushea, ni se intenta gatear.

**B4-08 - `vexillum ship` auto-gatea un proyecto no gateado en el primer uso**
Precondición: una tarea `done` válida, proyecto no gateado, `no-mistakes` instalado (en el test, un stub cuyo subcomando `init` agrega el remote `no-mistakes` apuntando a un bare repo real).
Acción: `vexillum ship <task-id>`.
Esperado: corre `no-mistakes init` primero (salida lo menciona explícitamente), después el push sale bien - exit code 0, mismo mensaje de confirmación que B4-04. Decisión tomada con el general después de la primera implementación (que refusaba en vez de auto-gatear) - ver `docs/no-mistakes.md`, "Auto-gate en `ship`, no en `init`".

**B4-09 - un auto-gate fallido se reporta y no intenta el push**
Precondición: una tarea `done` válida, proyecto no gateado, `no-mistakes` instalado pero su `init` falla (ej. sin remote real configurado).
Acción: `vexillum ship <task-id>`.
Esperado: error explícito ("'no-mistakes init' failed") con la salida de la herramienta; exit code distinto de 0; ningún intento de push después de un gate fallido.

**Alcance de B.4, según el diseño cerrado en `docs/no-mistakes.md`**: el binario dispara el gate (si hace falta) y el push determinista (`vexillum ship`). Ninguna lógica de review/test/lint/docs en Go - eso es 100% de `no-mistakes`. Ninguna supervisión del pipeline post-push - el general usa las herramientas propias de la herramienta (`no-mistakes axi status`, la TUI). `no-mistakes init` nunca se corre desde `vexillum init` - vive en `ship`, lazy, en el primer uso.

**Diferido a la tanda de pruebas en vivo**: confirmar con el binario `no-mistakes` real y un repo con remote de GitHub real que el pipeline efectivamente corre y abre un PR de verdad - los tests de arriba verifican el `git push` determinista contra un bare repo local, no el pipeline completo de la herramienta.

## V3 - PARTE C (saneamiento)

### C.1 - Namespace por proyecto en ~/.vexillum

Problema cerrado: `tasks/` y `wakes/` eran globales por máquina (`~/.vexillum/tasks/`, `~/.vexillum/wakes/`), sin proyecto en `Task`/`Wake`. Con dos proyectos abiertos, `sentinel.Drain` (que devolvía y ackeaba *todas* las wakes) dejaba que el Stop hook del commander del proyecto A consumiera las wakes de un soldier del proyecto B. Nuevo layout: `~/.vexillum/projects/<key>/{tasks,wakes,camps}`, con `<key>` calculado por una sola función compartida (`internal/project.Key`, antes duplicada en `camp.Acquire`/`camp.Resolve`). `sentinel.pid`/`sentinel.log` siguen globales (un sentinel por máquina, que recorre todos los proyectos en cada `Tick`). `Task` no cambia de formato ni de `SchemaVersion` - el directorio es el filtro. `Wake` pierde el campo `Acked`: `Drain` borra el archivo de la wake al entregarla en vez de marcarlo.

**C1-01 - `project.Key` es estable para la misma ruta absoluta**
Acción: llamar `project.Key(path)` dos veces con el mismo `path`.
Esperado: mismo resultado las dos veces.

**C1-02 - `project.Key` difiere entre dos proyectos con el mismo nombre base**
Precondición: dos rutas absolutas distintas que terminan en el mismo nombre de carpeta (`.../a/myproject`, `.../b/myproject`).
Acción: `project.Key` sobre cada una.
Esperado: claves distintas - la clave hashea la ruta absoluta completa, no solo el nombre base.

**C1-03 - `project.Root` ubica el proyecto en `vexillumHome/projects/<key>`**
Acción: `project.Root(vexillumHome, projectDir)`.
Esperado: `filepath.Join(vexillumHome, "projects", project.Key(<projectDir resuelto>))`.

**C1-04 - `project.Root` normaliza symlinks antes de hashear**
Precondición: un directorio real y un symlink a ese mismo directorio, desde otra ubicación.
Acción: `project.Root(vexillumHome, real)` y `project.Root(vexillumHome, symlink)`.
Esperado: la misma raíz para los dos - lo que mantiene a `vexillum dispatch` (resuelve el proyecto desde `os.Getwd()` sin tocar symlinks) y `vexillum sentinel drain` (resuelve desde `git rev-parse --show-toplevel`, que sí los sigue) de acuerdo sobre el mismo proyecto en una máquina donde su ruta involucra un symlink (ej. `$TMPDIR` de macOS bajo `/var` → `/private/var`).

**C1-05 - `project.AllRoots` da lista vacía, no error, sin `projects/`**
Precondición: `vexillumHome` recién creado, sin ningún proyecto namespaceado todavía.
Acción: `project.AllRoots(vexillumHome)`.
Esperado: slice vacío, `err == nil`.

**C1-06 - `project.AllRoots` lista todos los proyectos namespaceados**
Precondición: dos proyectos con raíz creada bajo `vexillumHome/projects/`.
Acción: `project.AllRoots(vexillumHome)`.
Esperado: las dos raíces, sin importar el orden - es lo que `sentinel.Tick` recorre en cada barrido.

**C1-07 - `sentinel.Drain` borra el archivo de la wake al entregarla**
Precondición: una wake pendiente real, generada por un `Tick` que detectó una transición.
Acción: `sentinel.Drain(projectRoot)`.
Esperado: el archivo `<project root>/wakes/<task-id>.json` deja de existir después del drain - no queda marcado con ningún campo de "acked", simplemente se borra.

**C1-08 - `sentinel.Tick` recorre todos los proyectos y aísla sus wakes**
Precondición: dos proyectos distintos bajo el mismo `vexillumHome`, cada uno con una tarea `running` que settlea en el mismo `Tick`.
Acción: `sentinel.Tick(vexillumHome, client)`, después `sentinel.Drain` sobre cada proyecto por separado.
Esperado: 2 wakes en total; el drain del proyecto A solo devuelve la wake de la tarea de A, el de B solo la de B - la garantía central de todo este cambio: dos proyectos abiertos a la vez ya no comparten `tasks/` ni `wakes/`.

**C1-09 - `resolveDrainTarget` fuera de un repo git es un no-op**
Precondición: `cwd` no está dentro de ningún repositorio git.
Acción: `resolveDrainTarget(cwd, vexillumHome)`.
Esperado: `("", nil)` - ni error ni proyecto resuelto.

**C1-10 - `resolveDrainTarget` resuelve la misma raíz que usa `camp.Acquire`**
Precondición: un proyecto git real, inicializado.
Acción: `resolveDrainTarget(projectDir, vexillumHome)` y por separado `camp.Acquire(projectDir, vexillumHome, taskID)`.
Esperado: la raíz que devuelve `resolveDrainTarget` es exactamente el padre de `c.PoolRoot` (que ahora es `<project root>/camps`) - `dispatch` y `drain` concuerdan en el mismo proyecto.

**C1-11 - `resolveDrainTarget` funciona desde una subcarpeta del proyecto**
Precondición: un proyecto git real con una subcarpeta.
Acción: `resolveDrainTarget` desde la raíz del proyecto y por separado desde la subcarpeta.
Esperado: la misma raíz de proyecto en los dos casos - a diferencia de `dispatch`/`land`/`release`/`redispatch`/`ship`, que exigen correr desde la raíz exacta (usan `os.Getwd()` sin buscar el toplevel), `drain`/`await` funcionan desde cualquier subdirectorio porque resuelven vía `git rev-parse --show-toplevel`.

**C1-12 - `resolveDrainTarget` desde dentro de un camp es un no-op**
Precondición: un camp real, adquirido con `camp.Acquire` sobre un proyecto real.
Acción: `resolveDrainTarget(c.Path, vexillumHome)`.
Esperado: `("", nil)` - el toplevel de un camp es el propio worktree, que vive bajo `vexillumHome`; el Stop hook de un soldier corriendo en su propio camp no tiene nada que drenar para sí mismo.

**Bug encontrado en vivo por este caso (arreglado antes de que este test pasara)**: la comparación de "¿el toplevel está bajo `vexillumHome`?" (`refuseInsideVexillumHome`) comparaba el toplevel resuelto por git (que sigue symlinks al buscar hacia arriba) contra un `vexillumHome` sin resolver. En una máquina donde la ruta de `vexillumHome` involucra un symlink (el caso de cualquier test bajo el `$TMPDIR` de macOS, `/var` → `/private/var`), la comparación de prefijos fallaba en silencio y un camp real no se reconocía como tal. Arreglado normalizando `vexillumHome` con `filepath.EvalSymlinks` antes de esa comparación específica (`internal/cli/sentinel.go`, `resolveDrainTarget`).

**C1-13 - un drain desde un camp no toca las wakes reales del proyecto**
Precondición: un proyecto real con una wake pendiente genuina (generada por un `Tick` real); un camp real de ese mismo proyecto.
Acción: `resolveDrainTarget` con `cwd = c.Path` (simulando el Stop hook de un soldier corriendo en su propio camp).
Esperado: no se resuelve ningún proyecto (ver C1-12), y la wake pendiente del proyecto sigue intacta en `<project root>/wakes/` después del intento - un drain lanzado desde un camp nunca consume las wakes del proyecto real.

**Verificado en el código (no en test)**: los soldiers heredan el Stop hook committeado por `vexillum init` en `.claude/settings.json` solo si ese archivo está comiteado en el proyecto - `vexillum init` lo escribe pero nunca lo comitea, y `camp.Acquire` crea el worktree vía `git worktree add`, que solo refleja lo que ya está en el commit. Confirma el paréntesis del general: "si `.claude/settings.json` está commiteado, sí" heredan el hook.

**Riesgo aceptado, documentado en el código** (`internal/project.Key`): renombrar o mover el proyecto cambia la clave y deja huérfanas sus tareas/wakes/camps previas - no hay migración ni detección del layout viejo. El estado previo (si existía, de antes de este cambio) se borra a mano antes del primer uso real; no hay código ni tests para el layout anterior.

## V3 - PARTE D (model/effort por soldier)

### D.1 - `vexillum dispatch --model/--effort`

Inspirado en el `crew-dispatch.json` de firstmate (kunchenguid), pero respetando ADR-05: acá el matching de reglas en lenguaje natural nunca se reimplementa en Go. La tabla de reglas (`when` → `model`/`effort`) vive como markdown en `productVexillumRule` (sección "Choosing a model and effort", `internal/cli/init.go`), interpretada por el commander. El binario solo gana `--model`/`--effort` en `vexillum dispatch`, valida ambos contra el set fijo que acepta el `claude` real (confirmado en vivo con `claude --help`: `--model` admite `haiku`/`sonnet`/`opus`/`fable`; `--effort` admite `low`/`medium`/`high`/`xhigh`/`max`) y los pasa tal cual, sin leer el prompt. `state.Task` gana `Model`/`Effort` (`omitempty`, sin bump de `SchemaVersion` - mismo criterio que `Redispatches`/`AgentNotFoundSince`); un re-dispatch los conserva porque reutiliza el mismo `Task`.

**D1-01 - `parseDispatchArgs` acepta `--model`/`--effort` mezclados con el prompt**
Acción: `parseDispatchArgs` con `--model`, `--effort`, `--kind` y palabras del prompt en cualquier orden.
Esperado: `model`/`effort` extraídos correctamente, el resto de las palabras se unen como prompt; `--model`/`--effort` sin valor es error (`internal/cli/dispatch_test.go`, `TestParseDispatchArgs`).

**D1-02 - `soldier.ValidateModelEffort` rechaza valores desconocidos**
Acción: `ValidateModelEffort(model, effort)` con cada valor válido, con `""` (ambos opcionales) y con valores inventados.
Esperado: `nil` para cualquier combinación de valores válidos/vacíos; error para un valor fuera del set fijo (`internal/soldier/claude_args_test.go`, `TestValidateModelEffort`).

**D1-03 - los flags llegan verbatim al lanzamiento real de `claude`**
Acción: `runDispatch` con `--model haiku --effort low` contra un `fakeHerdr`; por separado, `soldier.RunInHerdr` con un `state.Task{Model: "haiku", Effort: "low"}`.
Esperado: `client.AgentStart` recibe `--dangerously-skip-permissions --model haiku --effort low`, en ese orden (`internal/cli/dispatch_test.go`, `TestRunDispatch_PassesModelEffortToClaude`; `internal/soldier/herdr_run_test.go`, `TestRunInHerdr_PassesModelAndEffort`).

**D1-04 - `soldier.ClaudeCommand` (path headless) hace el mismo passthrough**
Acción: `ClaudeCommand(task)` con y sin `Model`/`Effort` seteados.
Esperado: los args incluyen `--model`/`--effort` solo cuando están seteados en el task, en el mismo formato que el path herdr (`internal/soldier/claude_args_test.go`, `TestClaudeCommand_ModelEffort`, `TestClaudeCommand_NoModelEffort`).

**Verificado en vivo, no en test**: `vexillum dispatch "<prompt>" --model haiku --effort low` contra un proyecto real (`vexillum-prueba`), confirmando que el soldier arranca con esos flags en un pane de herdr real - ver la bitácora de la sesión.
