# Casos de prueba - Vexillum

Documento vivo. La Capa 1 está a nivel de caso concreto (input, acción, resultado esperado): se prueba tal cual. Las Capas 2 a 4 están como criterios de aceptación de alto nivel: qué tiene que ser cierto para dar la capa por terminada. Cuando llegues a construir cada una de esas capas, bajás esos criterios a casos concretos con el mismo formato que la Capa 1.

Convención: cada caso tiene un id (`L1-01`), una precondición, una acción y un resultado esperado. Un caso pasa solo si el resultado esperado se cumple exacto.

---

## CAPA 1 - CLI base (casos concretos)

### `vexillum init`

**L1-01 - init en proyecto limpio**
Precondición: directorio que es un repo git, sin scaffold previo de vexillum, `~/.vexillum/` no existe.
Acción: correr `vexillum init`.
Esperado: se crea `~/.vexillum/`; se crea el scaffold local del proyecto (config local + AGENTS.md de producto); salida confirma qué se creó; exit code 0.

**L1-02 - init es idempotente**
Precondición: un proyecto donde ya se corrió `init` con éxito.
Acción: correr `vexillum init` de nuevo.
Esperado: no se duplica ni se corrompe nada; la salida informa que ya estaba inicializado; no se sobrescribe ningún archivo sin aviso; exit code 0.

**L1-03 - init no sobrescribe cambios del usuario**
Precondición: un proyecto inicializado donde el usuario editó a mano el AGENTS.md de producto.
Acción: correr `vexillum init` de nuevo.
Esperado: el AGENTS.md editado NO se pisa silenciosamente; si init quisiera regenerarlo, avisa y pide confirmación o lo deja intacto; exit code 0.

**L1-04 - init crea `~/.vexillum/` si falta pero el proyecto ya estaba**
Precondición: proyecto con scaffold local presente, pero `~/.vexillum/` borrado a mano.
Acción: correr `vexillum init`.
Esperado: recrea `~/.vexillum/` sin tocar el scaffold local existente; exit code 0.

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

## CAPA 4 - Concurrencia (criterios de aceptación de alto nivel)

- N soldiers corren en paralelo, cada uno en su propio camp, sin pisarse entre ellos ni corromper estado compartido.
- El sentinel detecta, vía la socket API de herdr, qué soldier está bloqueado o terminó, sin sondeo activo que gaste tokens.
- El sentinel despierta al commander solo cuando hay algo que atender (un soldier bloqueado o terminado), no en cada ciclo.
- Restart-proof: matar la sesión entera y volver a levantar vexillum reconstruye el estado de todas las tareas desde disco; los soldiers que se puedan resumir se resumen, los que no, quedan marcados como interrumpidos. herdr restaura el layout visual; el estado de dominio lo restaura vexillum.
- Una mission termina entregando cambios de código (un PR); un scout termina dejando un reporte de investigación; ambos resultados quedan persistidos y asociados a su tarea.
- Un soldier que falla no tumba a los demás ni al commander; su fallo queda aislado y reflejado en su estado.
- Dos soldiers nunca comparten el mismo camp ni la misma rama
