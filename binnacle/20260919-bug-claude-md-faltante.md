# 2026-09-19 - Bug real: `vexillum init` nunca escribía `CLAUDE.md`

## Resuelto y en pie

- **Bug encontrado en uso real, no en tests**: el general le pedía al
  commander en `vexillum-prueba` que lanzara un soldier, y el commander
  usaba su propia herramienta "Agent" (subagentes nativos de Claude Code)
  en vez de correr `soldier-demo` - ignorando por completo las
  instrucciones del `AGENTS.md` de producto, incluso después de
  reescribirlas de forma explícita y reiniciar la sesión. Causa raíz:
  **Claude Code carga `CLAUDE.md` automáticamente, no un `AGENTS.md`
  suelto**. El propio `CLAUDE.md` de este repo de vexillum es literalmente
  `@AGENTS.md` (una directiva de import) - el general ya sabía esto para
  el repo de vexillum, pero `vexillum init` (Capa 1) nunca lo replicaba en
  los proyectos que scaffoldea. Resultado: en todo proyecto inicializado
  con `vexillum init` hasta ahora, el `AGENTS.md` de producto nunca fue
  leído por ninguna sesión de Claude Code, en absoluto.

- **Arreglo en `internal/cli/init.go`**: se agrega una constante
  `productClaudeMD = "@AGENTS.md\n"` y se escribe `CLAUDE.md` con el mismo
  criterio de no-sobrescritura que ya tenía `AGENTS.md` (nunca pisa un
  archivo existente). Se generalizó `writeAgentsMDIfMissing` a
  `writeFileIfMissing(projectDir, name, content)` para no duplicar la
  lógica entre los dos archivos.

- **Sanado de proyectos ya inicializados antes de este fix**: como
  `vexillum-prueba` ya tenía `.vexillum/config.json` (de una corrida
  anterior de `init`), el chequeo original de "ya inicializado" cortaba
  antes de llegar a escribir ningún archivo nuevo - re-correr `init` no
  iba a arreglar el problema para proyectos existentes. Se cambió
  `runInit` para que, incluso en el camino de "ya inicializado", siga
  intentando crear `AGENTS.md`/`CLAUDE.md` si faltan (nunca sobrescribe
  los que ya existen) y avise que restauró algo, en vez de solo decir
  "nothing to do". Nuevo caso de test `L1-04b` en `docs/test-cases.md`,
  test `TestInit_HealsMissingClaudeMD`.

- **`vexillum-prueba` arreglado a mano primero** (para no bloquear la
  prueba del general mientras se armaba el fix real) y confirmado que el
  binario real (`vexillum init`, recompilado) también lo sana
  correctamente.

- Build, vet, gofmt y tests verdes (43 tests en total).

## Confirmado: el fix funcionó

- El general probó con sesión nueva después del fix: el commander en
  `vexillum-prueba` **sí** leyó el `AGENTS.md` y corrió `soldier-demo` vía
  Bash en background, tal como se esperaba - ya no usó su Agent tool
  nativo. Confirma que la causa raíz era exactamente la falta de
  `CLAUDE.md`.

## Segundo bug real encontrado en la misma prueba: `agent_pane_busy`

- El soldier falló al arrancar con `agent_pane_busy` en el pane recién
  creado (`wW:p3`) - un código de error no documentado en la doc pública
  de herdr, encontrado en uso real. No se pudo reproducir de forma
  determinística probando a mano (`tab create` + `agent start` inmediato,
  dos veces, ambas funcionaron) - parece una condición de carrera
  ocasional (el shell del pane recién creado, asentándose).

- **Arreglo**: `startAgent` (`internal/soldier/herdr_run.go`) ahora
  reintenta hasta 5 veces con 500ms de espera entre intentos cuando
  `AgentStart` devuelve `agent_pane_busy` (nuevo helper
  `herdr.IsPaneBusy`), antes de rendirse. No hay ningún diálogo que
  descartar en este caso - es una espera pura hasta que el pane se
  asiente. Se separó `startAgentOnce` de `startAgent` para que el reintento
  envuelva el intento completo (incluida la detección del diálogo de
  confianza, que sigue funcionando igual para `agent_not_ready`).

- 2 tests nuevos (`TestRunInHerdr_RetriesPaneBusy`,
  `TestRunInHerdr_GivesUpOnPersistentPaneBusy`) con un `fakeHerdr` que
  simula N intentos de "busy" antes de éxito o fallo definitivo.

- Build, vet, gofmt y tests verdes (45 tests en total).

## Pendiente para la próxima

- Sigue sin confirmarse en vivo que el reintento de `agent_pane_busy`
  resuelve el problema real (no se pudo forzar la condición de carrera a
  voluntad para probarlo end-to-end) - queda pendiente de que el general
  lo vuelva a intentar y, si vuelve a fallar, revisar si 5 intentos x
  500ms alcanza o si hace falta una ventana más larga.
- Vale la pena reconsiderar si en algún momento conviene escribir
  directamente el contenido combinado en `CLAUDE.md` en vez de usar
  `@AGENTS.md` como import - por ahora se replicó exactamente el patrón
  que ya usa el propio repo de vexillum, sin inventar nada nuevo.
