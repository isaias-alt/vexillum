# 2026-09-19 - Andamiaje inicial de Capa 1 (CLI base)

## Resuelto y en pie

- **Toolchain Go instalado vía Nix**: el entorno no tenía Go. Se instaló con
  `nix profile install nixpkgs#go` (Determinate Nix, flakes habilitados) en
  vez de Homebrew, por preferencia del general. Verificado con
  `go version` -> `go1.26.6 darwin/arm64`.

- **Librería de CLI: estándar (`os.Args` + `switch`), no Cobra.** Con solo dos
  subcomandos placeholder, Cobra es una dependencia externa que no aporta
  nada todavía. La estándar alcanza y mantiene el binario en cero
  dependencias, consistente con no adelantar máquina de capas futuras
  (AGENTS.md). Se puede reevaluar más adelante si `init`/`doctor` crecen
  mucho en flags propios; no está descartada para siempre, solo no se
  justifica ahora.

- **Módulo Go inicializado**: `github.com/isaias-alt/vexillum` (`go mod
  init`), sin entradas en `require` (cero dependencias externas).

- **Estructura de directorios**:
  ```
  cmd/vexillum/main.go       # entrypoint: parsea os.Args, resuelve --help, despacha a init/doctor
  internal/cli/init.go       # func Init(args []string) int -> placeholder
  internal/cli/doctor.go     # func Doctor(args []string) int -> placeholder
  ```
  `cmd/vexillum/` sigue la convención estándar de Go para el entrypoint de un
  binario (deja lugar a más de un binario en el repo sin reestructurar).
  `internal/cli/` es `internal` porque nada fuera del módulo debe importar
  estas funciones. Se separó `init.go` de `doctor.go` en archivos propios
  porque el PRD indica que cada uno va a crecer con lógica propia bastante
  distinta (scaffold vs. chequeos de entorno); es organización, no lógica
  adelantada, cada función sigue devolviendo el mismo placeholder
  `"<comando>: not implemented yet"` con exit code 0.

- **Comportamiento de CLI ya cableado** (sin lógica de negocio de
  `init`/`doctor`):
  - `vexillum` (sin args) imprime el usage general y sale con exit 0.
  - `vexillum --help` / `vexillum -h` imprime el mismo usage, exit 0.
  - `vexillum init` / `vexillum doctor` imprimen su placeholder, exit 0.
  - `vexillum init --help` / `vexillum doctor --help` imprimen el usage
    propio de cada subcomando, exit 0 (adelanta el caso de prueba L1-15,
    que pide help por comando, sin adelantar lógica de negocio).
  - Comando desconocido (`vexillum finish-my-taxes`): mensaje de error a
    stderr + sugerencia de `--help`, exit code 1 (adelanta L1-14).

- **Verificación manual**: se compiló con `go build -o vexillum
  ./cmd/vexillum` y se corrieron los cuatro casos pedidos (`vexillum`,
  `--help`, `init`, `doctor`), todos con el resultado esperado. También
  `go vet ./...` y `gofmt -l .` sin problemas.

- **Explícitamente NO hecho en este paso** (a pedido, para no adelantar
  capa): no se implementó lógica real de `init` ni `doctor`; no se tocó
  `~/.vexillum/`; no se escribió el AGENTS.md de producto que `init`
  scaffoldeará; no se escribieron tests (los casos de
  `docs/test-cases.md` quedan para cuando se implemente la lógica real).

## Pendiente para la próxima

- **Repo git de vexillum**: el propio repo de vexillum (no el de un
  proyecto que vexillum orqueste) todavía no es un repo git. No bloqueó
  este paso, pero conviene decidirlo antes de empezar a implementar
  `init`/`doctor` de verdad, para poder versionar los cambios.
- **Implementar `vexillum init`**: seguir `docs/prd-v1.md` (sección Capa 1)
  y los casos `L1-01` a `L1-06` de `docs/test-cases.md`.
- **Implementar `vexillum doctor`**: seguir el mismo PRD y los casos
  `L1-07` a `L1-13`.
- **Escribir tests** con `testing` estándar para los casos de
  `docs/test-cases.md` una vez exista la lógica real.
