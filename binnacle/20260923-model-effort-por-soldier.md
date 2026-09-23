# 2026-09-23 - Model/effort por soldier

## Resuelto y en pie

- **Origen**: se investigó `firstmate` (kunchenguid) - orquestador de flota
  similar a vexillum, con `config/crew-dispatch.json` para rutear a
  distintos harnesses/modelos por reglas en lenguaje natural. El general
  trajo un borrador propio (solo model/effort, sin campo `harness` - v1 de
  vexillum tiene un único harness por ADR-04, así que no aplica) y pidió
  implementarlo.

- **Tres decisiones de diseño, resueltas antes de tocar código** (pedidas
  explícitamente por el general):
  1. **Orden de evaluación**: primera regla que matchea gana, declarado
     como lista numerada explícita en el markdown - no queda implícito.
     Desempate para el caso "rename que toca 40 archivos": el criterio es
     uniformidad de la transformación, no cantidad de archivos - si las 40
     ediciones son la misma sustitución mecánica repetida, sigue siendo la
     regla mecánica; si alguna necesita juicio propio, no lo es.
  2. **Quién lo lee**: confirmado contra ADR-05 al pie de la letra
     (`docs/adr.md:43`). La tabla de reglas vive como markdown dentro de
     `productVexillumRule` (`internal/cli/init.go`, nueva sección
     "Choosing a model and effort"), interpretada por el commander - no
     como un JSON separado que algo en Go parsea y matchea (eso sería
     reimplementar el ruteo, aunque fuera en JSON). El binario solo gana
     `--model`/`--effort` en `vexillum dispatch`, valida contra un
     allowlist fijo y pasa los valores tal cual a `claude` - cero lectura
     del `when` en Go.
  3. **`--effort` real**: confirmado en vivo con `claude --help` en el
     binario instalado (`Claude Code v2.1.267` en esta máquina) - existe
     de verdad, acepta exactamente `low, medium, high, xhigh, max`.
     `--model` también real (`haiku, sonnet, opus, fable` confirmados;
     `--help` solo lista `fable/opus/sonnet` como ejemplos, no exhaustivo,
     pero `haiku` se confirmó funcionando en el E2E de abajo).

- **Implementación** (todo en la misma sesión, tests unitarios pasando +
  E2E en vivo):
  - `state.Task` gana `Model`/`Effort` (`omitempty`, sin bump de
    `SchemaVersion` - mismo criterio que `Redispatches`/
    `AgentNotFoundSince`, campos puramente aditivos).
  - `internal/soldier/claude_args.go` (nuevo): `ValidateModelEffort`
    (exportada, la usa `internal/cli`) y `claudeModelEffortArgs`
    (interna, arma `["--model", x, "--effort", y]` omitiendo lo que esté
    vacío).
  - `internal/soldier/herdr_run.go`: `startAgent`/`startAgentWithBusyRetry`/
    `startAgentOnce` ganan un parámetro `extraArgs []string`, enhebrado
    desde `RunInHerdr` (`claudeModelEffortArgs(task)`), agregado después
    de `--dangerously-skip-permissions` en la llamada a
    `client.AgentStart`.
  - `internal/soldier/soldier.go`: `ClaudeCommand` (path headless, sin
    caller en producción hoy pero se mantuvo por paridad) hace el mismo
    passthrough.
  - `internal/cli/dispatch.go`: `parseDispatchArgs` reconoce `--model`/
    `--effort` mezclados con el prompt; `Dispatch` valida con
    `soldier.ValidateModelEffort` antes de tocar disco o red; `runDispatch`
    setea `task.Model`/`task.Effort` antes de `acquireAndRunInHerdr`.
    `vexillum redispatch` no necesitó cambios - reutiliza el mismo `Task`
    cargado del disco, así que conserva el model/effort original sin
    tocar nada.
  - `internal/cli/init.go`: nueva sección "Choosing a model and effort" en
    `productVexillumRule`, con la tabla de 6 reglas (las 5 del borrador
    del general + el desempate del punto 1 explícito en la regla 3), justo
    después de "Dispatching a soldier".
  - Tests nuevos: `internal/cli/dispatch_test.go` (`TestParseDispatchArgs`
    extendido, `TestRunDispatch_PassesModelEffortToClaude`),
    `internal/soldier/herdr_run_test.go`
    (`TestRunInHerdr_PassesModelAndEffort`),
    `internal/soldier/claude_args_test.go` (nuevo, `ValidateModelEffort` +
    `ClaudeCommand`). Casos documentados en `docs/test-cases.md`, "V3 -
    PARTE D".

- **E2E en vivo, en `vexillum-prueba`** (`~/github/isaias-alt/tmp/vexillum-prueba`,
  proyecto real en GitHub `isaias-alt/vexillum-prueba`, usado en sesiones
  anteriores para probar vexillum): el binario se buildeó al scratchpad de
  la sesión, no a `/opt/homebrew/bin/vexillum` - el classifier de permisos
  bloqueó escribir ahí (ruta fuera del repo), así que se probó con la ruta
  completa al binario del scratchpad en vez de pedir ese permiso.
  - `vexillum init` sobre el proyecto (sus archivos de scaffold estaban
    borrados sin commitear desde antes - estado preexistente, no tocado)
    escribió la sección nueva en `.claude/rules/vexillum.md` - verificado
    con grep.
  - `vexillum dispatch "Run the shell command date..." --kind scout
    --model haiku --effort low` corrió de punta a punta contra un pane de
    herdr real: `status=done`, el transcript del pane muestra
    "Haiku 4.5 · Claude Pro" cargado de verdad, y el task persistido en
    `~/.vexillum/projects/vexillum-prueba-2380e857/tasks/` tiene
    `"model": "haiku", "effort": "low"`. Camp liberado después
    (`vexillum release`).
  - `vexillum dispatch "test" --model gpt-5` y `--effort extreme` fallan
    fail-fast con el mensaje esperado, exit 1, sin crear camp ni pane -
    confirma que la validación corta antes de gastar nada.

## Pendiente para la próxima

- El binario en `/opt/homebrew/bin/vexillum` (el que usa el general en el
  día a día) sigue sin reconstruirse con este cambio - se probó desde el
  scratchpad, no se tocó la instalación real. Si el general quiere este
  feature disponible ya en su `vexillum` de todos los días, falta ese
  rebuild explícito (el classifier lo bloqueó una vez en esta sesión).
- Separación binario personal (brew) vs binario de desarrollo
  (`vexillum-dev`) sigue pendiente para el próximo corte de tag - ver
  memoria de la sesión anterior, no se tocó hoy.
- `vexillum-prueba` quedó con `.vexillum/config.json` modificado y
  `.claude/rules/` nuevo, sin commitear (mismo patrón de sesiones
  anteriores) - decisión del general si los commitea.
