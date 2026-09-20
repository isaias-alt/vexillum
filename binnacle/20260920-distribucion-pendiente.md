# 2026-09-20 - Distribución (brew/curl): pendiente, no arrancada

## Pendiente para la próxima

- **La distribución real no está construida.** El PRD la marca como parte del alcance central de v1 ("Binario en Go, instalable por `brew` o `curl`, multiplataforma", `docs/prd-v1.md` línea 3, y la decisión de lenguaje en "Contexto y decisiones marco": "Elegido por distribución (binario estático, brew/curl, multiplataforma)"). Hoy no existe:
  - Ninguna fórmula de Homebrew (`.rb`).
  - Ningún script de instalación por curl.
  - Ningún workflow de release en `.github/workflows/` (no hay ni siquiera CI todavía).
  - El uso actual es 100% de desarrollo: `go build -o vexillum ./cmd/vexillum` en el repo raíz, con un symlink manual en `/opt/homebrew/bin/vexillum` que hay que recordar mantener apuntado al binario recién compilado. Funciona para el uso personal de esta sesión, pero no es la distribución real que describe el PRD.
- Con Capa 1 a 4 completas, testeadas y verificadas en vivo (ver binnacles de esta misma fecha), este queda como el próximo bloque de trabajo real cuando se retome - a propósito diferido para otra sesión, no arrancado hoy.
- Alcance a definir cuando se retome: al menos una fórmula de Homebrew (tap propio, dado que `vexillum` es un proyecto personal, no en homebrew-core) y un script de instalación por curl equivalente al patrón típico (`curl ... | sh`), multiplataforma (macOS/Linux al menos, dado que Go compila fácil para ambos) - sin asumir nada más sin investigar primero cómo lo resuelven proyectos Go comparables, mismo criterio que el resto de esta sesión.
