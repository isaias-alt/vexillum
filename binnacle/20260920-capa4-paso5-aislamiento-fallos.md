# 2026-09-20 - Capa 4 paso 5: aislamiento de fallos, formalizado

## Resuelto y en pie

- **Capa 4 queda completa**: con este paso, los 5 pasos en los que se dividió (soldier real en un pane, sentinel, N en paralelo, restart-proof, aislamiento de fallos) están implementados, testeados y verificados en vivo - el único pendiente real de fondo que queda es la resumption de una tarea `interrupted` (relanzar un agente en el mismo camp/rama), deliberadamente fuera de alcance por ahora.

- **Test formal nuevo**: `TestTick_OneFailingTaskDoesNotAffectItsSiblings` (`internal/sentinel`) - tres tareas en un mismo `Tick`: una se asienta a `done`, otra tiene su agente confirmado desaparecido (pasa a `interrupted`), otra sigue genuinamente `running`. Confirma que cada una termina en el estado que le corresponde sin que ninguna interfiera con las otras (ni en el conteo de wakes, ni en el estado persistido). Requirió extender `fakeHerdr` con un mapa de error por nombre de agente (`errFor`), ya que antes solo soportaba un error "global" para todas las llamadas.

- **Verificado en vivo, combinando las dos técnicas ya probadas por separado** (paralelismo real de `vexillum dispatch` + cierre de pane a propósito para simular una desaparición real): 3 missions despachadas con concurrencia genuina (bash `&`, no secuencial) contra el mismo proyecto scratch. Colisionaron de nombre las tres (mismo prefijo de prompt) - se desambiguaron bien, confirmando de paso que el fix de `disambiguatedName` sigue funcionando con 3 colisiones simultáneas, no solo 2. Se cerró el pane de la del medio apenas volvió su propio `vexillum dispatch` (sin esperar a que las otras terminaran, para no darle tiempo a asentarse sola - el primer intento sí le dio ese tiempo sin querer, y no probó nada real).
  - Resultado, con timing exacto: la interrumpida marcó `AgentNotFoundSince`, y pasó a `interrupted` **10 segundos después**, igual que en L4-22. Sus archivos (`stack.py`, `test_stack.py`) quedaron escritos en el camp pero sin comitear - el trabajo físico no se perdió, solo falta el commit.
  - Las otras dos (`queue`, `set`) siguieron su curso normal, sin ninguna demora ni interferencia: una a los 5s, la otra a los 25s.
  - `vexillum land`/`release` de las dos sanas y el intento de `release` de la interrumpida (rechazado correctamente por cambios sin comitear) confirmaron que cada camp se evalúa de forma completamente independiente - ninguna falla de un hermano contamina el land/release de los demás.
  - Todos los artefactos de la prueba (repo scratch, 3 camps, tasks, wakes, tabs de herdr) limpiados al terminar.

- `docs/test-cases.md`: L4-29 agregado con el detalle completo; la sección "Pendiente" de Capa 4 se colapsó a una sola nota real (resumption) más la decisión ya tomada de quedarse con polling - los pasos 3, 4 y 5 ya no aparecen como pendientes.

- Build, vet, gofmt y toda la suite en verde con `-race`.

## Pendiente para la próxima

- **Resumption real de una tarea `interrupted`** sigue siendo el único hueco de fondo de Capa 4 - relanzar un agente en el mismo camp/rama, retomando el trabajo donde quedó. No se tocó esta sesión, alcance deliberadamente fuera.
- Repo con cambios sin commitear de esta sesión (test de aislamiento de fallos + docs) - falta que el general revise el mensaje de commit.
