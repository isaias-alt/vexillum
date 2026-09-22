# no-mistakes - diseño de la integración (PRD v2, B.4)

Este documento cierra lo que el PRD v2 (`docs/prd-v2.md`, sección B.4) dejó pendiente antes de implementar: el diseño concreto del gate y la decisión abierta de quién dispara la apertura del PR. Investigado contra el repo real, no inventado.

## Corrección respecto del PRD

El PRD describe `no-mistakes` como "modelo de referencia: firstmate, que lo configura por repo (`.no-mistakes.yaml`)... con gh-axi en el paso final". Investigado en esta sesión (vía GitHub API, no supuesto):

- **`no-mistakes` no es una feature interna de firstmate.** Es su propio repo real y standalone: `github.com/kunchenguid/no-mistakes`. firstmate lo consume igual que cualquier otro proyecto (su propio `.no-mistakes.yaml` en la raíz de firstmate es un archivo de config *de cliente*, no la fuente del gate).
- **No es un Agent Skill instalado vía `npx skills add`** (a diferencia de quota-axi/lavish/chrome-devtools-axi). Es un binario real, instalado por `curl -fsSL .../install.sh | sh` - el mismo modelo de distribución que vexillum mismo (brew/curl, sin dependencias de runtime). `no-mistakes init` instala además su propio skill de agente (`/no-mistakes`) a nivel usuario, automáticamente - eso lo maneja la herramienta, no vexillum.
- **No necesita gh-axi para abrir el PR.** `no-mistakes` pone un git remote local (`no-mistakes`, un bare repo disposable en `~/.no-mistakes/repos/<hash>.git`) delante del remote real. `git push no-mistakes <branch>` dispara un pipeline propio (review → test → docs → lint) en un worktree aislado, y **solo si todo pasa** reenvía la rama al remote real (`origin`, o un fork con `--fork-url`) y **abre el PR él mismo** - con su propia integración de GitHub, no con gh-axi. gh-axi queda fuera del alcance mínimo de B.4: no es una dependencia real de este flujo. Si más adelante una mission necesita operar sobre issues/releases/workflow runs de GitHub por su cuenta, gh-axi sigue siendo una integración aparte, a pedido, como cualquier otro AXI del catálogo - no algo que B.4 necesite.

## Cómo funciona `no-mistakes`, en concreto

- `no-mistakes init` (una vez, por repo, corrido por el general): crea el remote `no-mistakes` y confirma el remote real de push (`origin` por defecto, o `--fork-url` para forks).
- Tres formas de disparar el gate: `git push no-mistakes <branch>` (la vía git explícita), la TUI (`no-mistakes`), o el skill de agente (`/no-mistakes`). Las tres corren el mismo pipeline.
- El pipeline corre en un **worktree aislado propio de no-mistakes** (no el camp de vexillum) - review, test (con evidencia), docs, lint; hallazgos "auto-fix" se aplican solos, los "ask-user" esperan una decisión humana.
- Solo cuando todo pasa: reenvía al remote real y abre el PR.
- Estado consultable sin TUI: `no-mistakes axi status` (JSON/TOON estructurado, exit 0 éxito / 1 fallo operacional / 2 uso incorrecto), `no-mistakes runs`, `no-mistakes doctor` (salud del propio no-mistakes, no confundir con `vexillum doctor`).

## Decisión abierta del PRD: quién dispara la apertura del PR

**Cerrada: el binario, de forma determinista.** No el soldier desde su propio prompt.

**Por qué.** El propio PRD ya daba la razón antes de que se investigara la mecánica real: "por ser efecto irreversible, la lógica de 'algo confiable no se deja al criterio del LLM' empuja hacia el binario". La investigación de esta sesión confirma que hay un mecanismo determinista real para hacerlo: `git push no-mistakes <branch>` es un comando git plano, sin juicio de por medio - vexillum puede correrlo él mismo contra el camp de una mission ya terminada, en el momento exacto que el general lo pide, en vez de dejar que un soldier decida solo cuándo "está listo para el PR".

**Lo que el binario NO hace:** no supervisa el pipeline de no-mistakes hasta el final, no reintenta fixes, no decide si un finding "ask-user" se aprueba o se saltea. Eso ya lo resuelven la TUI y el skill de `/no-mistakes` de la herramienta real - reimplementarlo en vexillum sería reinventar plomería ya resuelta (mismo criterio que ya se aplicó para no reimplementar la API de GitHub a mano en el PRD original). El rol de vexillum termina en el push determinista; el general sigue el progreso con las herramientas propias de `no-mistakes`.

## Alcance de la implementación

**Entra:**
- `vexillum doctor` reporta si el binario `no-mistakes` está instalado, y si este proyecto ya corrió `no-mistakes init` (existe el remote git `no-mistakes`) - informativo, nunca bloqueante, mismo criterio que el resto de `doctor`.
- Comando nuevo `vexillum ship <task-id>`: para una mission en estado `done`, con camp resuelto, corre `git push no-mistakes <camp-branch>` desde el camp - determinista, ninguna decisión de un LLM en el medio. Si el proyecto todavía no está gateado, `ship` corre `no-mistakes init` él mismo antes de pushear (ver "Auto-gate en `ship`, no en `init`" abajo). Reporta el resultado y le dice al general cómo seguir el pipeline (`no-mistakes axi status` / la TUI), sin intentar supervisarlo él mismo.
- `productAgentsMD`: una nota breve explicando que `vexillum ship` existe para este flujo, y que el commander se lo ofrece al general como alternativa a `vexillum land` cuando el general quiere un PR real validado en vez de un fast-forward local - nunca lo corre por su cuenta sin que el general lo pida (mismo criterio de "efecto irreversible, se pregunta primero" que ya rige para `redispatch`).

**No entra (a propósito):**
- Ninguna lógica de review/test/lint/docs en el binario Go - eso es 100% de `no-mistakes`.
- Ninguna supervisión del pipeline post-push (sentinel no lo poll-ea) - `no-mistakes` ya tiene su propia TUI/CLI para eso.
- `no-mistakes init` no se corre desde `vexillum init` (ver más abajo por qué).
- gh-axi: no es una dependencia de este flujo (ver "Corrección" arriba). No se agrega a `doctor` como parte de B.4.
- Manejo de merge: `vexillum ship` abre el camino al PR: aterrizarlo (mergear en GitHub) es responsabilidad del general, igual que hoy con cualquier PR.

## Auto-gate en `ship`, no en `init` (decisión tomada con el general después de la primera implementación)

Pedido del general: que el gate "venga por defecto" pero "no se use a no ser que se llame [ship] en el prompt" - separar el *setup* del *uso*. Se evaluaron dos lugares para el setup automático:

- **`vexillum init`**: descartado. `vexillum init` es hoy el comando más básico de vexillum, puramente local, sin red (mismo principio que ya rige para las AXIs: "meter red en init degradaría el comando más básico"). Auto-correr `no-mistakes init` ahí exigiría que `no-mistakes` ya esté instalado (si no, ¿fallar el init más básico del proyecto, o saltear en silencio y entonces no "viene por defecto" de verdad?) y asumir que el proyecto ya tiene un remote real de GitHub - no siempre cierto al momento de inicializar.
- **`vexillum ship`, lazy, en el primer uso** (elegido): la primera vez que `ship` encuentra un proyecto no gateado, si `no-mistakes` está instalado, le corre `init` ahí mismo antes de pushear. Si `no-mistakes` no está instalado, falla con un mensaje claro en vez de un error de `exec` confuso. Esto da exactamente lo pedido: nadie tiene que acordarse de correr `no-mistakes init` a mano por separado (el setup "viene por defecto" la primera vez que hace falta), pero nada pasa hasta que el general efectivamente pide shippear algo - cero red/side-effects en el `init` básico.

`doctor` sigue reportando si el proyecto está gateado o no, pero ya no lo presenta como algo que el general tiene que resolver a mano: "installed, but this project hasn't run 'no-mistakes init' yet - 'vexillum ship' will do this automatically the first time".

## Criterio de terminado

`vexillum doctor` reporta la presencia de `no-mistakes` y si el proyecto está gateado. `vexillum ship <task-id>` sobre una mission `done` en un proyecto no gateado corre `no-mistakes init` y después empuja la rama al remote `no-mistakes`, en un solo comando, con éxito determinista (el binario no depende de ningún juicio de agente para decidir si push). La verificación end-to-end real (que el pipeline de `no-mistakes` corra y abra un PR de verdad) queda para la tanda de pruebas en vivo, junto con las demás pendientes de esta sesión - requiere `no-mistakes` instalado y un repo real con remote de GitHub.
