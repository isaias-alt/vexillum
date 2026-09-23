# 2026-09-23 - ADR-10: Lieutenant cerrado

## Resuelto y en pie

- **ADR-10 agregado a `docs/adr.md`**, después de ADR-09. Cierra lo que
  ADR-09 dejaba abierto ("se reabre después de la Capa 4"): el lieutenant
  (segundo commander, local o remoto) queda cerrado, no diferido a v3.
  Razón registrada: el cuello de botella real del uso personal es la
  cuota del plan de Claude, no la capacidad de despachar/supervisar
  agentes - un segundo commander consume de la misma cuota y no agrega
  capacidad. El camino remoto además exige proyectos solo-ship (sin land
  local desde la máquina remota) y una durabilidad de endpoint (sesión
  GUI viva, o credenciales en keychain) más cara de lo que ADR-09 había
  estimado. Se reabre solo ante un límite distinto de la cuota - no
  porque la v2 (o cualquier parte) ya esté cerrada, que era la condición
  que ADR-09 dejaba planteada.

- **`docs/prd-v2.md`, sección "Features diferidas (post-v2)"** actualizada:
  la entrada de Lieutenant ya no dice "A v3 / reabrir cuando la Parte B
  esté cerrada" (esa condición ya se cumplió y aun así no se reabre) -
  apunta a ADR-10 como la decisión vigente. El diseño de referencia de
  firstmate (secondmate, jerárquico, sin reconciliar estado entre pares)
  se deja anotado como "sigue vigente si se reabre", no se borra.

- **Fuera de alcance de esta entrada, dejado como estaba a propósito**:
  la sección "Restricción heredada por v3" (línea ~148 de `docs/prd-v2.md`)
  es el registro histórico de una restricción que ya se cumplió durante
  el trabajo de v2 (no tomar decisiones de formato de `Task` que forzaran
  una migración si el lieutenant llegaba a construirse) - reescribirla
  para reflejar el cierre de ADR-10 sería reescribir historia ya cerrada,
  no corregir un puntero vivo. Solo se tocó la sección de diferidas, que
  es la que efectivamente apunta hacia adelante.

- Sin cambios de código en esta tarea - build/vet/gofmt/test siguen en
  verde porque no hay `.go` tocado.
