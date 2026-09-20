# PRD - Vexillum v1

Orquestador de agentes de código por CLI. Binario en Go, instalable por `brew` o `curl`, multiplataforma. Uso personal primero, con la puerta abierta a hacerlo público.

Este PRD cubre la **v1** y está seccionado por capas de construcción. La Capa 1 está a nivel construible (arrancás por acá). Las Capas 2 a 4 están a nivel de intención: qué hacen, por qué y en qué orden, para detallarlas al llegar a cada una. Al final, las features diferidas.

---

## Glosario del producto

| Concepto | Término (código / CLI) |
|---|---|
| Binario / comando | `vexillum` |
| Orquestador (le hablás vos) | commander |
| Subagentes ejecutores | soldiers |
| El humano (vos) | general |
| Tarea que cambia código y aterriza local (`vexillum land`) | mission |
| Tarea que solo investiga y deja reporte | scout |
| Worktree aislado de cada tarea | camp |
| Componente de supervisión (watcher) | sentinel |
| Directorio de estado / config | `~/.vexillum/` |

Un solo vocabulario, en inglés, para código y para la voz del agente. Sin tabla de traducción: el agente detecta en qué idioma le escribís y responde en ese idioma, pero los términos del dominio (commander, soldier, mission) no se traducen.

Cadena de mando: **general** (vos) → **commander** (orquestador) → **soldiers** (ejecutores). El **sentinel** vigila a los soldiers y despierta al commander solo cuando hace falta.

---

## Contexto y decisiones marco

Estas decisiones están fijadas y no se relitigan dentro de la v1. El detalle del porqué vive en el ADR.

- **Lenguaje:** Go. Primer proyecto en Go del autor (viene de front-end / Node). Elegido por distribución (binario estático, brew/curl, multiplataforma) y por ser fuerte en procesos y concurrencia.
- **Empaquetado:** Opción A, binario instalable. La herramienta scaffoldea estado y config en el proyecto/host; no es un repo que se clona.
- **Harness:** Claude Code, único, en toda la v1. Pi diferido.
- **Backend de sesión:** herdr como primario; tmux implementable en paralelo como backend de control/comparación.
- **Inteligencia del commander:** vive en un AGENTS.md editable (markdown) que Claude Code interpreta. El binario Go hace plomería determinista (worktrees, procesos, estado, consultar al sentinel), no razona.
- **Generación de código:** vía Claude Code, aceptado. En las capas de concurrencia, no se acepta código que el autor no pueda leer y explicar.

---

## Método de construcción por capas

Cada capa es usable y testeable sola. No se pasa a la siguiente sin la anterior probada. El orden va de lo determinista-y-solo a lo concurrente-y-difícil.

1. **Capa 1 - CLI base.** `init` + `doctor`. Cero concurrencia, cero procesos.
2. **Capa 2 - Estado en disco.** Structs serializados a JSON, leídos/escritos a mano. Sin workers.
3. **Capa 3 - Un proceso.** Lanzar un solo soldier en un camp, esperar, capturar resultado. Secuencial.
4. **Capa 4 - Concurrencia.** N soldiers en paralelo, cada uno en su camp, con el sentinel consultando la API de herdr.

---

## CAPA 1 - CLI base (construible)

### Objetivo

Un binario `vexillum` que no orquesta nada todavía. Solo dos comandos: uno que prepara un proyecto para usar vexillum, y otro que reporta si el entorno está listo. Sin procesos ni concurrencia.

### Alcance

Entra: el binario, el comando `init`, el comando `doctor`, la escritura del scaffold, la lectura del entorno.

No entra: lanzar agentes, worktrees, sesiones, supervisión, estado de tareas, cualquier cosa concurrente.

### Comando `vexillum init`

Prepara el directorio actual (un proyecto del general) para ser orquestado por vexillum.

Qué hace:
- Crea el directorio de estado `~/.vexillum/` si no existe.
- Escribe en el proyecto el **AGENTS.md de producto**: el markdown que define el comportamiento del commander, que Claude Code leerá cuando (en capas futuras) se lance la orquestación. En la Capa 1 este archivo se escribe pero todavía nadie lo ejecuta.
- Crea la estructura mínima de config del proyecto (por ejemplo un `.vexillum/` local con un archivo de configuración; la forma exacta se decide al implementar).
- Es idempotente: correrlo dos veces no rompe ni duplica. Si el scaffold ya existe, informa y no sobrescribe sin aviso.

Qué NO hace en Capa 1: no lanza ningún agente, no valida que Claude Code funcione (eso es `doctor`), no crea worktrees.

### Comando `vexillum doctor`

Reporte de salud del entorno, de solo lectura. No modifica nada.

Qué chequea y reporta:
- Si el binario de Claude Code está instalado y accesible en el PATH.
- Si herdr está instalado y accesible.
- Si tmux está instalado y accesible (backend de control).
- Si `~/.vexillum/` existe y es escribible.
- Si el proyecto actual está inicializado (existe el scaffold de `init`).
- Si el directorio actual es un repo git (requisito para las capas de worktrees, aunque acá solo se reporta).

Formato: salida clara, una línea por chequeo, con estado ok/faltante. Un resumen final de si el entorno está listo o qué falta.

### Criterios de aceptación

Los criterios detallados de prueba viven en el documento de casos de prueba de la Capa 1. A alto nivel: `init` es idempotente y deja el scaffold correcto; `doctor` reporta con precisión el estado real del entorno sin modificar nada; ambos comandos fallan con mensajes claros cuando algo no está.

---

## CAPA 2 - Estado en disco (intención)

Modelar en Go la representación de una tarea (mission o scout) como structs serializables a JSON, con funciones para leer y escribir ese estado en `~/.vexillum/`. Todavía sin workers ni concurrencia: el estado se crea y se lee a mano, invocado por el autor. Es la base del restart-proof: el estado de cada tarea vive en disco para poder reconciliarse tras un reinicio.

---

## CAPA 3 - Un proceso (intención)

Lanzar UN solo soldier: spawnear una instancia de Claude Code en un camp (git worktree aislado), esperar a que termine, capturar el resultado. Un worker, secuencial, sin flota. Acá aparece el manejo de procesos en Go (`os/exec`) y la creación/teardown de un worktree. Al ser un solo proceso, un fallo se aísla con precisión.

El camp es un slot de un pool de worktrees reutilizables por proyecto (inspirado en treehouse, ver `docs/references.md`), no un worktree que se crea y se destruye por tarea: un slot se libera al pool solo cuando está limpio (sin cambios sin commitear) y "aterrizado" (sus commits ya están mergeados en la rama base); nunca se destruye el worktree en sí, se devuelve limpio para que la próxima tarea lo reutilice (dependencias y build cache intactos).

**Resuelto en Capa 4, paso 1** (esta nota quedó pendiente en su momento - ver "Decisiones tomadas en el paso 1" más abajo para el detalle): cómo entrega sus cambios una mission (equivalente al concepto de "modo de proyecto" de firstmate: `no-mistakes` / `direct-PR` / `local-only`, ver `docs/references.md`). vexillum v1 solo implementa `local-only` - `camp.Land` aterriza con un fast-forward local al estilo `fm-merge-local.sh` de firstmate, nunca abre un PR de verdad. `no-mistakes` y `direct-PR` (que sí abren un PR real en un forge) quedan fuera de alcance de v1 - no hay decisión pendiente al respecto, es una limitación de alcance consciente.

---

## CAPA 4 - Concurrencia (intención)

El corazón y el pico de dificultad. Varios soldiers en paralelo, cada uno en su camp, coordinados por el commander. El sentinel implementa la supervisión event-driven zero-token: en vez de sondear a ciegas, consulta la socket API de herdr para saber qué soldier está bloqueado o terminó, y despierta al commander solo cuando hace falta. Restart-proof completo: matar la sesión y reconciliar el estado de dominio desde disco al reiniciar (herdr restaura el layout visual pero no el proceso ni el estado de tarea; esa mitad la persiste vexillum).

Las dos formas de tarea (mission y scout) se materializan en esta capa: mission entrega cambios de código (aterrizados localmente con `vexillum land`, no un PR - ver la nota resuelta en Capa 3 y "Decisiones tomadas en el paso 1" más abajo), scout deja un reporte de investigación.

**División interna de Capa 4 en pasos verificables** (ver `docs/test-cases.md`): 1) soldier real en un pane de herdr, todavía secuencial - hecho; 2) sentinel event-driven; 3) N soldiers en paralelo; 4) restart-proof; 5) aislamiento de fallos.

**Decisiones tomadas en el paso 1** (verificadas contra el repo real de `firstmate` y contra una instancia real de herdr, no asumidas):
- **`camp` sigue manejando worktrees con git directo**, sin depender de las operaciones `worktree.*` de herdr. Confirmado con `firstmate` (`docs/herdr-backend.md`): "Herdr provides the terminal session while Treehouse continues to provide task worktrees" - la misma separación que ya tenía vexillum. herdr solo se usa para el pane/tab donde corre el soldier.
- **Sin workspace dedicado para vexillum.** El skill file de herdr pide explícitamente no crear un workspace nuevo salvo pedido explícito del usuario; los soldiers viven como tabs dentro del workspace desde el que se despacha (`$HERDR_WORKSPACE_ID`).
- Los worktrees de camps parecen heredar la confianza del repo ya confiado (no repreguntan por cada path nuevo) - observado empíricamente, no documentado por Anthropic. La lógica de reconocer y descartar el diálogo de confianza queda como red de seguridad para el primer camp de un proyecto realmente nuevo.
- **El soldier corre con `--dangerously-skip-permissions` (decisión final, revisada dos veces).** Se probó primero con permisos normales (el soldier queda `blocked` y espera aprobación), pero en uso real eso obligaba a ir a destrabar cada pane de soldier a mano - exactamente lo que un solo commander como punto de contacto debía evitar, y sin sentinel (paso 2) todavía no hay quien detecte el bloqueo sin que alguien esté mirando. El control de seguridad real es el gate de aprobación humana en `camp.Land` (ver más abajo): nada del soldier llega a la historia real del proyecto sin que el general lo apruebe explícitamente antes de aterrizarlo; el worktree aislado acota mientras tanto el daño de lo que corre sin supervisión. `blocked` sigue existiendo como estado posible (una pregunta genuina de Claude Code, no de permisos), solo que ahora es raro en vez de la norma.
- **`camp.Land` - aterrizaje estilo `local-only` de firstmate.** Encontrado investigando `bin/fm-merge-local.sh` del repo real: firstmate aterriza trabajo local con una herramienta dedicada (fast-forward-only, checkout del proyecto debe estar limpio y en la rama base, nunca fuerza ni rebasea), no con el agente corriendo `git merge` a su criterio. `camp.Land` replica esos mismos chequeos reusando helpers ya existentes de `camp` (`currentBranch`, `isDirty`, `isAncestor`). Aterrizar sigue siendo decisión del commander, con aprobación del general por defecto (salvo que el proyecto tenga "yolo" explícito) - ese es el punto de control real del sistema, no los permisos del soldier.
- **`vexillum dispatch` / `land` / `release`: CLI real, no binario de prueba.** Se probó primero con un binario descartable (`tmp-demo/soldier-demo`, ya borrado) que necesitaba que el `AGENTS.md` de cada proyecto lo referenciara a mano por su path completo. Eso se rompía cada vez que un proyecto se reseteaba, porque las instrucciones de despacho nunca vivían en el producto real (`internal/cli/init.go`), solo en la instancia de prueba - un soldier "dispatch" sin esas instrucciones hace que el commander recaiga en su propia herramienta Agent nativa (que no usa camp/herdr para nada). Se resolvió construyendo los subcomandos reales (`internal/cli/dispatch.go`, resolviendo `projectDir`/`vexillumHome` igual que `init`/`doctor`) y reescribiendo `productAgentsMD` con todo lo aprendido esta capa (no usar Agent tool, despachar en background, reportar sin plomería, distinguir mission/scout, aterrizar con aprobación) para que cualquier proyecto scaffoldeado desde ahora lo tenga de entrada.

**Decisiones del paso 2 (sentinel)**, investigadas contra `firstmate` antes de construir (su propio `AGENTS.md`, sección 8 "Supervision protocol", y `docs/herdr-backend.md`, "Push events and polling fallback"):
- **Alcance elegido explícitamente por el general: "full" (daemon + hook + cola durable)**, no la versión mínima bajo demanda ni la de solo push - a sabiendas de que es la pieza más grande y compleja de todo `firstmate` ("el corazón y el pico de dificultad" del propio PRD, confirmado leyendo `bin/fm-spawn.sh` y su `AGENTS.md` real).
- **Polling, no `events.subscribe` real.** `firstmate` mismo documenta el polling como "the permanent fallback" cuando el push no está disponible, no como una opción inferior - y `internal/herdr` shell-ea el CLI de herdr a propósito (no el socket crudo), así que depender de un stream de eventos vía CLI hubiera requerido verificar una superficie que no confirmamos que existe. `internal/sentinel.Tick` sondea cada 5s el `agent_status` real de cada tarea `running`.
- **El despertar real usa el hook `Stop` de Claude Code**, no un mecanismo propio de vexillum: `{"decision":"block","reason":"..."}` en la salida de un comando de `Stop` impide que el turno termine e inyecta ese motivo como contexto - la misma primitiva que describe `firstmate` ("blocking-capable Stop hooks block... force one bounded follow-up"). `vexillum sentinel drain` es el comando que un hook `Stop` invoca; `vexillum init` lo agrega a `.claude/settings.json` con merge cuidadoso (nunca pisa hooks existentes).
- **Cola de wakes con ack, no notificación repetida.** Cada transición de estado detectada se persiste como `Wake` (`~/.vexillum/wakes/<id>.json`) hasta que `drain` la entrega una vez y la marca ack - mismo principio que la cola durable de `firstmate`, simplificado (sin las generaciones de spawn ni el lock de control por tarea, que son específicos de su arquitectura multi-actor).
- **Un sentinel por `~/.vexillum/`, con lock de instancia única** (`sentinel.AcquireLock`, PID file con chequeo de vida del proceso) - matches "never broadly kill watchers... race-proof singleton lock" de `firstmate`. Como `Tick` recorre todas las tareas bajo un `vexillumHome`, un solo sentinel cubre todos los proyectos que comparten ese home, no uno por proyecto.
- **Bug real encontrado y corregido en el camino**: `soldier.MapAgentStatus` nunca contemplaba `"working"` - tenía sentido en su contexto original (el resultado ya asentado de `agent prompt --wait` nunca es `working`), pero el sentinel sondea el estado en vivo, donde `working` es el caso normal mientras el soldier sigue corriendo. Sin el fix, cada tick marcaba de forma incorrecta como `failed` a cualquier soldier todavía trabajando.
- **No implementado**: el arranque automático del sentinel desde `dispatch` (spawnear el daemon en background la primera vez que hace falta). Por ahora el commander lo arranca a mano una vez por proyecto, documentado en `productAgentsMD` - mismo criterio de "empezar simple" que ya se usó para el resto de esta capa; se automatiza si resulta ser una fricción real repetida.

---

## Features diferidas (post-v1)

No entran en la v1. Se reabren cuando exista un commander único sólido (fin de Capa 4).

- **Lieutenant (orquestador secundario persistente), local y remoto por SSH.** Diferido por dependencia (necesita el commander único terminado; es "un segundo commander") y por dificultad (coordinación entre dos commanders y estado compartido/reconciliado, que Go no simplifica; es concurrencia sobre concurrencia). Go sí facilita el transporte remoto (binario estático que viaja por scp y corre sin dependencias), pero ese es el problema chico. Reabrir después de Capa 4.
- **Pi como segundo harness.** Requiere una interfaz `Harness` que abstraiga sobre los mecanismos de supervisión distintos (Stop hook en Claude, watcher extension en Pi). En la v1, con Claude único, la supervisión es un solo camino; la costura se deja preparada pero no se construye la abstracción.
- **tmux como backend primario.** En la v1 herdr es primario y tmux queda como backend de control. Promover tmux a opción de primer nivel es post-v1.
- **Chequeo de nombre de paquete** en el índice de distribución que se use, si se abre al público (más allá de brew + release por curl).
