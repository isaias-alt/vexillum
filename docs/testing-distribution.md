# Cómo probar la distribución (brew/curl) de un release

Este documento explica cómo se verificó en vivo el mecanismo de distribución (`binnacle/20260920-distribucion-brew-curl.md` tiene el detalle completo de esa sesión) y cómo repetir esa verificación vos mismo en cualquier release futuro. La regla de este proyecto es "reproducir en un entorno E2E lo más parecido posible a como lo vive un usuario real" - probar solo `goreleaser --snapshot` en local no alcanza, porque no toca ni GitHub Releases ni el tap real.

## Qué hay que verificar en cada release

1. El workflow de `release.yml` terminó en verde en GitHub Actions.
2. El GitHub Release tiene los 5 assets esperados (`checksums.txt` + 4 tarballs `darwin`/`linux` × `amd64`/`arm64`).
3. `isaias-alt/homebrew-tap/Formula/vexillum.rb` se actualizó con la versión correcta.
4. `brew install isaias-alt/tap/vexillum` funciona de punta a punta y el binario resultante corre.
5. `curl ... | bash` (el script de `scripts/install.sh`) funciona de punta a punta, en las dos ramas que tiene: con brew instalado y sin brew (descarga directa + verificación de checksum).

## Cómo lo probé la primera vez (v0.1.0)

### 1. Release real, no simulado

```sh
git tag -a v0.1.1 -m "mensaje"
git push origin v0.1.1
gh run list --limit 3                      # confirmar que "Release" arrancó
gh run watch <run-id> --exit-status        # esperar a que termine, en verde
```

### 2. Confirmar el release y la fórmula

```sh
gh release view v0.1.1 --repo isaias-alt/vexillum --json assets --jq '.assets[].name'
gh api repos/isaias-alt/homebrew-tap/contents/Formula/vexillum.rb --jq '.content' | base64 -d
```

Chequear a ojo que la fórmula tenga la versión nueva, los 4 `url`/`sha256` (`on_macos`/`on_linux`), y `license "MIT"`.

### 3. Probar `brew install` real

```sh
brew tap isaias-alt/tap
brew install isaias-alt/tap/vexillum
vexillum --version
```

**Cuidado real encontrado:** si ya tenés un binario de desarrollo symlinkeado a mano en `/opt/homebrew/bin/vexillum` (de compilar localmente con `go build -o vexillum ./cmd/vexillum` durante el desarrollo), `brew install` va a fallar el link con "already exists". No es un bug del release - es el residuo esperado del flujo de desarrollo. Se resuelve con:

```sh
brew link --overwrite vexillum
```

Al terminar de probar, para no dejar nada instalado de más:

```sh
brew uninstall vexillum
brew untap isaias-alt/tap
```

### 4. Probar el script de curl, rama "con brew"

```sh
curl -fsSL https://raw.githubusercontent.com/isaias-alt/vexillum/main/scripts/install.sh | bash
```

Si tenés brew instalado (lo normal), esto solo ejercita la rama `install_via_brew` del script - no la de descarga directa. Para probar la otra rama hace falta el paso 5.

### 5. Probar el script de curl, rama "sin brew" (la que tiene la lógica más delicada: checksum, instalación atómica)

Ocultar brew del `PATH` con un simple `PATH` recortado **no alcanza** y puede generar falsos positivos. La forma que funcionó:

```sh
# Un directorio con symlinks solo a los binarios del sistema que hacen falta
# (bash, curl, tar, shasum/sha256sum, grep, awk, sed, mktemp, uname, id,
# mkdir, mv, rm, printf, chmod, install, sudo, cat, head, tail) - sin
# ninguna ruta de Homebrew.
mkdir -p /tmp/no-brew-path
for cmd in bash curl tar sha256sum shasum grep awk sed mktemp uname id mkdir mv rm printf chmod install cat sudo head tail; do
  p=$(command -v "$cmd")
  [ -n "$p" ] && ln -sf "$p" /tmp/no-brew-path/"$cmd"
done

# Correr desde un directorio NEUTRAL (nunca el propio repo de vexillum:
# si hay un binario de desarrollo viejo ahí, un PATH mal armado puede
# terminar ejecutando ese en vez del recién instalado, sin que nadie lo
# note - así se coló un falso positivo la primera vez).
mkdir -p /tmp/install-test && cd /tmp/install-test

env -i PATH=/tmp/no-brew-path HOME="$HOME" \
  curl -fsSL https://raw.githubusercontent.com/isaias-alt/vexillum/main/scripts/install.sh -o install.sh

env -i PATH=/tmp/no-brew-path HOME="$HOME" bash install.sh
```

`env -i` arranca un entorno completamente limpio (sin heredar nada del shell interactivo) y `PATH`/`HOME` explícitos son las únicas variables que entran. Así se confirma que el script funciona con las herramientas mínimas reales, no con lo que casualmente haya en tu shell de todos los días.

Verificar el resultado:

```sh
$HOME/.local/bin/vexillum --version   # o /usr/local/bin, según a dónde haya instalado
```

Limpiar después:

```sh
rm -f "$HOME/.local/bin/vexillum"
rm -rf /tmp/no-brew-path /tmp/install-test
```

## Bug real que esta prueba encontró (y cómo se detectó)

La primera corrida de la rama "sin brew" reportó `vexillum: unknown command "--version"` al final, pero el resto del script (descarga, checksum, instalación) había funcionado bien. La sospecha inmediata (¿el binario del release no soporta `--version`?) se descartó corriendo el binario ya instalado por brew directamente (`/opt/homebrew/bin/vexillum --version` - funcionaba perfecto). Eso aisló el problema al **entorno de la prueba**, no al binario: el filtro de `PATH` usado la primera vez dejaba un `:` final vacío (que bash interpreta como "directorio actual"), y como la prueba corría parada en el propio repo de `vexillum`, encontró un binario de desarrollo viejo ahí (`./vexillum`, compilado antes de agregar `--version` esa sesión).

Una vez corregido el método de prueba (directorio neutral, sin PATH mal armado), apareció un bug real y reproducible: el script terminaba con `unbound variable` en la línea del `trap` de limpieza, aunque la instalación ya hubiera funcionado - `trap 'rm -rf "$tmpdir"' EXIT` referenciaba una variable `local` que dejaba de existir al salir de la función, y `set -u` lo convertía en un error real al cerrar el script. Efecto práctico: un `curl | bash` que instala todo bien terminaba con exit code distinto de cero de todos modos. Arreglado con `${tmpdir:-}` (expansión que no rompe si la variable ya no existe).

**Lección para la próxima vez que algo falle en una prueba así:** antes de asumir que el bug está en el release, correr el binario ya instalado directamente (sin pasar por el script) para descartar el entorno de prueba como causa.

## Checklist mínimo para un release futuro

No hace falta repetir la investigación forense completa cada vez - solo si algo falla. Para un release de rutina, alcanza con los pasos 1 a 4 de arriba (release real + brew install + curl con brew instalado). La prueba "sin brew" (paso 5) vale la pena repetirla completa si se toca `scripts/install.sh`, no en cada release de código del binario en sí.
