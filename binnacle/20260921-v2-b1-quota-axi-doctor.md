# 2026-09-21 - v2 B.1: quota-axi, doctor reporta si el skill está instalado

## Resuelto y en pie

- **El PRD v2 cambió de modelo de integración de AXIs entre la sesión anterior y esta.** La versión que se había leído antes decía "la instrucción vive en el markdown del commander". La versión actual lo corrige: un AXI se distribuye como Agent Skill (formato agentskills.io), se instala con `npx skills add <owner>/<repo> --skill <nombre> -g`, el skill es un stub de discovery, y el CLI real se baja on-demand la primera vez que se usa (`npx -y <axi>`). El único rol nuevo del binario, dicho explícitamente: `doctor` verifica presencia del skill, nunca instala nada, y la ausencia es puramente informativa - ni siquiera al nivel de `warn` que ya tiene A.1.

- **Investigación real antes de implementar, no un supuesto**: se verificó con WebFetch que `github.com/kunchenguid/quota-axi` existe de verdad. Su propio README recomienda literalmente `npx skills add kunchenguid/quota-axi --skill quota-axi -g`. Se investigó también el mecanismo real de `npx skills add` (repo `vercel-labs/skills`): un skill instalado queda en disco como `<destino>/.claude/skills/<nombre>/SKILL.md` - `-g` lo pone en `~/.claude/skills/`, sin el flag va a `<proyecto>/.claude/skills/`. Sin manifest ni lockfile: la detección es un stat directo a ese archivo, igual que hace la propia CLI con `skills list`.

- **`internal/cli/doctor.go`**: nuevo tipo `axiSkill` (`Name`, `Repo`) y slice `knownAXIs` (por ahora solo `quota-axi`) - estructurado así porque el propio PRD dice que esta verificación "aplica a las cuatro integraciones" (B.1-B.4 son la misma pieza con otro nombre/repo), no una abstracción especulativa. Nueva sección de salida, separada de los checks de ambiente y **sin afectar nunca el exit code**:
  ```
  AXIs (on-demand, installed as Agent Skills - not required):
  [installed] quota-axi
  ```
  o `[not installed] quota-axi - install with: npx skills add kunchenguid/quota-axi --skill quota-axi -g`. Se chequean ambas ubicaciones (proyecto y global) - a `doctor` no le importa cuál, solo que el skill esté disponible.

- **`runDoctor` gana un parámetro `homeDir`** (el `$HOME` real, para `~/.claude/skills/` - distinto de `vexillumHome`, que es `~/.vexillum` y no tiene por qué coincidir con `$HOME` en los tests). Se actualizaron las ~11 llamadas existentes en `doctor_test.go` (mecánico, cada una con su propio `t.TempDir()` para `homeDir`); `TestDoctor_ReadOnly` ahora también snapshotea `homeDir` antes/después.

- **4 tests nuevos** (`internal/cli/doctor_test.go`, casos B1-01 a B1-04): instalado global, instalado a nivel proyecto, no instalado en ningún lado (con el comando exacto), y que el estado de AXIs nunca cambia el exit code ni el resumen "Environment ready". Nuevo helper `writeSkillFile(t, dir, name)` que crea el `SKILL.md` real que dejaría `npx skills add`.

- **`docs/test-cases.md`**: sección nueva "V2 - PARTE B" con B1-01 a B1-04, documentando también la investigación de la mecánica real de `npx skills add`.

- **`docs/references.md`**: la línea de `quota-axi` describía el modelo viejo ("Modo: commander por prompt" implicando instrucción en markdown, sin mencionar el mecanismo de skill). Corregida para reflejar la distribución real como Agent Skill, con el comando de instalación verificado y una nota de que ya está implementado en v2.

- Build, vet, gofmt y toda la suite con `-race` en verde.

## Pendiente para la próxima

- Verificación en vivo real: correr `vexillum doctor` en la máquina real sin el skill instalado (debería salir `[not installed]`), instalar de verdad con `npx skills add kunchenguid/quota-axi --skill quota-axi -g`, y confirmar que pasa a `[installed]` sin tocar el resto de la salida ni el exit code. No se hizo en esta sesión (agente en modo texto).
- Con B.1 cerrado, sigue B.2 (lavish-axi) en la secuencia recomendada del PRD - mismo patrón (`knownAXIs` gana una entrada más), pero conviene investigar su repo real antes de asumir que el mecanismo de instalación es idéntico al de quota-axi.
