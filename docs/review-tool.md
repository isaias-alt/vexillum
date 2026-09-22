# review-tool - diseño de la integración (PRD v2, B.4)

Este documento cierra lo que el PRD v2 (`docs/prd-v2.md`, sección B.4) dejó pendiente antes de implementar: el diseño concreto del gate y la decisión abierta de quién dispara la apertura del PR. Investigado contra el repo real, no inventado.

## Corrección respecto del PRD

El PRD describe `review-tool` como "modelo de referencia: upstream-tool, que lo configura por repo (`.review-tool.yaml`)... con gh-tool en el paso final". Investigado en esta sesión (vía GitHub API, no supuesto):

- **`review-tool` no es una feature interna de upstream-tool.** Es su propio repo real y standalone: `github.com/upstream`. upstream-tool lo consume igual que cualquier otro proyecto (su propio `.review-tool.yaml` en la raíz de upstream-tool es un archivo de config *de cliente*, no la fuente del gate).
- **No es un Agent Skill instalado vía `npx skills add`** (a diferencia de quota-tool/forum/chrome-devtools-tool). Es un binario real, instalado por `curl -fsSL .../install.sh | sh` - el mismo modelo de distribución que vexillum mismo (brew/curl, sin dependencias de runtime). `review-tool init` instala además su propio skill de agente (`/review-tool`) a nivel usuario, automáticamente - eso lo maneja la herramienta, no vexillum.
- **No necesita gh-tool para abrir el PR.** `review-tool` pone un git remote local (`review-tool`, un bare repo disposable en `~/.review-tool/repos/<hash>.git`) delante del remote real. `git push review-tool <branch>` dispara un pipeline propio (review → test → docs → lint) en un worktree aislado, y **solo si todo pasa** reenvía la rama al remote real (`origin`, o un fork con `--fork-url`) y **abre el PR él mismo** - con su propia integración de GitHub, no con gh-tool. gh-tool queda fuera del alcance mínimo de B.4: no es una dependencia real de este flujo. Si más adelante una mission necesita operar sobre issues/releases/workflow runs de GitHub por su cuenta, gh-tool sigue siendo una integración aparte, a pedido, como cualquier otro AXI del catálogo - no algo que B.4 necesite.

## Cómo funciona `review-tool`, en concreto

- `review-tool init` (una vez, por repo, corrido por el general): crea el remote `review-tool` y confirma el remote real de push (`origin` por defecto, o `--fork-url` para forks).
- Tres formas de disparar el gate: `git push review-tool <branch>` (la vía git explícita), la TUI (`review-tool`), o el skill de agente (`/review-tool`). Las tres corren el mismo pipeline.
- El pipeline corre en un **worktree aislado propio de review-tool** (no el camp de vexillum) - review, test (con evidencia), docs, lint; hallazgos "auto-fix" se aplican solos, los "ask-user" esperan una decisión humana.
- Solo cuando todo pasa: reenvía al remote real y abre el PR.
- Estado consultable sin TUI: `review-tool axi status` (JSON/TOON estructurado, exit 0 éxito / 1 fallo operacional / 2 uso incorrecto), `review-tool runs`, `review-tool doctor` (salud del propio review-tool, no confundir con `vexillum doctor`).

## Decisión abierta del PRD: quién dispara la apertura del PR

**Cerrada: el binario, de forma determinista.** No el soldier desde su propio prompt.

**Por qué.** El propio PRD ya daba la razón antes de que se investigara la mecánica real: "por ser efecto irreversible, la lógica de 'algo confiable no se deja al criterio del LLM' empuja hacia el binario". La investigación de esta sesión confirma que hay un mecanismo determinista real para hacerlo: `git push review-tool <branch>` es un comando git plano, sin juicio de por medio - vexillum puede correrlo él mismo contra el camp de una mission ya terminada, en el momento exacto que el general lo pide, en vez de dejar que un soldier decida solo cuándo "está listo para el PR".

**Lo que el binario NO hace:** no supervisa el pipeline de review-tool hasta el final, no reintenta fixes, no decide si un finding "ask-user" se aprueba o se saltea. Eso ya lo resuelven la TUI y el skill de `/review-tool` de la herramienta real - reimplementarlo en vexillum sería reinventar plomería ya resuelta (mismo criterio que ya se aplicó para no reimplementar la API de GitHub a mano en el PRD original). El rol de vexillum termina en el push determinista; el general sigue el progreso con las herramientas propias de `review-tool`.

## Alcance de la implementación

**Entra:**
- `vexillum doctor` reporta si el binario `review-tool` está instalado, y si este proyecto ya corrió `review-tool init` (existe el remote git `review-tool`) - informativo, nunca bloqueante, mismo criterio que el resto de `doctor`.
- Comando nuevo `vexillum ship <task-id>`: para una mission en estado `done`, con camp resuelto, corre `git push review-tool <camp-branch>` desde el camp - determinista, ninguna decisión de un LLM en el medio. Si el proyecto todavía no está gateado, `ship` corre `review-tool init` él mismo antes de pushear (ver "Auto-gate en `ship`, no en `init`" abajo). Reporta el resultado y le dice al general cómo seguir el pipeline (`review-tool axi status` / la TUI), sin intentar supervisarlo él mismo.
- `productAgentsMD`: una nota breve explicando que `vexillum ship` existe para este flujo, y que el commander se lo ofrece al general como alternativa a `vexillum land` cuando el general quiere un PR real validado en vez de un fast-forward local - nunca lo corre por su cuenta sin que el general lo pida (mismo criterio de "efecto irreversible, se pregunta primero" que ya rige para `redispatch`).

**No entra (a propósito):**
- Ninguna lógica de review/test/lint/docs en el binario Go - eso es 100% de `review-tool`.
- Ninguna supervisión del pipeline post-push (sentinel no lo poll-ea) - `review-tool` ya tiene su propia TUI/CLI para eso.
- `review-tool init` no se corre desde `vexillum init` (ver más abajo por qué).
- gh-tool: no es una dependencia de este flujo (ver "Corrección" arriba). No se agrega a `doctor` como parte de B.4.
- Manejo de merge: `vexillum ship` abre el camino al PR: aterrizarlo (mergear en GitHub) es responsabilidad del general, igual que hoy con cualquier PR.

## Auto-gate en `ship`, no en `init` (decisión tomada con el general después de la primera implementación)

Pedido del general: que el gate "venga por defecto" pero "no se use a no ser que se llame [ship] en el prompt" - separar el *setup* del *uso*. Se evaluaron dos lugares para el setup automático:

- **`vexillum init`**: descartado. `vexillum init` es hoy el comando más básico de vexillum, puramente local, sin red (mismo principio que ya rige para las AXIs: "meter red en init degradaría el comando más básico"). Auto-correr `review-tool init` ahí exigiría que `review-tool` ya esté instalado (si no, ¿fallar el init más básico del proyecto, o saltear en silencio y entonces no "viene por defecto" de verdad?) y asumir que el proyecto ya tiene un remote real de GitHub - no siempre cierto al momento de inicializar.
- **`vexillum ship`, lazy, en el primer uso** (elegido): la primera vez que `ship` encuentra un proyecto no gateado, si `review-tool` está instalado, le corre `init` ahí mismo antes de pushear. Si `review-tool` no está instalado, falla con un mensaje claro en vez de un error de `exec` confuso. Esto da exactamente lo pedido: nadie tiene que acordarse de correr `review-tool init` a mano por separado (el setup "viene por defecto" la primera vez que hace falta), pero nada pasa hasta que el general efectivamente pide shippear algo - cero red/side-effects en el `init` básico.

`doctor` sigue reportando si el proyecto está gateado o no, pero ya no lo presenta como algo que el general tiene que resolver a mano: "installed, but this project hasn't run 'review-tool init' yet - 'vexillum ship' will do this automatically the first time".

## Criterio de terminado

`vexillum doctor` reporta la presencia de `review-tool` y si el proyecto está gateado. `vexillum ship <task-id>` sobre una mission `done` en un proyecto no gateado corre `review-tool init` y después empuja la rama al remote `review-tool`, en un solo comando, con éxito determinista (el binario no depende de ningún juicio de agente para decidir si push). La verificación end-to-end real (que el pipeline de `review-tool` corra y abra un PR de verdad) queda para la tanda de pruebas en vivo, junto con las demás pendientes de esta sesión - requiere `review-tool` instalado y un repo real con remote de GitHub.
