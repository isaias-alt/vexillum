# AGENTS.md - Vexillum (repo raíz)

Este archivo gobierna cómo un agente de código (Claude Code) trabaja en el repositorio de **vexillum**. No confundir con el AGENTS.md de producto que vexillum scaffoldea en los proyectos del usuario: ese define el comportamiento del commander. Este define cómo se construye vexillum.

## Qué es vexillum

Orquestador de agentes de código por CLI, escrito en Go. Le hablás a un agente coordinador (commander) y despacha subagentes (soldiers) en paralelo, cada uno aislado en un git worktree (camp), que devuelven un PR (mission) o un reporte (scout). Un componente de supervisión (sentinel) vigila a los soldiers y despierta al commander solo cuando hace falta. Estado y config en `~/.vexillum/`.

## Reglas de arquitectura que no se relitigan

No las cambies ni propongas alternativas sin que el general lo pida explícitamente.

- **Lenguaje: Go.** No sugieras reescribir en otro lenguaje.
- **Distribución: binario por brew/curl**, multiplataforma. No introduzcas dependencias de runtime (Node, Python) en el camino de instalación.
- **Harness único: Claude Code** en la v1. Pi está diferido; no construyas la abstracción multi-harness todavía, solo dejá la costura preparada.
- **Backend: herdr primario, tmux de control.** No elimines tmux; no promuevas tmux a primario.
- **La inteligencia del commander vive en el AGENTS.md de producto (markdown), no en Go.** El binario hace plomería determinista. No reimplementes ruteo/razonamiento en Go.
- **Vocabulario del dominio en inglés** (commander, soldier, mission, scout, camp, sentinel), en código y en voz del agente. Sin traducción.
- **Lieutenant (segundo commander) está fuera de la v1.** No lo implementes ni diseñes el código anticipándolo.

## Construcción por capas

Se construye por capas, sin pasar a la siguiente sin la anterior probada:

1. Capa 1: CLI base (`init` + `doctor`). Sin procesos ni concurrencia.
2. Capa 2: estado en disco (structs a JSON en `~/.vexillum/`).
3. Capa 3: un proceso (un soldier en un camp, secuencial).
4. Capa 4: concurrencia (N soldiers, sentinel sobre la socket API de herdr, restart-proof).

Trabajá en la capa activa. No adelantes código de capas futuras salvo pedido explícito.

## Convenciones de código Go

- Errores explícitos con `if err != nil`; no los tragues. Envolvé con contexto (`fmt.Errorf("...: %w", err)`) cuando aporte.
- Nada de panics en flujo normal; panic solo para estados verdaderamente irrecuperables.
- Código y comentarios en inglés. Los mensajes de CLI al usuario también en inglés.
- La escritura de estado a disco es atómica desde la Capa 2 (escribir a temp + rename), aunque en esa capa se invoque a mano, porque la Capa 4 escribe en paralelo.
- Tests con la librería estándar (`testing`). Cada capa se prueba antes de avanzar (ver casos de prueba).
- Mantené el binario autosuficiente: sin dependencias de runtime externas más allá de las herramientas que orquesta (Claude Code, herdr, tmux), que se verifican con `doctor`, no se instalan.
