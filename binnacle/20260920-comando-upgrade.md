# 2026-09-20 - Comando `vexillum upgrade`

## Resuelto y en pie

- **`vexillum upgrade`** (`internal/cli/upgrade.go`, wireado en
  `cmd/vexillum/main.go`): actualiza un proyecto ya inicializado a lo
  último que trae el binario (plantilla de `AGENTS.md`/`CLAUDE.md`, hook
  `Stop` del sentinel) sin necesidad de borrar `.git`/`.vexillum` y volver
  a correr `init` de cero. Pedido explícito del general: "es molesto"
  tener que resetear todo el proyecto de prueba cada vez que la plantilla
  cambiaba.

- **Mecanismo de decisión (evita pisar ediciones reales del general)**:
  `localConfig` (`.vexillum/config.json`) ahora guarda `agents_md_hash` y
  `claude_md_hash` - el sha256 hex del contenido que `vexillum` mismo
  escribió la última vez que tocó ese archivo. `vexillum upgrade` compara
  el contenido actual del archivo contra ese hash guardado:
  - si el archivo no existe: lo crea con la plantilla actual.
  - si coincide con la plantilla actual: no hace nada, reporta "already
    up to date".
  - si coincide con el hash guardado pero no con la plantilla actual
    (nadie lo tocó desde que `vexillum` lo escribió, pero la plantilla
    cambió): lo actualiza a la plantilla actual y refresca el hash
    guardado.
  - si no hay hash guardado (proyecto de antes de esta funcionalidad) o
    el contenido no coincide con el hash guardado (el general lo editó a
    mano): **no lo toca**, reporta "has local changes, left untouched".
  Este último caso es intencionalmente conservador: preferimos que un
  proyecto viejo (sin hash) necesite un empujón manual una vez, a
  arriesgarnos a pisar una edición real de un `AGENTS.md` de producción en
  el futuro. Los hashes se graban en `internal/cli/init.go` tanto en el
  flujo de `init` fresco como en su rama de sanado (`recordScaffoldHashes`),
  así que todo proyecto inicializado desde ahora en más queda cubierto
  para `upgrade` sin ningún paso extra.
  - **Nota para el proyecto de prueba actual** (`vexillum-prueba`): fue
    inicializado con un binario anterior a esta funcionalidad, así que no
    tiene hash guardado todavía. Si su `AGENTS.md`/`CLAUDE.md` en disco ya
    coincide byte a byte con la plantilla actual, `upgrade` lo reportará
    "already up to date" sin problema. Si no coincide, `upgrade` lo dejará
    intacto (caso "unknown provenance") - hace falta correr `vexillum
    init` una vez más ahí (no hace falta borrar nada, `init` ya sanea sin
    destruir) para que quede con hash grabado y `upgrade` funcione a pleno
    de ahí en adelante.

- Siempre corre `ensureSentinelHook` (mismo merge cuidadoso que ya usaba
  `init`, no duplica ni pisa hooks existentes) - eso sí es 100% seguro de
  re-correr siempre, sin necesidad de tracking de hash.

- **7 tests nuevos** en `internal/cli/upgrade_test.go`: rechazo en
  proyecto no inicializado, refresco de scaffold intacto, no pisar
  archivo editado a mano, tratamiento conservador de "unknown
  provenance", recreación de archivo borrado, idempotencia en una segunda
  corrida, agregado del hook faltante. Build, vet, gofmt y todos los
  tests del repo en verde.

- **Verificado en vivo** (no solo con tests) en un proyecto scratch real:
  `init` fresco → `upgrade` (no-op, ambos "already up to date") → se borró
  `CLAUDE.md` y `.claude/`, se editó `AGENTS.md` a mano → `upgrade` recreó
  `CLAUDE.md`, re-agregó el hook, y dejó `AGENTS.md` intacto reportando
  "has local changes, left untouched" - exactamente el comportamiento
  esperado.

## Pendiente para la próxima

Según el orden que ya había fijado el general en
`20260920-sentinel-bug-race-y-upgrade-pendiente.md` (ítem 1 de esa lista
queda resuelto acá):

1. **Arreglar el parseo de argumentos de `vexillum sentinel`**: cualquier
   `args[0]` no reconocido (ej. `status`) hoy cae al comportamiento por
   defecto y arranca el loop infinito en vez de fallar con un error de
   uso. Confirmar además si el proceso accidental (pid 12336 en el
   reporte anterior) sigue vivo y matarlo si corresponde.
2. **Repensar el diseño del sentinel** dado el bug de carrera ya
   documentado (dispatch síncrono le gana la carrera al polling del
   sentinel en el camino feliz). Necesita su propia sesión de diseño, no
   un parche rápido - no arrancar sin volver a alinear con el general.
- Repo con cambios sin commitear de esta sesión (comando `upgrade`
  completo) - falta que el general revise el mensaje de commit antes de
  commitear, como siempre.
