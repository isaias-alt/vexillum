# 2026-09-19 - Vuelta a `--dangerously-skip-permissions` para el soldier

## Resuelto y en pie

- **Se revierte la decisión tomada al arrancar la Capa 4** ("el soldier
  corre con permisos normales, el sentinel escala un bloqueo"). Motivo:
  en uso real (dos soldiers en paralelo, uno de ellos con fricción real de
  git), la fricción no vino de los soldiers bloqueados por permisos -
  vino de la propia sesión del commander pidiendo reconfirmación. Pero el
  argumento de fondo que dio el general es más general: sin sentinel
  (paso 2, todavía no existe), un soldier `blocked` por un permiso queda
  sin que nadie se entere hasta que alguien vaya a mirar su pane a mano -
  exactamente lo que se supone que evita tener un solo commander como
  punto de contacto.

- **El control de seguridad real pasa a ser `camp.Land`** (implementado
  esta misma sesión, antes de esta reversión): nada de lo que hace un
  soldier llega a la historia real del proyecto sin aprobación explícita
  del general al aterrizar. El worktree aislado acota mientras tanto el
  daño de lo que corre sin supervisión en el camp. Esto es coherente con
  lo que el propio general describió de `firstmate`: autonomía del agente
  hasta un gate de review (no-mistakes / merge aprobado), no aprobación
  por cada acción individual - "revisar cada PR a mano... no vale la pena,
  la IA escribe más rápido de lo que se puede revisar."

- **Cambio de interfaz**: `herdr.Client.AgentStart` gana un parámetro
  variádico `agentArgs ...string`, pasado tal cual al binario real vía
  `herdr agent start ... -- <agent-args>` (documentado en el skill file de
  herdr: "Pass native agent arguments only after `--`"). `soldier`
  siempre pasa `--dangerously-skip-permissions` ahí
  (`herdrAgentArg` constante nueva en `internal/soldier/herdr_run.go`).

- **`state.StatusBlocked` no se eliminó**: sigue siendo un resultado
  posible si Claude Code hace una pregunta genuina (no de permisos) - solo
  que ahora es la excepción, no la norma. `mapAgentStatus` no cambió.

- **Documentación actualizada**: `docs/prd-v1.md` (Capa 4) y
  `docs/test-cases.md` (intro del paso 1) reflejan la decisión final con
  el razonamiento completo, no solo el estado actual - para que quien lea
  esto después entienda por qué se fue y volvió, no solo dónde quedó.
  `AGENTS.md` de `vexillum-prueba` actualizado para explicar por qué
  `--dangerously-skip-permissions` es deliberado, no un descuido.

- 1 test nuevo (verifica que `AgentStart` se llama con
  `--dangerously-skip-permissions`), tests existentes actualizados a la
  nueva firma. Build, vet, gofmt y tests verdes (49 tests).

## Pendiente para la próxima

- Falta la prueba en vivo con el binario real actualizado - el general
  todavía no corrió un soldier con este cambio aplicado.
- Los camps/panes que quedaron de las pruebas de esta sesión (con la
  fecha de WWW, el rebase pendiente, etc.) siguen sin resolver - quedaron
  en medio del problema que motivó esta reversión. Vale la pena
  limpiarlos en la próxima sesión antes de seguir probando cosas nuevas.
