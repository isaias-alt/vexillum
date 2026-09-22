# 2026-09-21 - v2 B.4 (ajuste post-cierre): `ship` auto-gatea en el primer uso

## Resuelto y en pie

- **Cambio de diseño acordado con el general**, después de que B.4 ya estaba cerrado e implementado. Pedido: que el gate de `review-tool` "venga por defecto" pero "no se use a no ser que se llame [ship] en el prompt" - separar el *setup* (que exista el remote `review-tool`) del *uso* (correr el pipeline). La primera implementación de `vexillum ship` refusaba directamente si el proyecto no estaba gateado, exigiendo que el general corriera `review-tool init` a mano primero.

- **Se evaluaron dos lugares para el auto-setup, se descartó uno con razones concretas**:
  - `vexillum init`: descartado. Rompería el principio ya establecido de que `init` es el comando más básico de vexillum, puramente local, sin red (mismo argumento que ya se usó para que los AXIs no se instalen desde `init`). Además asumiría que el proyecto ya tiene un remote real de GitHub al momento de inicializar, no siempre cierto.
  - `vexillum ship`, lazy, en el primer uso (elegido): la primera vez que `ship` encuentra un proyecto no gateado, si `review-tool` está instalado, le corre `init` ahí mismo antes de pushear. Si no está instalado, falla con mensaje claro en vez de un error de `exec` confuso.

- **`internal/cli/ship.go`**: nueva `gateProject(projectDir, stdout, stderr)` - chequea `exec.LookPath("review-tool")` primero (falla claro si no está), corre `review-tool init` con `cmd.Dir = projectDir`, reporta éxito/fallo. `runShip` la llama solo cuando `review-toolGateConfigured` da falso, en vez de refusar directamente.

- **`internal/cli/doctor.go`**: mensaje de "no gateado" actualizado para reflejar que ya no hace falta que el general lo resuelva a mano ("'vexillum ship' will do this automatically the first time").

- **Tests actualizados/nuevos** (`ship_test.go`): la vieja `TestRunShip_RefusesUngatedProject` se dividió en `TestRunShip_RefusesWhenreview-toolNotInstalled` (sigue refusando si el binario no está) y dos casos nuevos - `TestRunShip_AutoGatesOnFirstShip` (un stub de `review-tool` cuyo `init` agrega el remote de verdad contra un bare repo local, confirma que el push subsiguiente sale bien) y `TestRunShip_AutoGateFailureIsReported` (el `init` del stub falla, `ship` reporta el fallo y nunca intenta el push). Nuevos helpers `gitOnlyPath`/`review-toolStub` para simular el binario sin depender de que esté realmente instalado en la máquina que corre los tests.

- **`docs/review-tool.md`**, **`docs/prd-v2.md`**, **`docs/test-cases.md`** y `productAgentsMD` actualizados con la decisión y su razonamiento.

- Build, vet, gofmt y toda la suite con `-race` en verde. Binario reinstalado en `/opt/homebrew/bin/vexillum` (build local, versión `dev`) - verificado en vivo contra el proyecto real de pruebas (`~/github/isaias-alt/tmp/vexillum-prueba`): `vexillum doctor` corrió ahí y mostró todos los checks nuevos de la sesión (herdr version, los tres AXIs, review-tool) funcionando correctamente.

## Nota operativa

Se confirmó en esta sesión que no había ningún binario `vexillum` instalado en la máquina (se había desinstalado al cerrar la prueba de distribución brew/curl de la sesión anterior) - el general lo reinstaló con `go build -o /opt/homebrew/bin/vexillum ./cmd/vexillum` para poder probar en vivo. Cualquier cambio de código futuro necesita este mismo rebuild para reflejarse en `vexillum-prueba`.

## Pendiente para la próxima

- La tanda de pruebas en vivo completa sigue pendiente (A.2, B.1/B.2/B.3/B.4 con las herramientas reales), ahora con un binario real instalado para poder correrla.
