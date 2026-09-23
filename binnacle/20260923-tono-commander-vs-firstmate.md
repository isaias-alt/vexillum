# 2026-09-23 - Tono del commander, comparado con firstmate

## Resuelto y en pie

- **Contexto**: comparación del `AGENTS.md` real de `kunchenguid/firstmate`
  (fetched de GitHub) contra `productVexillumRule` (`internal/cli/init.go`).
  Cinco diferencias de tono identificadas; el general pidió aplicar tres.

- **Tres cambios aplicados a `productVexillumRule`** (mismo archivo que
  `vexillum init` scaffoldea a `.claude/rules/vexillum.md`, verificado
  renderizando en un proyecto temporal con el binario reconstruido):

  1. **Identidad comprometida desde la primera línea**: el párrafo de
     apertura pasó de describir el mecanismo en tercera persona ("These
     rules govern how the vexillum commander behaves...") a abrir con
     "You are the commander. The general is the human you report to.",
     al estilo de firstmate ("You are the first mate. The user is the
     captain.").

  2. **Sección nueva "Authority"**: una sola declaración general de que
     una instrucción explícita del general pisa una regla en pie del
     archivo - pero avisando (nombrando qué regla se deja de lado y por
     qué), nunca en silencio. Reemplaza el patrón anterior de repetir esta
     idea localmente en land/ship sin nunca declararla una vez arriba.
     Distinto de la versión de firstmate ("captain instruction overrides
     any conflicting standing rule", sin exigir aviso) - el general pidió
     explícitamente la variante con aviso.

  3. **Sección nueva "Tone"**: registro opcional y decorativo ("general"
     como forma de dirigirse, un tono levemente "commander") sobre el
     reporte, nunca en vez del contenido - se cae solo ante malas noticias
     (blocked/failed/refusal), mismo gatillo que el "aye"/"shipshape" de
     firstmate (decorativo, limitado al chat, descartado para malas
     noticias). Cross-referenciado con una frase corta en "Report the
     outcome, not the plumbing" (sección "Dispatching a soldier") para no
     duplicar la regla en dos lugares.

  **Tensión con ADR-06, nombrada antes de aplicar**: ADR-06 no solo pide
  vocabulario del dominio sin traducir - da una razón puntual ("no hace
  falta que el agente hable español para que se sienta romano") que
  argumenta específicamente en contra de decoración en español. El pedido
  del general (punto 3) le da la vuelta a esa razón sin romper la letra
  del ADR (los términos `commander`/`soldier`/`camp` siguen sin traducir,
  el flavor va en lo que el commander dice, no en la prosa de las reglas
  - que se mantiene en inglés). Correspondía nombrarlo y no implementarlo
  sin que el general lo pidiera explícitamente - se pidió, se aplicó.

- **Verificado**: `go build`/`go vet`/`go test ./...` en verde después del
  cambio; render confirmado en un proyecto git temporal con el binario
  reconstruido al scratchpad de la sesión (no se tocó
  `/opt/homebrew/bin/vexillum` ni `vexillum-prueba` para este cambio -
  no hay plumbing nuevo que probar en vivo, es prosa).

## Pendiente para la próxima

- El "Tone" nuevo (registro opcional) todavía no se vio en uso real -
  nadie disparó un `vexillum dispatch`/reporte con este `.claude/rules/vexillum.md`
  puesto de por medio. La primera vez que el general vea al commander
  reportar con este flavor (o sin él, ante una mala noticia) vale la pena
  confirmar si el criterio de "malas noticias" quedó bien calibrado.
- Sigue sin tocarse `/opt/homebrew/bin/vexillum` (ver pendiente de la
  sesión anterior, mismo motivo: separación binario personal/dev para el
  próximo tag).
