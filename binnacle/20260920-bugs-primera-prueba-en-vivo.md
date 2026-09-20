# 2026-09-20 - Dos bugs reales encontrados en la primera prueba en vivo del rediseño

## Resuelto y en pie

La primera prueba en vivo del rediseño async (`20260920-sentinel-rediseno-async.md`)
funcionó en el mecanismo central - `vexillum dispatch` volvió rápido, el
sentinel se auto-arrancó, y el hook `Stop` disparó de verdad con una
transición real (algo que nunca había pasado en toda la sesión: la
carpeta `~/.vexillum/wakes/` había estado vacía siempre hasta ahora). Pero
la prueba también encontró dos bugs reales, los dos confirmados con
evidencia del filesystem real, no solo de la transcripción del commander.

- **Bug 1: "done" prematuro por una carrera que no había contemplado del
  todo.** La primera mission despachada (`38ce9089911883ee`) quedó
  marcada `done` a los 5 segundos de creada. Se verificó en el camp real:
  sin commits nuevos, checkout limpio, `NOTES.md` no existía, y el
  `output` guardado era solo el eco del prompt más un spinner genérico
  ("✳ Pollinating…") - el soldier nunca llegó a hacer nada. La wake la
  registró el sentinel (confirmado por `sentinel.log`: "recorded 1
  wake(s)"), no `dispatch`. Causa: el sentinel se auto-arranca casi al
  mismo tiempo que `dispatch` somete el prompt, y en esa ventana breve
  herdr puede seguir reportando `idle` (el agente recién creado, sin
  haber arrancado a procesar el prompt todavía) antes de pasar a
  `working`. `RunInHerdr`'s propio `AgentPrompt --wait` ya está protegido
  contra esto (herdr exige ver `working`/`blocked` antes de aceptar un
  `idle` posterior como asentamiento real, según su propia ayuda), pero
  el `client.AgentStatus` crudo que usa `sentinel.Tick` no tiene esa
  protección - ganó la carrera y escribió el `done` falso antes de que
  `RunInHerdr` pudiera concluir su propio probe protegido.

  **Arreglo**: `sentinel.Tick` ahora ignora cualquier tarea `Running`
  cuyo `UpdatedAt` sea más reciente que `settleGracePeriod` (8s, margen
  sobre los 5000ms que documenta la ayuda de `herdr agent prompt`).
  `RunInHerdr` ahora también persiste `UpdatedAt` justo antes de llamar a
  `AgentPrompt` (no solo al marcar `Running` antes de `startAgent`, que
  puede demorar hasta `trustDialogSettleWindow` si hay diálogo de
  confianza) - así el período de gracia del sentinel se ancla al momento
  real de envío del prompt, no a un timestamp que podría quedar
  desactualizado si el arranque del agente tardó. Test nuevo:
  `TestTick_SkipsTasksWithinSettleGracePeriod` (`internal/sentinel`).

- **Bug 2: despachar desde adentro de un camp crea un pool fantasma.**
  La segunda mission (`d4159c3f64705e57`) quedó con
  `camp_path` bajo una raíz de proyecto (`vexillum-prueba-b728ba83`)
  distinta a la de la primera (`vexillum-prueba-2380e857`). Se confirmó
  calculando el hash directamente: `sha256(ruta absoluta)[:8]` de
  `~/.vexillum/vexillum-prueba-2380e857/2/vexillum-prueba` (el camp de
  la primera tarea) da exactamente `b728ba83`. El commander había
  hecho `cd` a ese camp para inspeccionarlo (leer su `git log`/`git
  status`) y, al no volver a la raíz real del proyecto, el segundo
  `vexillum dispatch` calculó un pool de camps completamente distinto y
  huérfano - invisible para cualquier comando futuro corrido
  correctamente desde la raíz real, y un `vexillum land`/`release` para
  ese mismo número de slot corrido desde la raíz real habría resuelto
  el camp EQUIVOCADO (el de la primera tarea).

  **Arreglo**: nueva `refuseInsideVexillumHome(projectDir, vexillumHome)`
  en `internal/cli/dispatch.go` - refuso si `projectDir` está dentro de
  `vexillumHome`. Wireado en `resolveDirs()` (cubre dispatch/land/
  release/sentinel de una sola vez) y directamente en `runInit` y
  `runUpgrade` (que reciben `projectDir`/`vexillumHome` como parámetros,
  no pasan por `resolveDirs`). Tests nuevos:
  `TestRefuseInsideVexillumHome`, `TestInit_RefusesInsideVexillumHome`,
  `TestUpgrade_RefusesInsideVexillumHome`.

- Build, vet, gofmt y toda la suite en verde después de los dos
  arreglos (algunos tests existentes necesitaron backdatear
  `task.UpdatedAt` para no toparse ellos mismos con el nuevo período de
  gracia del sentinel - no era el comportamiento que estaban probando).

## Pendiente para la próxima

- **Verificación en vivo de estos dos arreglos, todavía no hecha** -
  repetir la misma prueba (mission real, commander corriendo por su
  cuenta) y confirmar: (a) que ninguna tarea real quede marcada `done`
  antes de que el soldier haya hecho algo de verdad, y (b) que si el
  commander inspecciona un camp con `cd` y se olvida de volver, el
  siguiente `vexillum dispatch`/`land`/`release` falle con un mensaje
  claro en vez de crear un pool fantasma.
- El camp huérfano bajo `vexillum-prueba-b728ba83` (y su tarea
  `d4159c3f64705e57`, que sí hizo trabajo real y comiteó `e1c42d6` en
  ESE camp) queda sin aterrizar ni liberar - no hay ningún comando que
  hoy pueda resolverlo desde la raíz real del proyecto (necesitaría
  correrse desde adentro de ese camp mismo, o una limpieza manual). No
  es urgente resolverlo ahora, pero hay que tenerlo presente si se
  audita `~/.vexillum/` de `vexillum-prueba` más adelante.
- El período de gracia de 8s es una cota empírica (margen sobre el 5s
  que documenta herdr), no una garantía matemática - queda anotado como
  el criterio que se usó, por si hace falta revisarlo si se repite el
  bug con otro margen.
- Repo con cambios sin commitear de esta sesión (los dos arreglos) -
  falta que el general revise el mensaje de commit.
