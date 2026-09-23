# 2026-09-23 - Migración de AGENTS.md/CLAUDE.md a .claude/rules/vexillum.md

## Resuelto y en pie

- **Bug real encontrado, no hipotético**: `vexillum init` solo escribía
  `AGENTS.md`/`CLAUDE.md` si no existían ya (`writeFileIfMissing`), sin
  verificar si el contenido existente era del propio vexillum o ajeno.
  Confirmado con dos proyectos reales del filesystem del general:
  - `~/github/kinexa/nutrione/nutrione-api`: `CLAUDE.md` real de 1299
    líneas (documentación propia de NestJS), sin `AGENTS.md`. `init`
    creaba un `AGENTS.md` de vexillum que nada importaba - el commander
    quedaba inerte en silencio.
  - `~/github/isaias-alt/nondeterministic`: `AGENTS.md` propio, con
    `CLAUDE.md` ya symlinkeado a él. `init` no tocaba nada, pero el
    `CLAUDE.md` existente importaba el `AGENTS.md` del general, nunca el
    de vexillum - mismo resultado, inerte en silencio.

- **Investigado el repo real de `firstmate` antes de decidir** (mismo
  criterio que el resto del proyecto): `bin/fm-ensure-agents-md.sh`
  resuelve el mismo problema manteniéndose dentro de la convención
  AGENTS.md/CLAUDE.md - promueve un `CLAUDE.md` real preexistente a
  `AGENTS.md` (`mv`), rechaza (`exit 1`) si ambos son reales y distintos,
  y marca su propia sección con un heading/comentario idempotente en vez
  de un hash de archivo completo. **Descartado como modelo para
  vexillum**: esa máquina de promoción/conflicto existe porque firstmate
  *eligió* mezclar sus instrucciones con el AGENTS.md del proyecto.
  Vexillum no necesita tomar esa misma decisión.

- **Decisión final (discutida y cerrada con el general)**: vexillum deja
  de leer y escribir `AGENTS.md`/`CLAUDE.md` del proyecto por completo.
  El vocabulario del commander (general/camp/soldier/mission/scout) y su
  mecánica (dispatch, land/ship/release, sentinel) se escriben en
  `.claude/rules/vexillum.md` - un archivo/directorio que Claude Code
  carga siempre, con la misma prioridad que `.claude/CLAUDE.md`
  (confirmado contra `code.claude.com/docs/en/memory`, no asumido), sin
  depender de si el proyecto ya tiene su propio `AGENTS.md`/`CLAUDE.md` ni
  de qué versión de Claude Code use el general (el soporte nativo de
  fallback a `AGENTS.md` recién llega en la 2.1.277+; `.claude/rules/` es
  anterior a la 2.1.211).

- **Principio explícito, no solo implícito**: vexillum nunca modifica,
  combina o mueve contenido entre `AGENTS.md`/`CLAUDE.md` del proyecto. Si
  en algún momento quisiera ayudar a reconciliarlos, tiene que preguntar
  primero - nunca decidirlo solo. Esto es lo que efectivamente descarta el
  modelo de firstmate, no solo la duplicación.

- **Alcance global vs. local, cerrado en la misma sesión**: `vexillum
  init` sigue siendo local por defecto (escribe en el proyecto). Se agregó
  `vexillum init --global` (y `vexillum upgrade --global`) como opt-in
  explícito que escribe una sola vez en `~/.claude/rules/vexillum.md`,
  aplicando a toda sesión de Claude Code en la máquina - nunca el
  comportamiento por defecto, coherente con el principio de no ser
  invasivo.

- **Implementación en `internal/cli/init.go` y `upgrade.go`**:
  - `productAgentsMD` + `productClaudeMD` colapsados en un solo
    `productVexillumRule` (mismas 6 secciones - Vocabulary, sentinel,
    dispatch, land, ship, release - solo cambia el párrafo de intro).
  - `localConfig.AgentsMDHash`/`ClaudeMDHash` reemplazados por un solo
    `VexillumRuleHash`.
  - `readLocalConfig`/`writeLocalConfig`/`recordScaffoldHash`
    generalizados para tomar el directorio de `config.json` directamente
    (`<proyecto>/.vexillum` en modo local, `vexillumHome` en modo
    global) - mismo código sirve para los dos modos.
  - `runInit`/`runUpgrade` escriben `.claude/rules/vexillum.md` (con
    `os.MkdirAll` del directorio, que antes no hacía falta porque
    `AGENTS.md`/`CLAUDE.md` iban en la raíz del proyecto).
  - `runInitGlobal`/`runUpgradeGlobal` (nuevas): mismo mecanismo, sin
    chequeo de repo git, contra `~/.claude/rules/vexillum.md` y
    `~/.vexillum/config.json`.
  - `ensureSentinelHook`/`.claude/settings.json` sin cambios - archivo
    distinto, no relacionado.

- **Tests**: `TestInit_HealsMissingClaudeMD` → `TestInit_HealsMissingRuleFile`;
  `TestInit_DoesNotOverwriteEditedAgentsMD` →
  `TestInit_DoesNotOverwriteEditedRuleFile`; mismo patrón en
  `upgrade_test.go`. Sumados 4 tests de `init --global` y 4 de `upgrade
  --global` (clean machine, idempotente, no pisa ediciones, no escribe un
  scaffold con forma de proyecto bajo home; refusal sin inicializar,
  refresh, leave-untouched, --force). `dispatch_test.go`/`doctor_test.go`
  ajustados solo en la llamada a `writeLocalConfig` (ahora recibe el
  subdirectorio `.vexillum`, no el proyecto). Build, vet, gofmt y
  `go test -race ./...` en verde en todo el repo.

- **Verificado en vivo, no solo en tests**: binario recompilado, corrido
  de verdad (`vexillum init`, sin flags) contra `vexillum-prueba`
  (`~/github/isaias-alt/tmp/vexillum-prueba`), que ya tenía un
  `AGENTS.md`/`CLAUDE.md` reales de una versión anterior de vexillum.
  Resultado exacto: creó `.claude/rules/vexillum.md`, actualizó
  `.vexillum/config.json` (el campo viejo `agents_md_hash` desaparece del
  JSON al no existir más en el struct, reemplazado por
  `vexillum_rule_hash`), y **no tocó `AGENTS.md` ni `CLAUDE.md` en
  absoluto** (`git status` solo marca `.vexillum/config.json` modificado
  y `.claude/rules/` como nuevo) - exactamente el comportamiento no
  invasivo documentado.

- **Plan completo, con toda la investigación y las decisiones, en
  `.lavish/rules-migration-plan.html`** (artefacto Lavish, revisado y
  aprobado por el general en la misma sesión - incluye el diagrama
  antes/después, la tabla de alternativas descartadas, y el principio de
  no invasividad).

## Pendiente para la próxima

- **`vexillum init --global`/`upgrade --global` no se corrieron contra la
  máquina real** (`~/.claude/rules/`, `~/.vexillum/config.json`) - solo
  contra directorios temporales en los tests. Es una acción de alcance
  amplio (afecta toda sesión de Claude Code en la máquina) que no
  correspondía tomar sin pedírselo al general explícitamente. Si la quiere
  probar en vivo, es la que falta.
- **Segunda mitad de la verificación E2E pendiente, tal como se acordó**:
  "primero yo, después el general" - falta que el general repita la
  prueba en `vexillum-prueba` (o donde prefiera) antes de dar esto por
  cerrado del todo.
- `vexillum-prueba` quedó con cambios reales sin commitear
  (`.vexillum/config.json` modificado, `.claude/rules/vexillum.md`
  nuevo) - decisión del general si los commitea, y si en algún momento
  quiere borrar a mano el `AGENTS.md`/`CLAUDE.md` viejos de ese proyecto
  (vexillum nunca lo va a hacer solo).
