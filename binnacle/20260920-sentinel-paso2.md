# 2026-09-20 - Capa 4, paso 2: el sentinel

## Resuelto y en pie

- **Alcance "full" elegido explícitamente por el general**, a sabiendas de
  que era la pieza más grande y compleja de `firstmate`. Se investigó su
  implementación real antes de construir nada: `AGENTS.md` sección 8
  ("Supervision protocol") y `docs/herdr-backend.md` ("Push events and
  polling fallback"). También se cargó la skill `update-config` para
  confirmar la mecánica exacta del hook `Stop` de Claude Code (`{"decision":
  "block","reason":"..."}` impide que el turno termine e inyecta el motivo
  como contexto) antes de diseñar nada.

- **`internal/sentinel`** (nuevo paquete): `Tick` sondea cada tarea
  `running` contra su `agent_status` real en herdr (`herdr agent get`),
  persiste transiciones, y registra una `Wake` durable con ack
  (`~/.vexillum/wakes/<id>.json`). `Drain` entrega las wakes sin ack una
  sola vez (las marca ack al leerlas) - no repite el mismo aviso en cada
  llamada. `Run` es el loop infinito (5s de intervalo). `AcquireLock` es
  un lock de instancia única por `vexillumHome` (PID file con chequeo de
  vida del proceso vía `syscall.Signal(0)`) - reclama un lock huérfano de
  un proceso muerto en vez de quedar bloqueado para siempre.

- **Decisión consciente: polling, no `events.subscribe` real.** `firstmate`
  mismo documenta el polling como "the permanent fallback", no como
  segunda categoría - y como `internal/herdr` shell-ea el CLI de herdr a
  propósito (nunca el socket crudo, ver su doc de paquete), depender de un
  stream de eventos hubiera requerido verificar una superficie que nunca
  confirmamos que el CLI expone. Es una simplificación real y documentada,
  no una limitación oculta.

- **`vexillum sentinel`** (foreground, el commander lo backgroundea) y
  **`vexillum sentinel drain`** (lo que un hook `Stop` invoca - imprime
  `{}` si no hay nada pendiente, o `{"decision":"block","reason":"..."}`
  nombrando la tarea y la transición si hay algo).

- **`vexillum init` agrega el hook `Stop` a `.claude/settings.json`** con
  merge cuidadoso: nunca pisa hooks o settings existentes, nunca duplica
  el hook en corridas repetidas, y si el archivo existente tiene JSON
  inválido lo deja intacto y reporta el error en vez de arriesgarse a
  corromperlo (mismo criterio que pide la skill `update-config`). Se
  validó en vivo con `jq -e` sobre un proyecto scratch real, siguiendo el
  protocolo de verificación de la propia skill.

- **Bug real encontrado en el camino**: `soldier.MapAgentStatus`
  (compartida entre `soldier` y `sentinel`, exportada esta sesión) no
  contemplaba `"working"` - tenía sentido en su contexto original (el
  resultado ya asentado de `agent prompt --wait` nunca devuelve
  `working`), pero el sentinel sondea el estado en vivo, donde `working`
  es exactamente lo normal mientras el soldier sigue corriendo. Sin el
  fix, cada tick marcaba `failed` a cualquier soldier todavía trabajando -
  lo atrapó el primer test que se corrió, antes de tocar nada más.

- **`productAgentsMD` actualizada**: nueva sección "The sentinel"
  explicando al commander que lo arranque una vez por proyecto (si no
  está corriendo ya) y qué significa que su turno quede bloqueado por el
  hook. La entrada de vocabulario de "sentinel" (que decía "not built yet")
  también se actualizó.

- **18 tests nuevos** (`internal/sentinel`: 8, `internal/cli`: 5 de
  sentinel/hook + los ya existentes). Casos concretos `L4-12` a `L4-18` en
  `docs/test-cases.md`. Build, vet, gofmt y **66 tests en verde** en todo
  el repo.

## Pendiente para la próxima

- **El sentinel no arranca solo** - `dispatch` no lo lanza automáticamente
  en background la primera vez que hace falta. Queda como paso manual
  documentado en `AGENTS.md` ("empezar simple", mismo criterio que el
  resto de esta capa) - se automatiza si resulta ser fricción real.
- **Sin verificación end-to-end en vivo todavía** del ciclo completo
  sentinel corriendo + hook disparando de verdad durante una sesión real
  del commander - se validó cada pieza por separado (tests unitarios +
  `jq` sobre el settings.json real), pero no el journey completo con un
  soldier real bloqueado o terminado mientras el sentinel está corriendo.
- Quedan los pasos 3 (N en paralelo formalizado), 4 (restart-proof) y 5
  (aislamiento de fallos) de Capa 4.
- Repo sin commitear esta sesión (sentinel completo).
