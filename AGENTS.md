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

## Bitácora de sesiones (binnacle)

Cada decisión que se toma en una sesión de trabajo se guarda en la carpeta `binnacle/` del repo. Una decisión (lo que se resolvió, lo que se probó y se revirtió, lo que quedó pendiente) no vive solo en el chat: se registra en un archivo de bitácora.

**Nomenclatura del archivo:** `YYYYMMDD-tema-corto-con-guiones.md`
Ejemplo: `20260903-prompt-colors-claude-theme-vim.md`

**Estructura del archivo** (seguí este esqueleto):

```markdown
# YYYY-MM-DD - Título corto de la sesión

## Resuelto y en pie

- **Nombre de la decisión / tarea**: qué se decidió y quedó firme, con el detalle
  concreto (rutas, valores, comandos, enlaces). Suficiente para que el yo-futuro
  reconstruya el qué y el porqué sin releer el chat.

## Probado y revertido: <qué se intentó>

Contexto de la queja o el objetivo puntual.

1. **Hipótesis N (descartada): <resumen>.** Qué se asumió, qué se hizo, qué efecto
   tuvo (o no), y la conclusión. Incluí los enlaces de referencia consultados.
2. ...

**Decisión**: qué se abandonó o se cerró, y en qué estado quedó todo (qué se
revirtió, qué archivos quedaron como estaban).

## Pendiente para la próxima

- **Tarea**: qué queda por hacer y, si aplica, la pista concreta para retomarla
  (un enlace, un comando, un archivo a mirar) en vez de arrancar de cero.
```

No todas las secciones son obligatorias en cada archivo: si una sesión solo resolvió cosas, alcanza con "Resuelto y en pie". Si solo exploró sin cerrar, "Probado y revertido" y "Pendiente" bastan. Lo obligatorio es que **toda decisión de la sesión quede registrada** en un archivo de bitácora con la nomenclatura de arriba, para que el historial de por qué las cosas son como son viva en el repo y no en la memoria de un chat.

Distinción con el ADR: el ADR guarda decisiones de arquitectura estructurales y duraderas (por qué Go, por qué herdr). La binnacle guarda el registro cronológico de cada sesión de trabajo, incluidas las cosas chicas, los experimentos fallidos y los pendientes. Una decisión de arquitectura grande puede empezar en una entrada de binnacle y graduarse a un ADR.
