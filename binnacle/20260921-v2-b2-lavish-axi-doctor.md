# 2026-09-21 - v2 B.2: lavish-axi, doctor reporta si el skill está instalado

## Resuelto y en pie

- **Investigación real antes de implementar, siguiendo el mismo criterio que B.1**: se verificó con WebFetch que `github.com/kunchenguid/lavish-axi` es un repo real (3.8k estrellas, MIT). Su propio README recomienda `npx skills add kunchenguid/lavish-axi --skill lavish`.

- **No era el mismo patrón que quota-axi, como se sospechaba al cerrar B.1**: dos diferencias reales, no cosméticas:
  - El valor de `--skill` es `lavish`, no `lavish-axi` - el nombre de carpeta del skill instalado (`.claude/skills/lavish/SKILL.md`) no coincide con el nombre del repo.
  - Su instalación recomendada **no lleva `-g`** (project-local por defecto), a diferencia de quota-axi que sí la recomienda.

- **`internal/cli/doctor.go`**: `axiSkill` ganó un campo `Global bool` para que cada AXI declare su propio comando de instalación recomendado en vez de asumir un patrón fijo. `axiStatusLine` arma el comando condicionalmente (agrega `-g` solo si `Global` es true). `knownAXIs` ahora tiene dos entradas:
  ```go
  {Name: "quota-axi", Repo: "kunchenguid/quota-axi", Global: true},
  {Name: "lavish", Repo: "kunchenguid/lavish-axi", Global: false},
  ```
  La detección (`axiSkillInstalled`) no cambió - sigue chequeando ambas ubicaciones (proyecto y global) sin importarle cuál, independientemente de cuál sea la recomendada.

- **4 tests nuevos** (`internal/cli/doctor_test.go`, casos B2-01/B2-02 más su cobertura implícita en el test existente de que el exit code no cambia): instalado a nivel proyecto (reportado como `lavish`, no `lavish-axi`), no instalado (comando exacto sin `-g`, con un assert explícito de que la línea NO lleva `-g`).

- **`docs/test-cases.md`** y **`docs/references.md`** actualizados con la investigación real y la corrección del comando de instalación (el que estaba en `references.md` era genérico y no reflejaba el flag `-g` ausente ni el nombre real del skill).

- Build, vet, gofmt y toda la suite con `-race` en verde.

## Pendiente para la próxima

- Verificación en vivo real de B.1 y B.2 juntas: se dejó pendiente a propósito para el final de la Parte B (decisión del general en esta sesión), no se hizo ahora.
- Sigue B.3 (chrome-devtools-axi) en la secuencia recomendada - a diferencia de B.1/B.2, esta sí toca el core (Capa 4: un browser vivo por soldier es estado que el re-dispatch de A.2 debe aprender a limpiar). Antes de implementar, investigar el repo real de chrome-devtools-axi con el mismo criterio (comando de instalación real, nombre de skill real, y además cómo se materializa el "browser vivo" dentro del camp de un soldier).
