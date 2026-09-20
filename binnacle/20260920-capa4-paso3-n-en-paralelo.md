# 2026-09-20 - Capa 4 paso 3: N soldiers en paralelo, y un bug real de carrera en el pool de camps

## Resuelto y en pie

- **Limpieza previa: camp huérfano de la prueba original resuelto.** El commit real (`e1c42d6`, `NOTES.md`) quedó descartado a pedido del general (ya desactualizado - decía "no code files exist" cuando para ese momento ya se había aterrizado el módulo BST). Los dos worktrees huérfanos bajo `vexillum-prueba-b728ba83` se sacaron con `git worktree remove` (no a mano, para no corromper la metadata compartida del repo real), las ramas `vexillum/c4dda7bff73bae62` y `vexillum/d4159c3f64705e57` se borraron, y el pool + los archivos de tarea/wake correspondientes se limpiaron. `git worktree list`/`git status` en el proyecto real confirmados sanos después.

- **Bug real de carrera encontrado antes de escribir ningún test formal**: al revisar `internal/camp/camp.go` para diseñar las pruebas de "N en paralelo", se notó que `Acquire` (y `Release`) hacen un read-modify-write sobre `pool.json` (leer, calcular el slot, escribir) sin ninguna exclusión mutua. Se escribió un test con goroutines (`TestAcquire_ConcurrentCallsNeverCollideOnASlot`, 8 llamadas concurrentes) **antes** de arreglar nada, para confirmarlo - **reprodujo el bug 100% de las veces en 5 corridas seguidas**: `git worktree add` fallando con "already exists" o "index.lock: File exists" - dos `Acquire` concurrentes leyendo el mismo estado, calculando el mismo número de slot, y pisándose.

- **Arreglo**: nueva `lockPool(poolRoot)` en `internal/camp/camp.go` - un lock exclusivo por `flock` (no un pid-file como el del sentinel, que es para un lock de instancia única de todo un proceso; acá hace falta exclusión mutua de una sección corta, y `flock` se libera solo si el proceso muere a mitad de camino, sin dejar un lock huérfano). Envuelve la sección crítica completa de `Acquire` (desde `loadPool` hasta el `git worktree add`/`savePool`) y de `Release` (mismo problema, aunque con una consecuencia menos grave - una actualización perdida en vez de una colisión de `git worktree`). Con el lock puesto: **10/10 corridas en verde con `-race`**, mismo test.

- **Segundo test de concurrencia**: `TestRelease_ConcurrentCallsDoNotLoseUpdates` - 8 camps liberados al mismo tiempo, confirma que `pool.json` termina con los 8 slots correctamente desarrendados (ninguna escritura pisa a otra).

- **Verificado en vivo, no solo con goroutines de test**: 3 procesos `vexillum dispatch` reales, genuinamente concurrentes (lanzados con `&` desde bash, no secuenciales), contra el mismo proyecto scratch. Los tres terminaron limpio: slots 1/2/3 distintos, camps distintos, ramas distintas, cada uno con su propio commit real. `pool.json` final revisado a mano - exactamente 3 slots, cada uno arrendado por la tarea correcta.

- **`docs/test-cases.md`**: L4-27 (Acquire concurrente) y L4-28 (Release concurrente) agregados con el detalle del bug y el fix; la sección "Pendiente" de Capa 4 actualizada - paso 3 ahora formalizado y verificado, paso 4 (restart-proof) ya estaba mayormente cubierto de la sesión anterior, queda solo paso 5 (aislamiento de fallos con soldiers reales, uno fallando a propósito) como pendiente de alto nivel.

- 2 tests nuevos en `internal/camp`. Build, vet, gofmt y toda la suite en verde, incluido `-race` en todo el repo.

## Pendiente para la próxima

- **Paso 5 (aislamiento de fallos) sigue sin un caso formal con soldiers reales** - hoy está cubierto estructuralmente (un error transitorio en una tarea no tumba el resto del tick del sentinel, L4-25) pero falta un escenario con múltiples soldiers reales corriendo a la vez, uno de ellos fallando a propósito, confirmando que el commander y los demás soldiers siguen sin verse afectados.
- **`camp.Land` no tiene el mismo tipo de protección** - opera sobre el checkout del propio proyecto (`git merge --ff-only`), no sobre `pool.json`, así que dos `vexillum land` corridos genuinamente al mismo tiempo contra el mismo proyecto podrían chocar a nivel git. Se dejó fuera de alcance esta ronda a propósito: aterrizar es una acción gateada por aprobación humana explícita (el general aprueba una mission a la vez, según `AGENTS.md`), así que la probabilidad de que ocurra de verdad es mucho más baja que con `Acquire`/`Release` (que SIEMPRE se disparan en paralelo cuando se despachan N soldiers a la vez). Queda anotado por si alguna vez se vuelve un problema real.
- Repo con cambios sin commitear de esta sesión - falta que el general revise el mensaje de commit.
