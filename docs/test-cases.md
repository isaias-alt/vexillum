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

## CAPA 3 - Un proceso (criterios de aceptación de alto nivel)

- Se crea un camp (git worktree aislado) para una tarea, con su rama propia, sin ensuciar el working tree del proyecto.
- Se lanza una instancia de Claude Code apuntada a ese camp; vexillum captura su salida y su exit code.
- Cuando el proceso termina, su resultado y su estado final quedan persistidos (usando la Capa 2).
- Al terminar (éxito o fallo), el camp se limpia según política definida (se borra, o se conserva para inspección); la política es explícita y consistente.
- Un fallo del proceso lanzado (crash, exit distinto de 0) se detecta y se refleja en el estado de la tarea, sin dejar el camp colgado.
- Matar vexillum mientras el proceso corre no deja estado mentiroso: al volver, el estado refleja que la tarea quedó interrumpida.

## CAPA 4 - Concurrencia (criterios de aceptación de alto nivel)

- N soldiers corren en paralelo, cada uno en su propio camp, sin pisarse entre ellos ni corromper estado compartido.
- El sentinel detecta, vía la socket API de herdr, qué soldier está bloqueado o terminó, sin sondeo activo que gaste tokens.
- El sentinel despierta al commander solo cuando hay algo que atender (un soldier bloqueado o terminado), no en cada ciclo.
- Restart-proof: matar la sesión entera y volver a levantar vexillum reconstruye el estado de todas las tareas desde disco; los soldiers que se puedan resumir se resumen, los que no, quedan marcados como interrumpidos. herdr restaura el layout visual; el estado de dominio lo restaura vexillum.
- Una mission termina entregando cambios de código (un PR); un scout termina dejando un reporte de investigación; ambos resultados quedan persistidos y asociados a su tarea.
- Un soldier que falla no tumba a los demás ni al commander; su fallo queda aislado y reflejado en su estado.
- Dos soldiers nunca comparten el mismo camp ni la misma rama
