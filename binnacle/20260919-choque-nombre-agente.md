# 2026-09-19 - Choque de nombre de agente en dispatch paralelo real

## Resuelto y en pie

- **Riesgo que ya habíamos anotado como aceptado ("si dos soldiers en
  paralelo generan slugs idénticos, el segundo agent start va a fallar")
  pasó de verdad**: el general lanzó dos soldiers en paralelo con prompts
  que empezaban igual (mismo slug `vx-<...>`), y el segundo falló al
  arrancar. Se reprodujo en vivo contra herdr real (dos tabs, mismo
  nombre) para confirmar el código exacto: `agent_name_taken`.

- **Arreglo**: `startAgent` (`internal/soldier/herdr_run.go`) ahora
  atrapa `agent_name_taken` (nuevo `herdr.IsNameTaken`) y reintenta una
  vez con un nombre desambiguado: `<candidato>-<6 primeros chars del task
  id>`. Ya no falla toda la corrida por una coincidencia de slug. El
  nombre que termina quedando vivo (candidato original o el desambiguado)
  se usa para el resto de la interacción (`AgentPrompt`, `AgentRead`) y se
  persiste en `Task.HerdrAgentName` - si cambió respecto al que se guardó
  antes de arrancar, se vuelve a guardar el `Task` con el nombre correcto.

- Mantiene la decisión de la ronda anterior (nombre sin sufijo por
  default, legible) - el sufijo aparece únicamente cuando hace falta de
  verdad, no de rutina.

- 1 test nuevo (`TestRunInHerdr_FallsBackOnNameCollision`) que verifica:
  se reintenta con el nombre correcto, `AgentPrompt` se dirige al nombre
  que realmente quedó vivo (no al candidato original), y el `Task`
  persistido refleja el nombre final. Caso concreto `L4-08` en
  `docs/test-cases.md`.

- Build, vet, gofmt y tests verdes (49 tests en total).

## Pendiente para la próxima

- No se probó en vivo con el binario real todavía (solo con el
  `fakeHerdr` de los tests) - falta que el general repita el dispatch
  paralelo con prompts parecidos para confirmarlo end-to-end.
- Si la SEGUNDA tentativa (con el nombre desambiguado) también choca
  (muy improbable, pero posible con 3+ soldiers de prompts casi
  idénticos), `startAgent` falla sin un segundo reintento - no se
  generalizó a un loop de sufijos crecientes. Aceptable por ahora dado el
  volumen de paralelismo actual (todavía no existe Capa 4 paso 3
  formalmente), a revisar si se vuelve un problema real.
