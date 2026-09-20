# 2026-09-19 - Capa 4 paso 1: ajustes tras la primera prueba real

Primera prueba real del flujo "hablale al commander en `vexillum-prueba`,
que despache un soldier vía `AGENTS.md`" (sin pasar el comando a mano).
Funcionó de punta a punta, pero el general encontró dos problemas de UX
reales corriéndolo.

## Resuelto y en pie

- **El commander quedaba bloqueado mientras el soldier corría** (~42s sin
  poder seguir hablando con el general). Causa: `soldier.RunInHerdr` usa
  `agent prompt --wait`, que es bloqueante por diseño, y no hay sentinel
  todavía que le avise al commander cuando termina sin que él mismo tenga
  que esperar. Arreglo inmediato (no de fondo): se actualizó la sección
  "Dispatching a soldier" del `AGENTS.md` de `vexillum-prueba` para
  indicarle al commander que lance el comando **en background** (la
  opción de ejecución en background de su propio Bash tool, no `&`/`nohup`
  manual), y que en el ínterin pueda consultar progreso sin bloquear con
  `herdr agent get <name>` / `herdr agent read <name>`. El arreglo de
  fondo (que el commander no tenga que acordarse de esto y directamente
  reciba el aviso) es el sentinel - paso 2, todavía sin construir.

- **Nombre de pane/agente ilegible**: era el id crudo de la tarea
  (`a827011b90275d77`). Ahora `internal/soldier/herdr_run.go:herdrAgentName`
  arma `vx-<slug del prompt>-<sufijo corto del id>` (ej.
  `vx-crea-un-archivo-a827`), usado tanto como nombre del agente como
  label del tab en herdr. El sufijo (6 hex del id de la tarea) garantiza
  unicidad entre agentes vivos (herdr lo exige); el slug es lo que hace
  legible la barra de tabs. Respeta el límite de 32 caracteres y el patrón
  `[a-z][a-z0-9_-]{0,31}` de herdr.

- Test actualizado (`TestRunInHerdr_Success`) para verificar el prefijo
  `vx-<slug>-` en vez de solo "empieza con letra minúscula". Build, vet,
  gofmt y tests verdes.

## Resuelto y en pie (segunda ronda de feedback, misma sesión)

- **El pane no se cerraba nunca al terminar.** Verificado contra
  `firstmate` (`docs/herdr-backend.md`, líneas 127 y 361-366): tampoco lo
  hace al terminar el turno - lo cierra recién como parte del *teardown*,
  junto con la devolución del worktree al pool ("committed work must be
  landed before the worktree is returned... cleanup closes only the exact
  recorded task pane"). Se agregó `soldier.ReleaseInHerdr` (llama a
  `camp.Release` y, solo si eso tiene éxito, `herdr.Client.TabClose` sobre
  el tab del soldier). Si `camp.Release` se niega (dirty, no aterrizado,
  dueño incorrecto), el pane se queda abierto - sigue habiendo algo para
  inspeccionar. Nuevo método `TabClose` en `herdr.Client`. Casos `L4-06` y
  `L4-07` en `docs/test-cases.md`, 2 tests nuevos con un camp git real.

- **Se sacó el sufijo de unicidad del nombre del agente/tab.** Era
  `vx-<slug>-<6 hex del id>`; ahora es solo `vx-<slug>` (ej.
  `vx-crea-un-archivo`), igual que `firstmate` usa directamente `fm-<id>`
  sin agregar nada más. Se acepta el riesgo teórico de colisión entre dos
  prompts que generen el mismo slug estando vivos al mismo tiempo - no
  aplica todavía porque no hay paralelismo (Capa 4 paso 3 sigue
  pendiente); si pasara, herdr devuelve un error claro al intentar
  `agent start` con un nombre ya en uso.

## Resuelto y en pie (tercera ronda: cablear `release` al demo)

- **`camp.Resolve(projectDir, vexillumHome, slot)`** (`internal/camp/camp.go`):
  reconstruye el `Camp` de un slot ya adquirido leyendo el estado del
  pool, a partir de solo el número de slot (lo único que queda persistido
  en `state.Task.CampSlot`). Necesario porque `Task` no guarda
  `PoolRoot`/`ProjectDir` - se recalculan de forma determinística, igual
  que hace `Acquire`. Test nuevo (`TestResolve_ReconstructsAcquiredCamp`)
  que verifica que reconstruye exactamente el mismo `Camp` que devolvió
  `Acquire`.

- **`tmp-demo/soldier-demo` pasa a tener dos subcomandos**: `run
  <project> <home> <prompt>` (lo que ya había) y `release <project>
  <home> <task-id>` (nuevo: `state.Load` la tarea, `camp.Resolve` con su
  slot, `soldier.ReleaseInHerdr`). `run` ahora también imprime el
  `task_id` al final, necesario para poder invocar `release` después.

- **`AGENTS.md` de `vexillum-prueba` actualizado**: instrucciones de
  `release` agregadas (cuándo llamarlo - recién cuando el trabajo ya está
  mergeado en la base -, qué pasa si se niega, qué devuelve al éxito).
  Corregida también la referencia al nombre del agente (ya no lleva
  sufijo, ver ronda anterior) y la sintaxis del comando `run` (ahora
  necesita el subcomando).

## Resuelto y en pie (cuarta ronda: ambigüedad "soldier" vs. Agent tool nativo)

- **El commander usó su propia herramienta "Agent" (subagentes nativos de
  Claude Code) en vez de correr `soldier-demo` vía Bash.** Resultado: no
  se abrió ningún pane, no se tocó `camp`/`herdr` para nada - el commander
  "lanzó un soldier" en un sentido completamente distinto al que
  vexillum define (un subagente interno de Claude Code, invisible para el
  general, no un proceso real en un worktree aislado). Causa: la
  instrucción del `AGENTS.md` no distinguía explícitamente "soldier" (el
  concepto de dominio de vexillum) de "Agent tool" (la herramienta nativa
  del harness con nombre parecido) - el modelo asumió que eran lo mismo.
  Arreglo: se agregó una advertencia explícita al principio de la sección
  de despacho ("Do NOT use your own Agent/Task tool... it is not a
  vexillum soldier") y se reforzó la instrucción final para decir "via
  Bash tool (no Agent/Task tool)". Vale la pena tener este mismo cuidado
  cuando se escriba el `AGENTS.md` de producto real (el que scaffoldea
  `init`), no solo en esta instancia de prueba.

## Pendiente para la próxima

- El arreglo de background es un parche a nivel de instrucción
  (`AGENTS.md`), no una garantía del sistema - si el general edita ese
  archivo o el commander no lo sigue, vuelve a bloquear. El sentinel
  (paso 2) es lo que lo hace estructural: el commander despacha y sigue,
  el sentinel le avisa cuando hay algo que atender.
- No se probó todavía qué pasa si el general le pide status al commander
  *durante* una corrida en background real (el flujo de "chequear sin
  bloquear" vía `herdr agent get/read` está documentado en el AGENTS.md
  pero no ejercitado en vivo).
- El flujo `run` → (revisar y mergear a mano) → `release` tampoco se
  probó todavía de punta a punta con el binario actualizado - falta un
  ciclo completo real antes de dar por cerrado el paso 1 de Capa 4.
- Si dos soldiers en paralelo (Capa 4 paso 3) generan slugs idénticos, el
  segundo `agent start` va a fallar con un nombre duplicado - no
  resuelto, anotado como riesgo conocido y aceptado por el general.
