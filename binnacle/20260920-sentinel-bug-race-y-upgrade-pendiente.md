# 2026-09-20 - Sentinel: bug real de carrera encontrado en la primera prueba en vivo

## Resuelto y en pie

- **Primera prueba real del sentinel + hook `Stop`, hecha 100% a través del
  commander** (el general pidió `vexillum init` + arrancar el sentinel +
  despachar una mission en background, todo por prompt, nunca comandos
  sueltos). El hook nunca disparó. Se verificó con evidencia, no se asumió:
  `~/.vexillum/wakes/` **ni existía** - cero wakes registradas jamás.

- **Causa raíz real, no un bug menor de implementación**: `vexillum
  dispatch` (vía `soldier.RunInHerdr`) maneja todo el ciclo de vida de la
  tarea *dentro de su propio proceso*, de forma síncrona: escribe
  `running`, espera con `agent prompt --wait`, escribe el estado final
  (`done`/`blocked`) - todo antes de que el comando `dispatch` termine. El
  sentinel corre en un **proceso aparte**, sondeando cada 5s. Para cuando
  el sentinel mira de nuevo, `dispatch` ya escribió el estado final él
  mismo. `sentinel.Tick` solo actúa sobre tareas con `Status == Running`
  (`if task.Status != state.StatusRunning { continue }`) - si para cuando
  sondea la tarea ya está `done` (escrita por `dispatch`, no por el
  sentinel), la salta en silencio. **El sentinel, tal como está diseñado
  hoy, no puede disparar nunca para el camino feliz de `dispatch`** - solo
  tendría sentido para una tarea que quedó `running` huérfana (ej.
  `vexillum` murió a mitad de camino) - que es terreno de restart-proof
  (paso 4 de Capa 4), no del paso 2 tal como se planteó.
  **Sin arreglar todavía** - el general pidió priorizar el comando
  `upgrade` (ver abajo) antes de tocar esto.

- **Bug secundario, real pero menor**: `vexillum sentinel <arg-no-reconocido>`
  (el commander probó `vexillum sentinel status`, que no existe como
  subcomando) cae al comportamiento por defecto de `internal/cli/sentinel.go:Sentinel`
  y arranca el loop infinito en vez de fallar con un error de uso. Quedó
  un proceso sentinel corriendo *por accidente* (pid 12336, arrancado
  ~12:27PM), no porque alguien lo haya arrancado a propósito. Hay que
  matarlo a mano y arreglar el parseo de argumentos (cualquier arg que no
  sea `drain`/`-h`/`--help` debería ser un error, no un fallback silencioso
  a "arrancar el loop").

- **Confirmado, no es un bug**: el pane no se cerró - es el comportamiento
  esperado, ya que nadie llamó `land`/`release` (se le pidió al commander
  explícitamente que no actuara más después de despachar).

## Pendiente para la próxima (en este orden, según pidió el general)

1. **Implementar `vexillum upgrade`**: pedido explícito del general antes
   de arreglar nada más - un comando que actualice un proyecto ya
   inicializado a lo último (plantilla de `AGENTS.md`/`CLAUDE.md` al día,
   hook del sentinel agregado/actualizado, cualquier sanado que hoy solo
   pasa como efecto secundario de volver a correr `vexillum init`), sin
   tener que borrar `.git`/`.vexillum` y reinicializar todo de cero como
   se vino haciendo toda la sesión. Diseño a definir: ¿reusa la misma
   lógica de sanado que ya tiene `runInit` (healing path), expuesta bajo
   un nombre más claro? ¿O es un comando nuevo con su propia lógica de
   "qué versión tiene esto vs. qué versión trae el binario"? Falta pensar
   si hace falta versionar el scaffold (`.vexillum/config.json` ya tiene
   un campo `Version`, hoy sin uso real) para saber qué actualizar sin
   volver a escribir todo cada vez.
2. **Arreglar el parseo de argumentos de `vexillum sentinel`** (falla
   silenciosa descrita arriba) - matar el proceso accidental (pid 12336)
   como parte del arreglo, y confirmar que no dejó una wake mal armada.
3. **Repensar el diseño del sentinel dado el bug de carrera.** Opciones a
   evaluar (sin decidir todavía): (a) que `dispatch` NO escriba el estado
   final él mismo cuando hay un sentinel corriendo, dejando que el
   sentinel sea la única fuente de verdad para la transición Running→final
   (cambio de diseño real); (b) que el sentinel sirva específicamente para
   el caso de restart-proof/tareas huérfanas (redefinir su alcance, no
   como reemplazo del reporte que ya da `dispatch` en foreground/background
   vía la propia notificación de Claude Code); (c) alguna otra forma de
   que el sentinel is enterese de transiciones sin depender de ganarle la
   carrera a `dispatch`. Necesita una sesión de diseño propia, no un
   parche rápido.
- Sigue pendiente también todo lo que ya estaba anotado en
  `20260920-sentinel-paso2.md` (pasos 3-5 de Capa 4, arranque automático
  del sentinel).
- Repo con cambios sin commitear de esta sesión (ninguno todavía - esta
  sesión fue puramente de investigación/prueba, sin tocar código).
