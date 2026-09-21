# 2026-09-20 - Distribución por brew/curl: repos, goreleaser, CI y install script

## Resuelto y en pie

- **Repo remoto creado**: `vexillum` no tenía `origin` hasta esta sesión.
  Se verificaron primero los 16 commits existentes (ninguno con
  `Co-Authored-By`, todos de "Lucas Casco <cascolucasisaias@gmail.com>") y,
  con confirmación explícita del general, se creó
  [isaias-alt/vexillum](https://github.com/isaias-alt/vexillum) como repo
  público y se pusheó `main`.

- **Tap de Homebrew separado, no fórmula en el mismo repo**: se investigó
  primero cómo lo resuelve un proyecto Go comparable real en el mismo
  dominio (`Gentleman-Programming/gentle-ai`, orquestador de agentes de
  código) en vez de asumir - mismo criterio que pide el AGENTS.md. Ese
  proyecto usa un tap separado (`Gentleman-Programming/homebrew-tap`, no
  `homebrew-<proyecto>`) con un token dedicado, dando el one-liner estándar
  `brew install owner/tap/formula`. Se descartó la alternativa más simple
  (fórmula en una carpeta `Formula/` del propio repo `vexillum`, sin repo
  ni token nuevo) una vez confirmado el patrón real - el general la pidió
  explícitamente después de ver la referencia. Se creó
  [isaias-alt/homebrew-tap](https://github.com/isaias-alt/homebrew-tap)
  público (compartido para futuros proyectos personales, no solo
  vexillum).

- **`brews` (fórmula), no `homebrew_casks`, a pesar del warning de
  deprecación de goreleaser**: se investigó con la documentación oficial de
  goreleaser antes de migrar sin más al reemplazo sugerido. Confirmado:
  `homebrew_casks` es exclusivamente para macOS (DMG, `xattr`, `launchctl`
  para desinstalar) - no da soporte Linux. Como el AGENTS.md exige
  multiplataforma (macOS/Linux al menos), se descartó la migración y se
  mantiene `brews` a propósito, con el warning aceptado como
  trade-off consciente. La misma referencia (`gentle-ai`) confirma esta
  misma decisión en su propio `.goreleaser.yaml`.

- **`.goreleaser.yaml`** (raíz del repo): build para `cmd/vexillum`,
  `darwin`/`linux` x `amd64`/`arm64`, `CGO_ENABLED=0`, ldflags inyectando
  `main.version`. Archivos `.tar.gz` (`vexillum_{version}_{os}_{arch}`),
  checksums sha256, fórmula de Homebrew publicada en
  `isaias-alt/homebrew-tap` vía `HOMEBREW_TAP_TOKEN`. Verificado en vivo
  con `goreleaser release --snapshot --clean` (instalado localmente vía
  `brew install goreleaser`, 2.18.2) - build de los 4 targets, archivos, y
  fórmula generada correctamente, con `on_macos`/`on_linux` separados por
  arquitectura, antes de tocar ningún workflow remoto.

- **`cmd/vexillum/main.go`**: agregado `-v`/`--version` (no existía ningún
  flag de versión). `var version = "dev"` inyectable por ldflags - sin eso,
  la fórmula/el binario liberado no podrían reportar su propia versión.
  Sin test nuevo (el paquete `main` no tenía tests, es solo el switch de
  despacho).

- **`.github/workflows/ci.yml`** (nuevo, no existía CI): build, vet,
  gofmt, y `go test ./... -race` en cada push a `main` y cada PR.

- **`.github/workflows/release.yml`** (nuevo): dispara con tags `v*`,
  corre `goreleaser release --clean` con `permissions: contents: write` y
  los dos tokens (`GITHUB_TOKEN` para el propio repo y release,
  `HOMEBREW_TAP_TOKEN` para el tap).

- **`scripts/install.sh`**: adaptado del patrón real de `gentle-ai`
  (detecta plataforma, prioriza `brew` si está instalado, si no baja el
  binario de GitHub Releases), acotado a lo que vexillum necesita hoy -
  sin canales beta/nightly, sin fallback a `go install` con path
  versionado, sin firma minisign (ninguno de esos aplica todavía a
  vexillum). Se mantuvieron las partes que sí son correctitud básica, no
  "de más": **verificación de checksum contra `checksums.txt`** (aborta si
  no coincide), instalación atómica (staging + `mv`), fallback a `sudo` si
  no hay permiso de escritura, y aviso si el directorio de instalación no
  queda en el `PATH`. Verificado en vivo contra un artefacto real generado
  por el snapshot de goreleaser (no solo revisado a ojo): checksum
  calculado a mano coincidió con el de `checksums.txt`, extracción y
  `chmod +x` funcionaron, y `vexillum --version` del binario extraído
  reportó la versión inyectada correctamente.

- **`README.md`** (nuevo, no existía): instrucciones de instalación por
  brew y por curl, y referencia rápida de comandos.

- **`isaias-alt/homebrew-tap` seedeado con un `README.md`**, mirando la
  estructura real de la referencia (`Gentleman-Programming/homebrew-tap`
  tiene `Casks/`, `Formula/`, y un `README.md` con instrucciones de tap +
  una sección por fórmula disponible). El repo estaba completamente vacío
  (`isEmpty: true`, sin default branch) - se clonó a un directorio
  temporal, se agregó el README con esa misma estructura (instalar por
  tap, por `owner/tap/formula` directo, actualizar, desinstalar) adaptado
  a tener una sola fórmula (`vexillum`) por ahora, y se pusheó a `main`.
  Queda como tap compartido: agregar una sección nueva ahí cuando exista
  otro proyecto personal con release por goreleaser.

- **`.gitignore`**: agregado `/dist` (carpeta de salida de goreleaser).

- Build, vet, gofmt y toda la suite en verde después de los cambios en
  `main.go`.

## Resuelto y en pie (continuación)

- **`HOMEBREW_TAP_TOKEN` cargado como secret de `isaias-alt/vexillum`**.
  Identificada la herramienta que el general mencionó como "Atomic
  Vault": es **Automic Vault** (`av`, instalado en `/usr/local/bin/av`,
  4.11.4) - encontrada investigando el repo real de `firstmate`
  (`docs/verification/dispatch-resolve.md` usa exactamente
  `av inject +TYPESAFE_API_KEY -- ...`), no adivinada. Confirmado con la
  doc oficial (automicvault.com/docs): `av save NOMBRE` guarda un valor en
  Keychain con prompt oculto; `av inject +NOMBRE <comando>` lo expone como
  variable de entorno solo para ese comando, nunca impreso. Se cargó con:
  `av inject +HOMEBREW_TAP_TOKEN sh -c 'printf %s "$HOMEBREW_TAP_TOKEN" |
  gh secret set HOMEBREW_TAP_TOKEN --repo isaias-alt/vexillum'` - el valor
  nunca pasó por la salida visible de ningún comando. Verificado con
  `gh secret list --repo isaias-alt/vexillum` (solo nombre y fecha, nunca
  el valor). PAT fine-grained, scope único `isaias-alt/homebrew-tap`,
  permiso `Contents: Read and write`, vencimiento acordado en 366 días -
  **rotar antes de ~2027-09-21**.

## Primer release real (v0.1.0), verificado en vivo de punta a punta

- **Commits pusheados a `main`** (`68cd7d0` con goreleaser/workflows/
  install.sh/README/`--version`, y luego `f125677` con el arreglo
  descrito abajo) - sin coautoría de agente, por pedido explícito del
  general (ver más abajo, `attribution.commit`).

- **Tag `v0.1.0` cortado y pusheado**, disparando `release.yml` de
  verdad contra GitHub Actions (no simulado). Resultado: release
  publicado con los 4 binarios + `checksums.txt`, y
  `isaias-alt/homebrew-tap/Formula/vexillum.rb` actualizado
  automáticamente por goreleaser con el `HOMEBREW_TAP_TOKEN` cargado
  antes - confirmado leyendo el contenido real del archivo en el tap
  después del run, no solo el log del workflow.

- **`brew install isaias-alt/tap/vexillum` verificado en vivo**, dos
  veces. La primera se topó con un conflicto real (ya había un symlink
  de desarrollo manual en `/opt/homebrew/bin/vexillum` de sesiones
  previas, documentado en la binnacle de "distribución pendiente" de
  antes) - resuelto con `brew link --overwrite`, no era un bug de esta
  sesión. `vexillum --version` del binario instalado por brew reportó
  `0.1.0` correctamente.

- **`scripts/install.sh` verificado en vivo contra el release real**,
  no solo contra el snapshot local. Primero se coló un falso positivo:
  un `curl | bash` con un PATH filtrado a mano para simular "sin brew"
  dejó sin querer un `:` final vacío, que bash interpreta como
  "directorio actual" - corrido parado en el propio repo de `vexillum`,
  encontró un binario de desarrollo viejo (`./vexillum`, compilado antes
  de agregar `--version` esta sesión) en vez del recién instalado.
  Identificado comparando contra `/opt/homebrew/bin/vexillum` corrido
  directo, que sí reportaba `0.1.0` bien. Se repitió la prueba
  correctamente: un directorio con symlinks a solo binarios del sistema
  (sin brew) más `env -i` con `PATH`/`HOME` explícitos, corrido desde un
  directorio neutral sin ningún `vexillum` local cerca.

  - **Bug real encontrado y arreglado** (`f125677`): `install_via_binary`
    registraba `trap 'rm -rf "$tmpdir"' EXIT` con `tmpdir` declarado
    `local` a la función - al retornar la función, esa variable deja de
    existir, y con `set -u` la salida real del script (después de que
    `main` ya terminó) revienta con "unbound variable". Efecto real: un
    `curl | bash` que instala todo bien (checksum verificado, binario
    instalado atómicamente, todo correcto) terminaba con exit code
    distinto de cero igual - cualquier caller que chequee `$?` vería un
    fallo falso en una instalación que en los hechos funcionó perfecto.
    Arreglado con expansión segura `${tmpdir:-}` (mismo patrón que ya
    usa la referencia de `gentle-ai`, que sí lo tenía bien). Reverificado
    en el mismo entorno aislado después del fix: `exit=0` limpio,
    checksum verificado, binario instalado en `~/.local/bin/vexillum`
    (fallback correcto porque `/usr/local/bin` no era escribible en ese
    entorno), y `vexillum --version` del binario recién instalado
    reportó `0.1.0`. Pusheado a `main` aparte - el script se sirve
    siempre desde `main` vía `raw.githubusercontent.com`, no desde el
    tag, así que no hizo falta cortar un nuevo release para que aplique.

- **`attribution.commit`**: el general pidió que ningún commit de esta
  sesión tuviera coautoría de agente. El `CLAUDE.md` global ya lo decía,
  pero un system-reminder de sesión contradictorio lo estaba pisando.
  Investigado el mecanismo real (no asumido): existe `attribution.commit`
  (string; `""` oculta la atribución) en el schema real de `settings.json`
  de Claude Code - confirmado contra el schema completo vía el skill
  `update-config`, no solo contra lo que había devuelto un subagente
  antes (que el propio harness marcó como posible alucinación,
  "instruction-shaped pattern", y que por eso no se usó sin verificar).
  Se agregó `"attribution": {"commit": ""}` en
  `~/github/isaias-alt/dotfiles/home/.claude/settings.json` (no directo
  en `~/.claude/settings.json`, que es un symlink de home-manager al
  store de Nix generado a partir de ese archivo fuente) - pendiente que
  el general corra su rebuild de home-manager para que tome efecto.

- **Todos los artefactos de prueba de esta sesión limpiados**: el
  binario de desarrollo viejo en la raíz del repo (`./vexillum`,
  gitignorado, pre-`--version`), el directorio de PATH mínimo usado para
  simular "sin brew", el `brew tap`/`brew install` real hecho para
  probar (desinstalado y destapeado al terminar) - nada residual queda
  en el sistema del general por estas pruebas.

## Pendiente para la próxima

- **Sin LICENSE**: el repo quedó público sin archivo de licencia. No se
  agregó a criterio propio por ser una decisión legal del general, no
  técnica - queda pendiente que la defina (afecta además qué puede incluir
  la fórmula de Homebrew, que típicamente declara `license` explícitamente
  como hace `gentle-ai` con `license: "MIT"`).
- El general todavía no corrió su rebuild de home-manager para que
  `attribution.commit: ""` tome efecto de verdad en `~/.claude/settings.json`.
