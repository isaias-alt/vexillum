# 2026-09-21 - v2 B.4: no-mistakes (docs/no-mistakes.md + vexillum ship) - cierra la v2

## Resuelto y en pie

- **Investigación real antes de escribir nada**: el PRD exigía cerrar `docs/no-mistakes.md` y la decisión abierta ("quién dispara el PR") antes de implementar. Se investigó `no-mistakes` vía GitHub API (`gh api search/code`, no solo WebFetch) porque la búsqueda inicial de `docs/no-mistakes.md` dentro de firstmate dio 404 - señal de que la premisa del PRD ("firstmate lo configura, `.no-mistakes.yaml`") apuntaba a otra cosa. Encontrado: **`no-mistakes` es su propio repo standalone** (`kunchenguid/no-mistakes`), no una feature interna de firstmate. firstmate solo lo *consume* (su `.no-mistakes.yaml` en la raíz es config de cliente, no la fuente del gate).

- **Dos correcciones reales al PRD, documentadas en `docs/no-mistakes.md` y en el propio `docs/prd-v2.md`**:
  1. No es un Agent Skill (`npx skills add`) como los otros tres AXIs de la Parte B - es un binario real instalado por `curl` (mismo modelo de distribución que vexillum mismo).
  2. **No necesita gh-axi para abrir el PR.** Pone un git remote local (`no-mistakes`) delante del real; `git push no-mistakes <branch>` corre su propio pipeline (review/test/docs/lint) en un worktree aislado y, si todo pasa, reenvía al remote real y abre el PR con su propia integración de GitHub. gh-axi queda fuera del alcance mínimo de B.4 - corregido en `docs/references.md` (la entrada de gh-axi decía explícitamente que era "el mecanismo con el que no-mistakes abre PRs").

- **Decisión abierta del PRD, cerrada**: quién dispara el PR - **el binario**, determinista, vía `vexillum ship <task-id>`. La investigación dio el mecanismo concreto que el PRD pedía sin tenerlo todavía: `git push no-mistakes <branch>` es un comando git plano, sin juicio de agente de por medio, exactamente el tipo de acción que "no se deja al criterio del LLM" por ser irreversible.

- **`docs/no-mistakes.md`** (nuevo): documento de diseño completo - corrección, mecánica real de la herramienta, decisión cerrada con su razón, alcance de lo que entra/no entra, criterio de terminado. Escrito antes de tocar código, como pedía el PRD.

- **`internal/cli/doctor.go`**: nuevo `checkNoMistakes(projectDir)` - reporta `[missing]` si el binario no está en PATH, `[missing]` con mensaje distinto si está pero el proyecto no corrió `no-mistakes init` (sin el remote git `no-mistakes`), `[ok]` si ambas cosas están. Nueva `noMistakesGateConfigured(projectDir)` (chequea `git remote get-url no-mistakes`) - reusada después por `ship.go`. Ninguno de los dos estados afecta el exit code (opcional, igual que los AXIs).

- **Comando nuevo `vexillum ship <task-id>`** (`internal/cli/ship.go`): refuse si la tarea no es `KindMission` (un scout no tiene nada que shippear, mismo criterio que ya existe para `land`), refuse si no está `StatusDone`, refuse si el proyecto no está gateado (mensaje que menciona `no-mistakes init` y ofrece `vexillum land` como alternativa). Si todo está bien: `git push no-mistakes <camp-branch>` desde el camp - determinista, sin decidir nada por su cuenta. No toca el ciclo de vida del camp (no libera, no cambia `Task.Status`) - el PR queda "en vuelo", aterrizarlo es responsabilidad del general en GitHub, igual que hoy.

- **`productAgentsMD`**: sección nueva "Shipping through the no-mistakes gate (alternative to landing)" - el commander lo ofrece cuando el general quiere un PR real validado en vez de un fast-forward local, pregunta antes de correrlo (mismo criterio que `land`/`redispatch`), y no libera el camp después (el PR sigue en vuelo).

- **8 tests nuevos**: 3 en `doctor_test.go` (no instalado / instalado-no-gateado / gateado, con un helper `addGitRemote` para simular `no-mistakes init` sin el binario real), 4 en `ship_test.go` (éxito contra un bare repo local real, refuse-no-done, refuse-scout, refuse-no-gateado). Build, vet, gofmt y toda la suite con `-race` en verde.

- **`docs/prd-v2.md`** actualizado de punta a punta: B.4 marcada `[RESUELTA]`, "Parte B [RESUELTA]" (ya no `[PENDIENTE]`), "Estado al día de hoy" dice que toda la v2 está resuelta, "Secuencia recomendada" reescrita en pasado confirmando que se siguió al pie de la letra.

## Estado de la v2 al cierre de esta sesión

**Toda la v2 (Parte A + Parte B) está implementada y verificada por tests.** Lo único que falta es la tanda de verificación en vivo, diferida a propósito (decisión del general, esta sesión): A.2 (redispatch real contra un pane de herdr), B.1/B.2/B.4 (instalar los binarios/skills reales y correr `doctor` contra ellos, y `vexillum ship` contra un `no-mistakes` real con GitHub real), B.3 (un browser real abierto y cerrado por el ciclo de redispatch).

## Pendiente para la próxima

- La tanda de pruebas en vivo completa (ver arriba) - es lo único que queda de la v2 entera.
- Nota de proceso de toda esta sesión (A.2 en adelante): se trabajó sin `EnterPlanMode`/`ExitPlanMode` desde la mitad de B.3 en adelante, a pedido explícito del general ("cenare, no podre aceptar los planes") - guardado en memoria (`feedback_skip_plan_mode_unattended`). Retomar el flujo de planificación normal en la próxima sesión salvo que el general indique lo contrario.
