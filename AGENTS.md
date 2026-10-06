# AGENTS.md - Vexillum (repo raíz)

Este archivo gobierna cómo un agente de código (Claude Code) trabaja en el repositorio de **vexillum**. No confundir con el AGENTS.md de producto que vexillum scaffoldea en los proyectos del usuario: ese define el comportamiento del commander. Este define cómo se construye vexillum.

## Qué es vexillum

Orquestador de agentes de código por CLI, escrito en Go. Le hablás a un agente coordinador (commander) y despacha subagentes (soldiers) en paralelo, cada uno aislado en un git worktree (camp), que devuelven un PR (mission) o un reporte (scout). Un componente de supervisión (sentinel) vigila a los soldiers y despierta al commander solo cuando hace falta. Estado y config en `~/.vexillum/`.

Este repositorio no es solo el binario Go: también contiene `site/`, una app Next.js separada (marketing/docs, con su propio `package.json` y `pnpm-lock.yaml`) gobernada por `site/AGENTS.md`, y `skills/muster/`, un skill de Claude Code first-party que se distribuye dentro de este repo y que `vx doctor` reporta como instalado o no (ver `skills/muster/SKILL.md`). Las reglas de este archivo, en particular la sección siguiente, son sobre el binario Go; no aplican a esos dos.

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

## Changelog generado

No existe un CHANGELOG.md y nunca se edita a mano. goreleaser genera las notas desde los commits convencionales al publicar el release (config en `.goreleaser.yaml`), y la página `/docs/changelog` del sitio se arma desde los releases de GitHub con `site/scripts/changelog.mjs` (archivos generados y gitignoreados, con encabezado de auto-generado). Para que una entrada salga bien agrupada, usá el prefijo `feat:`, `fix:` o `docs:`; `chore:` y `test:` se filtran.

## Bitácoras y TODOs

Las bitácoras de sesión (ver skill `binnacle`, local y gitignoreada en este
repo) y los TODOs de trabajo pendiente no viven en el repo de vexillum: viven
en el repo de notas privado del maintainer, con carpetas `binnacles/` y
`todos/`. Después de escribir o actualizar un archivo ahí, commiteá y pusheá
ese repo a mano (no confiar en su auto-sync periódico para esto) antes de dar
la sesión por terminada.

`todos/`: un archivo markdown por item, nombre `YYYYMMDD-tema-corto.md`, con
una sección `## Estado` al final que dice si sigue abierto, parcial o
resuelto. Los que se resuelven del todo se mueven a `todos/closed/` para no
ensuciar la vista activa, sin perder el historial - los abiertos o
parcialmente resueltos quedan sueltos en `todos/`.

<!-- BEGIN VEXILLUM v:1 hash:3329663f -->
## Vexillum commander

### Role check

Leé esto primero. Estas reglas aplican solo si sos el commander, hablando
directamente con el general. Si estás corriendo dentro de un camp porque un
commander te despachó como soldier (una mission o un scout), no aplican para vos:
este bloque está en todos los checkouts del proyecto, camps incluidos, así que lo
estás leyendo por accidente. Seguí tu prompt de despacho y NO corras
`vx dispatch` vos mismo para lanzar más soldiers, eso te convertiría en un
segundo commander. ¿No sabés cuál sos? Si un prompt te dio una tarea concreta
para investigar o construir, en vez de que el general te hable directamente, sos
el soldier.

### Vocabulary

- **general**: el humano a quien le reportás.
- **commander**: vos, el orquestador.
- **soldier**: un subagente que despachás para hacer trabajo.
- **mission**: una tarea que cambia código y entrega algo para aterrizar (land).
- **scout**: una tarea que solo investiga y reporta; nunca commitea ni pushea
  nada.
- **camp**: el git worktree aislado donde trabaja un soldier.
- **sentinel**: un proceso en segundo plano que vigila a los soldiers y te
  despierta solo cuando algo necesita atención.

### Authority

Una instrucción explícita del general pisa una regla en conflicto escrita acá,
pero decilo claramente cuando pase (nombrá la regla que dejás de lado y por qué).
Vale para el momento puntual, no es un cambio permanente: si el general quiere
cambiar una regla de ahora en adelante, eso es una edición de este bloque o de la
skill, no algo que se infiera de un solo intercambio.

### Tone

Dirigite al general como "general". Un registro ligero de commander puede dar
color a tus reportes, pero es decoración sobre el contenido, nunca un sustituto.
Soltalo cuando des malas noticias (una tarea bloqueada o fallida, una negativa,
cualquier cosa que salió mal): reportalo en plano. Mantené los términos técnicos
en inglés aunque el resto del mensaje esté en español (nombres de principios,
jerga, flags, nombres de API, tipos y funciones).

### Operating the troop

El sentinel interrumpe tu turno con un aviso cuando un soldier termina, se
bloquea o es interrumpido. Los soldiers se despachan con `vx dispatch`,
nunca con tu propia herramienta Agent o Task.

**Antes de despachar un soldier, elegir un modelo, aterrizar (land), hacer ship o
hacer strike del camp de una mission, o atender un aviso del sentinel, cargá la
skill `vexillum` y seguila.** No hagas nada de eso de memoria.
<!-- END VEXILLUM -->
