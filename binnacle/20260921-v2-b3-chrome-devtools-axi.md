# 2026-09-21 - v2 B.3: chrome-devtools-axi, namespacing de browser por soldier + limpieza en redispatch

## Nota de proceso

El general pidió seguir implementando B.3/B.4 punto por punto (implementar → probar → siguiente) pero avisó que iba a estar afuera ("cenare luego vuelvo, no podre aceptar los planes") y no iba a poder aprobar el `ExitPlanMode` interactivo. Rechazó ese tool call explícitamente con "aplica, para la proxima no planifiques" - se interpretó como autorización a implementar directo sin el paso formal de aprobación de plan por el resto de esta sesión desatendida. Se guardó como memoria de feedback (`feedback_skip_plan_mode_unattended`) para sesiones futuras. El plan ya escrito (contenido íntegro más abajo) se implementó tal cual, sin cambios respecto de lo diseñado.

## Resuelto y en pie

- **Investigación real antes de implementar**: `github.com/kunchenguid/chrome-devtools-axi` recomienda `npx skills add kunchenguid/chrome-devtools-axi --skill chrome-devtools-axi -g` (mismo patrón que quota-axi: `--skill` = nombre del repo, con `-g` - a diferencia de lavish-axi). Arquitectura real: un proceso "bridge" persistente por sesión (`CHROME_DEVTOOLS_AXI_SESSION=<nombre>`), estado en `~/.chrome-devtools-axi/sessions/<nombre>/bridge.pid`, cierre explícito con `CHROME_DEVTOOLS_AXI_SESSION=<nombre> chrome-devtools-axi stop`.

- **Verificado en la máquina real, no asumido**: `herdr tab create --help` expone `--env <KEY=VALUE>` ("Set an environment variable for the launched process"), confirmado que acepta múltiples `--env` sin error de parseo (`herdr tab create ... --env A=1 --env B=2` pasó de la validación de args). Esto es lo que resuelve el problema de diseño central de B.3: cómo namespacear el browser de cada soldier por su propio task id sin que el soldier tenga que cooperar.

- **Diseño**: cada pane de soldier arranca con `CHROME_DEVTOOLS_AXI_SESSION=vx-<task-id>` seteado siempre (barato, inofensivo si el soldier nunca toca un browser). Al descartar un camp (`soldier.DiscardInHerdr`, ya usado por `redispatch` desde A.2), se reconstruye ese mismo nombre y se chequea si `~/.chrome-devtools-axi/sessions/vx-<task-id>/bridge.pid` existe - **detección por `stat`, sin invocar nada**, respetando el principio "on-demand, sin costo hasta que se usa" del PRD. Solo si existe se invoca `npx -y chrome-devtools-axi stop` con esa sesión, best-effort.

- **`internal/herdr.Client.CreateTab`** gana un parámetro variádico `env ...string`. Impacto medido antes de tocar nada (`grep -rn CreateTab`): 1 call site real (`internal/soldier/herdr_run.go`) + 3 fakes de test (`internal/soldier`, `internal/cli`, `internal/sentinel`) - actualizados todos, mecánico.

- **`internal/soldier/chrome_devtools.go`** (nuevo): `chromeDevtoolsSessionName(taskID) = "vx-" + taskID`, `stopOrphanBrowser(taskID, homeDir)` (stat-first, best-effort).

- **`internal/soldier/herdr_run.go`**: `RunInHerdr` pasa `CHROME_DEVTOOLS_AXI_SESSION=vx-<task-id>` a `CreateTab` en todo dispatch (fresco o redispatch, mismo código). `DiscardInHerdr` gana `homeDir string` y llama a `stopOrphanBrowser` antes de intentar `TabClose`.

- **`internal/cli/redispatch.go`**: `Redispatch` (la exportada) resuelve `homeDir` vía `os.UserHomeDir()` - mismo patrón que ya usa `Doctor()` - sin tocar `resolveDirs()` (que nadie más necesitaba extender). `runRedispatch` propaga `homeDir` a `DiscardInHerdr`.

- **`internal/cli/doctor.go`**: nueva entrada en `knownAXIs` (`{Name: "chrome-devtools-axi", Repo: "kunchenguid/chrome-devtools-axi", Global: true}`) - sin sorpresas esta vez, mismo patrón que quota-axi.

- **`productAgentsMD`**: una línea agregada donde ya se explica `vexillum redispatch`, avisando que también cierra el browser huérfano del soldier muerto - nada de cómo *usar* el browser, eso lo enseña el propio skill una vez instalado.

- **Tests nuevos**: `internal/soldier/chrome_devtools_test.go` (stat-first: no invoca nada sin `bridge.pid`, invoca `npx -y chrome-devtools-axi stop` con el env correcto cuando existe), un caso en `herdr_run_test.go` (verifica el env en `CreateTab`), dos casos en `herdr_release_test.go` (`DiscardInHerdr` con y sin browser huérfano), dos en `doctor_test.go` (B3-01/B3-02), uno en `redispatch_test.go` (flujo completo con `bridge.pid` presente). Los stubs de `npx`/`git` en tests usan PATH con **prepend**, no reemplazo (`t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))`) - un primer intento reemplazando el PATH entero rompió `camp.Discard` al dejar `git` inaccesible, encontrado por el test fallando, no por análisis.

- Build, vet, gofmt y toda la suite con `-race` en verde.

## Pendiente para la próxima

- Diferido a la tanda de pruebas en vivo (junto con A.2/B.1/B.2, decisión ya tomada): confirmar con un browser real que `chrome-devtools-axi stop` efectivamente mata el proceso - lo que se probó acá es que se invoca el comando correcto con el env correcto, no que un browser real muere.
- Sigue B.4 (no-mistakes) - el PRD exige escribir `no-mistakes.md` y cerrar una decisión abierta (quién dispara el PR: soldier vs binario) **antes** de implementar. Es el siguiente paso, no una implementación directa como B.1-B.3.
