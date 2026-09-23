# 2026-09-22 - ReleaseInHerdr no detenía el browser huérfano del soldier

## Resuelto y en pie

- **Bug encontrado leyendo el código, no reportado en vivo**: `DiscardInHerdr`
  (el camino destructivo, usado por `redispatch` sobre una tarea
  `interrupted`) llamaba a `stopOrphanBrowser`; `ReleaseInHerdr` (el camino
  normal, `vexillum release`) no. Efecto: un soldier que usó
  chrome-devtools-axi y terminó bien (camp limpio, aterrizado, release
  normal) dejaba el bridge del browser vivo para siempre - solo un soldier
  que terminaba mal y se redespachaba tenía su browser limpiado. El caso
  común (termina bien) era justo el que no se limpiaba.

- **Fix**: `ReleaseInHerdr(task, c, client, homeDir)` gana el parámetro
  `homeDir` y llama a `stopOrphanBrowser(task.ID, homeDir)` después de que
  `camp.Release` tiene éxito y antes de `TabClose` - mismo orden y mismo
  criterio best-effort que ya regía `DiscardInHerdr`. Si `camp.Release`
  rechaza (sucio, no aterrizado, dueño equivocado), ni el browser ni el pane
  se tocan.

- **Caller actualizado**: `internal/cli/dispatch.go`, `Release`/`runRelease`
  ahora resuelven `homeDir` vía `os.UserHomeDir()` (mismo patrón que ya usa
  `Redispatch` para `DiscardInHerdr`) y se lo pasan a `ReleaseInHerdr`.

- **Comentario desactualizado borrado**: el párrafo sobre `DiscardInHerdr`
  que decía "Not built yet - no browser-tracking state exists on Task
  today" ya no era cierto (`stopOrphanBrowser` existe y se llama ahí mismo,
  debajo del comentario) - eliminado en vez de dejarlo mintiendo sobre el
  propio código que lo rodea.

- **Casos de prueba nuevos**: `docs/test-cases.md` B3-06 (stopea un bridge
  real), B3-07 (no invoca nada si nunca hubo browser), B3-08 (un
  `camp.Release` rechazado no toca ni browser ni pane) - espejo de
  B3-03/B3-04 para el camino no destructivo. Tests en
  `internal/soldier/herdr_release_test.go`:
  `TestReleaseInHerdr_StopsOrphanBrowser`,
  `TestReleaseInHerdr_NoOrphanBrowserIsFine`, y la extensión de
  `TestReleaseInHerdr_KeepsTabOpenWhenCampRefuses` para verificar que un
  release rechazado no invoca `npx`.

- **Corrección aparte, de paso**: la entrada de binnacle
  `20260920-distribucion-brew-curl.md` describía a `gentle-ai` (la
  referencia usada para el patrón de distribución brew/curl) como
  "orquestador de agentes de código" - el general corrigió: es un
  configurador de ecosistema de agentes (memoria persistente, Spec-Driven
  Development, skills, MCP, persona), no lanza ni supervisa agentes en
  paralelo. Corregida esa única línea; las demás menciones de `gentle-ai`
  en ese archivo solo lo citaban como referencia técnica (patrón de
  `.goreleaser.yaml`, de `install.sh`, de `license:`) y no necesitaban
  cambio.

- Build, vet, gofmt y `go test -race ./...` en verde después de este
  cambio.
