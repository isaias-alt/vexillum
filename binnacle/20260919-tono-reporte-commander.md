# 2026-09-19 - Tono de reporte del commander (feedback real de firstmate)

## Resuelto y en pie

- **El commander reportaba de más**: después de correr un soldier con
  éxito, agregaba de rutina el path del camp, el nombre de la rama, el
  task id, y el comando exacto de `release` - información operativa que
  el general no pidió. Causa: la sección "Dispatching a soldier" del
  `AGENTS.md` de `vexillum-prueba` se lo pedía explícitamente ("report
  back the result (status, and where to find the pane...)").

- **Corregido usando las notas de referencia del general sobre
  `firstmate`** (`~/Desktop/Ideaverse/03-Resources/software/agentic-workflow/`,
  especialmente `03-l8-principal-building-full-stack-app-agentic-engineering.md`:
  "El agente testea end-to-end solo y se autocorrige, avisa solo cuando
  necesita algo que solo el humano puede dar"). Se agregó una sección
  explícita "Report the outcome, not the plumbing": reportar como si
  fuera trabajo propio ("listo, creé X con Y"), sin exponer camp
  path/branch/task id/comando de release de rutina - esa info está
  disponible si el general la pide, no hace falta adelantarla. Solo traer
  a colación el aterrizaje/liberación de forma proactiva cuando algo
  realmente necesita juicio humano (ej. el soldier volvió `blocked`).

- **Aclarado, no es un bug**: que el camp/pane sigan abiertos después de
  que el soldier termina es comportamiento intencional (matchea el
  teardown de `firstmate`, que cierra el pane recién cuando el trabajo
  aterriza) - el general lo señaló como posible problema pero era la
  verbosidad del reporte lo que realmente molestaba, no que el pane
  siguiera vivo.

## Pendiente para la próxima

- Esta guía de tono vive solo en el `AGENTS.md` de `vexillum-prueba`
  (instancia de prueba). Cuando se defina cómo se ve el `AGENTS.md` de
  producto real (post-Capa 4, o cuando exista un CLI real de despacho),
  vale la pena llevar este mismo criterio de "reportar resultado, no
  plomería" al template real (`internal/cli/init.go:productAgentsMD`).
