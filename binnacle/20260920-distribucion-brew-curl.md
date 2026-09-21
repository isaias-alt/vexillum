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

## Pendiente para la próxima
- **Ningún release real todavía**: nunca se corrió el workflow contra
  GitHub Actions de verdad (solo `--snapshot` local). Falta cortar el
  primer tag (`v0.1.0` o similar) una vez cargado el token, y ahí sí
  verificar en vivo el camino completo como lo haría un usuario real:
  `brew install isaias-alt/tap/vexillum` desde cero, y
  `curl ... | bash` desde cero, en un entorno limpio - no alcanza con la
  verificación local ya hecha del script contra el snapshot.
- **Sin LICENSE**: el repo quedó público sin archivo de licencia. No se
  agregó a criterio propio por ser una decisión legal del general, no
  técnica - queda pendiente que la defina (afecta además qué puede incluir
  la fórmula de Homebrew, que típicamente declara `license` explícitamente
  como hace `gentle-ai` con `license: "MIT"`).
- Repo con cambios sin commitear de esta sesión (goreleaser, workflows,
  install.sh, README, version flag) - falta que el general revise el
  mensaje de commit.
