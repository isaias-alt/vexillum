# PRD - Vexillum v1

Orquestador de agentes de código por CLI. Binario en Go, instalable por `brew` o `curl`, multiplataforma. Uso personal primero, con la puerta abierta a hacerlo público.

Este PRD cubre la **v1** y está seccionado por capas de construcción. La Capa 1 está a nivel construible (arrancás por acá). Las Capas 2 a 4 están a nivel de intención: qué hacen, por qué y en qué orden, para detallarlas al llegar a cada una. Al final, las features diferidas.

---

## Glosario del producto

| Concepto | Término (código / CLI) |
|---|---|
| Binario / comando | `vexillum` |
| Orquestador (le hablás vos) | commander |
| Subagentes ejecutores | soldiers |
| El humano (vos) | general |
| Tarea que cambia código y entrega PR | mission |
| Tarea que solo investiga y deja reporte | scout |
| Worktree aislado de cada tarea | camp |
| Componente de supervisión (watcher) | sentinel |
| Directorio de estado / config | `~/.vexillum/` |

Un solo vocabulario, en inglés, para código y para la voz del agente. Sin tabla de traducción: el agente detecta en qué idioma le escribís y responde en ese idioma, pero los términos del dominio (commander, soldier, mission) no se traducen.

Cadena de mando: **general** (vos) → **commander** (orquestador) → **soldiers** (ejecutores). El **sentinel** vigila a los soldiers y despierta al commander solo cuando hace falta.

---

## Contexto y decisiones marco

Estas decisiones están fijadas y no se relitigan dentro de la v1. El detalle del porqué vive en el ADR.

- **Lenguaje:** Go. Primer proyecto en Go del autor (viene de front-end / Node). Elegido por distribución (binario estático, brew/curl, multiplataforma) y por ser fuerte en procesos y concurrencia.
- **Empaquetado:** Opción A, binario instalable. La herramienta scaffoldea estado y config en el proyecto/host; no es un repo que se clona.
- **Harness:** Claude Code, único, en toda la v1. Pi diferido.
- **Backend de sesión:** herdr como primario; tmux implementable en paralelo como backend de control/comparación.
- **Inteligencia del commander:** vive en un AGENTS.md editable (markdown) que Claude Code interpreta. El binario Go hace plomería determinista (worktrees, procesos, estado, consultar al sentinel), no razona.
- **Generación de código:** vía Claude Code, aceptado. En las capas de concurrencia, no se acepta código que el autor no pueda leer y explicar.

---

## Método de construcción por capas

Cada capa es usable y testeable sola. No se pasa a la siguiente sin la anterior probada. El orden va de lo determinista-y-solo a lo concurrente-y-difícil.

1. **Capa 1 - CLI base.** `init` + `doctor`. Cero concurrencia, cero procesos.
2. **Capa 2 - Estado en disco.** Structs serializados a JSON, leídos/escritos a mano. Sin workers.
3. **Capa 3 - Un proceso.** Lanzar un solo soldier en un camp, esperar, capturar resultado. Secuencial.
4. **Capa 4 - Concurrencia.** N soldiers en paralelo, cada uno en su camp, con el sentinel consultando la API de herdr.

---

## CAPA 1 - CLI base (construible)

### Objetivo

Un binario `vexillum` que no orquesta nada todavía. Solo dos comandos: uno que prepara un proyecto para usar vexillum, y otro que reporta si el entorno está listo. Sin procesos ni concurrencia.

### Alcance

Entra: el binario, el comando `init`, el comando `doctor`, la escritura del scaffold, la lectura del entorno.

No entra: lanzar agentes, worktrees, sesiones, supervisión, estado de tareas, cualquier cosa concurrente.

### Comando `vexillum init`

Prepara el directorio actual (un proyecto del general) para ser orquestado por vexillum.

Qué hace:
- Crea el directorio de estado `~/.vexillum/` si no existe.
- Escribe en el proyecto el **AGENTS.md de producto**: el markdown que define el comportamiento del commander, que Claude Code leerá cuando (en capas futuras) se lance la orquestación. En la Capa 1 este archivo se escribe pero todavía nadie lo ejecuta.
- Crea la estructura mínima de config del proyecto (por ejemplo un `.vexillum/` local con un archivo de configuración; la forma exacta se decide al implementar).
- Es idempotente: correrlo dos veces no rompe ni duplica. Si el scaffold ya existe, informa y no sobrescribe sin aviso.

Qué NO hace en Capa 1: no lanza ningún agente, no valida que Claude Code funcione (eso es `doctor`), no crea worktrees.

### Comando `vexillum doctor`

Reporte de salud del entorno, de solo lectura. No modifica nada.

Qué chequea y reporta:
- Si el binario de Claude Code está instalado y accesible en el PATH.
- Si herdr está instalado y accesible.
- Si tmux está instalado y accesible (backend de control).
- Si `~/.vexillum/` existe y es escribible.
- Si el proyecto actual está inicializado (existe el scaffold de `init`).
- Si el directorio actual es un repo git (requisito para las capas de worktrees, aunque acá solo se reporta).

Formato: salida clara, una línea por chequeo, con estado ok/faltante. Un resumen final de si el entorno está listo o qué falta.

### Criterios de aceptación

Los criterios detallados de prueba viven en el documento de casos de prueba de la Capa 1. A alto nivel: `init` es idempotente y deja el scaffold correcto; `doctor` reporta con precisión el estado real del entorno sin modificar nada; ambos comandos fallan con mensajes claros cuando algo no está.

---

## CAPA 2 - Estado en disco (intención)

Modelar en Go la representación de una tarea (mission o scout) como structs serializables a JSON, con funciones para leer y escribir ese estado en `~/.vexillum/`. Todavía sin workers ni concurrencia: el estado se crea y se lee a mano, invocado por el autor. Es la base del restart-proof: el estado de cada tarea vive en disco para poder reconciliarse tras un reinicio.

---

## CAPA 3 - Un proceso (intención)

Lanzar UN solo soldier: spawnear una instancia de Claude Code en un camp (git worktree aislado), esperar a que termine, capturar el resultado. Un worker, secuencial, sin flota. Acá aparece el manejo de procesos en Go (`os/exec`) y la creación/teardown de un worktree. Al ser un solo proceso, un fallo se aísla con precisión.

---

## CAPA 4 - Concurrencia (intención)

El corazón y el pico de dificultad. Varios soldiers en paralelo, cada uno en su camp, coordinados por el commander. El sentinel implementa la supervisión event-driven zero-token: en vez de sondear a ciegas, consulta la socket API de herdr para saber qué soldier está bloqueado o terminó, y despierta al commander solo cuando hace falta. Restart-proof completo: matar la sesión y reconciliar el estado de dominio desde disco al reiniciar (herdr restaura el layout visual pero no el proceso ni el estado de tarea; esa mitad la persiste vexillum).

Las dos formas de tarea (mission y scout) se materializan en esta capa: mission entrega cambios de código (un PR), scout deja un reporte de investigación.

---

## Features diferidas (post-v1)

No entran en la v1. Se reabren cuando exista un commander único sólido (fin de Capa 4).

- **Lieutenant (orquestador secundario persistente), local y remoto por SSH.** Diferido por dependencia (necesita el commander único terminado; es "un segundo commander") y por dificultad (coordinación entre dos commanders y estado compartido/reconciliado, que Go no simplifica; es concurrencia sobre concurrencia). Go sí facilita el transporte remoto (binario estático que viaja por scp y corre sin dependencias), pero ese es el problema chico. Reabrir después de Capa 4.
- **Pi como segundo harness.** Requiere una interfaz `Harness` que abstraiga sobre los mecanismos de supervisión distintos (Stop hook en Claude, watcher extension en Pi). En la v1, con Claude único, la supervisión es un solo camino; la costura se deja preparada pero no se construye la abstracción.
- **tmux como backend primario.** En la v1 herdr es primario y tmux queda como backend de control. Promover tmux a opción de primer nivel es post-v1.
- **Chequeo de nombre de paquete** en el índice de distribución que se use, si se abre al público (más allá de brew + release por curl).
