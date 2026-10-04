package main

import (
	"fmt"

	"github.com/isaias-alt/vexillum/internal/cli"
)

// commandES is the Spanish text of one command's reference page: its
// one-line summary and its full help. Everything that is a literal stays in
// English exactly as the binary prints it (command names, flags, synopsis
// lines, placeholders, quoted output, file names, JSON), and the domain
// vocabulary (commander, soldier, mission, scout, camp, sentinel) is never
// translated. Only the prose around those literals is Spanish, in the voseo
// register of the hand-written Spanish docs.
//
// Summary must open Usage's first paragraph, the same rule the English
// registry follows, so the two cannot drift apart.
type commandES struct {
	Summary string
	Usage   string
}

// commandsES is keyed by command name. The tests in cli_es_test.go fail
// when a command in internal/cli has no entry here, when an entry has no
// command, and when a translation drops a flag or another literal of the
// English help, so a new command or flag cannot ship untranslated.
var commandsES = map[string]commandES{
	"init": {
		Summary: "Preparar el proyecto actual para que vexillum lo orqueste",
		Usage: `Preparar el proyecto actual para que vexillum lo orqueste.

Uso:
  vx init [--yes] [--lang en|es] [--skills | --no-skills]

init le da al agente del proyecto las reglas del commander y las herramientas
para usar bien vexillum. Antes de escribir nada lista los archivos tuyos que va
a cambiar y pide consentimiento (Continue? [Y/n]):

  AGENTS.md              recibe un bloque administrado por vexillum con el
                         núcleo del commander, siempre activo
  CLAUDE.md              recibe la línea @AGENTS.md para que Claude Code vea
                         ese bloque (se crea si no existe; uno existente se
                         edita solo después de una segunda pregunta)
  .claude/settings.json  recibe el hook Stop del sentinel, un comando de una
                         línea que encuentra vx sin depender del PATH del
                         shell del hook

Cuando el proyecto no tiene AGENTS.md, init propone poner el bloque en
CLAUDE.md en lugar de crear un AGENTS.md (es la respuesta por defecto;
CLAUDE.md se crea si no existe, y entonces no necesita la línea @AGENTS.md).
Respondé no para crear AGENTS.md como se describió arriba. Un bloque que ya
vive en CLAUDE.md se queda ahí.

El bloque se escribe en el idioma que detecta en el archivo que lo contiene
(inglés cuando no puede saberlo); vos lo confirmás o lo cambiás. Después init
pregunta "Install the vexillum, forum and muster skills?" y, si aceptás, las
escribe en .claude/skills/. También escribe los archivos propios de vexillum
bajo .vexillum/ (config.json, models.json, .gitignore); un models.json
existente nunca se sobrescribe. Volver a correr init no cambia nada de lo que
ya está en su lugar.

Sin una terminal (scripts, CI) init no pregunta nada y no hace nada a menos que
se pase --yes; sin él, imprime lo que cambiaría y sale con un error.

Flags:
  --yes, -y       Acepta todas las preguntas, incluidas poner el bloque en
                  CLAUDE.md cuando no hay AGENTS.md, editar un CLAUDE.md
                  existente e instalar las skills.
  --lang en|es    Idioma del bloque, en lugar de detectarlo.
  --skills        Instala las skills sin preguntar.
  --no-skills     No instala las skills, sin preguntar.
`,
	},

	"upgrade": {
		Summary: "Actualizar vx a la última release y después refrescar con ella el scaffold de vexillum del proyecto",
		Usage: `Actualizar vx a la última release y después refrescar con ella el scaffold de vexillum del proyecto.

Uso:
  vx upgrade [--channel stable|canary] [--check] [--scaffold-only] [--force] [--yes] [--lang en|es] [--skills | --no-skills]

upgrade trabaja en dos pasos.

1. El binario. Mira cómo se instaló vx:
   - Homebrew: corre 'brew upgrade vexillum'.
   - El script de instalación (una descarga directa): descarga el archivo de
     la release para tu sistema operativo y arquitectura desde GitHub
     Releases, verifica su checksum sha256 contra el checksums.txt de la
     release y reemplaza el ejecutable de forma atómica, conservando su modo
     de archivo. Un checksum que no coincide, o un directorio donde no podés
     escribir, detiene el upgrade sin tocar el binario.
   - Cualquier otra cosa (un build desde el código fuente, un binario
     renombrado): se niega y te dice cómo reinstalar. 'vx upgrade
     --scaffold-only' sigue funcionando.
   Cuando vx ya está en la última release no se reemplaza nada, y nunca baja
   de versión. Un sentinel que esté corriendo nota el binario reemplazado y se
   retira; el siguiente comando que necesite uno lo arranca desde el binario
   nuevo.

2. El scaffold. Se arranca el binario nuevo para refrescar los archivos del
   proyecto, así se aplican sus propias plantillas. Lista los archivos tuyos que
   va a cambiar y pide consentimiento primero, igual que 'vx init'. Ver su
   ayuda. Después:

  - escribe o actualiza el bloque de vexillum en el archivo que lo contiene
    (AGENTS.md, o CLAUDE.md cuando el bloque vive ahí), y se asegura de que
    CLAUDE.md importe AGENTS.md cuando el bloque está en AGENTS.md. Un bloque
    que editaste no se sobrescribe: upgrade imprime el diff entre el tuyo y la
    plantilla actual. Un bloque malformado (marcadores desbalanceados o
    duplicados) no se toca hasta que confirmes las reparaciones que lista; el
    archivo se guarda antes como .vexillum/<file>.backup.
  - refresca las skills instaladas que no cambiaron desde que vexillum las
    escribió (se guarda un hash del contenido por skill en
    .vexillum/config.json) e informa las que editaste. Cuando todavía no hay
    ninguna skill instalada ofrece instalarlas, como hace init.
  - mantiene al día el hook Stop del sentinel, y crea .vexillum/models.json si
    falta. Un models.json existente nunca se sobrescribe, y tus propios hooks
    se conservan.

Fuera de un proyecto inicializado solo se actualiza el binario. Sin una
terminal el paso del scaffold no pregunta nada y no hace nada a menos que se
pase --yes.

Flags:
  --channel stable|canary
                  Canal de releases a seguir (por defecto stable). stable es la
                  última release completa; canary es el último pre-release
                  cuyo tag contiene -canary. Una instalación con Homebrew solo
                  sigue stable: para instalar un build canary usá en su lugar
                  el --channel canary del script de instalación.
  --check         Imprime la versión disponible y no cambia nada.
  --scaffold-only
                  Refresca el scaffold y deja el binario como está.
  --yes, -y       Acepta todas las preguntas, incluidas las reparaciones de un
                  bloque malformado (antes se hace un backup del archivo).
  --force         Sobrescribe lo que editaste: el bloque de vexillum, una skill
                  editada. Antes se guarda un backup
                  (.vexillum/agents-md-slot.backup.md para el bloque,
                  .vexillum/backups/skills/<name>/ para una skill).
  --lang en|es    Idioma del bloque, en lugar de conservar el idioma actual del
                  bloque o detectarlo.
  --skills        Instala también las skills que faltan.
  --no-skills     Deja las skills como están: no instala ni refresca.
`,
	},

	"doctor": {
		Summary: "Informar sobre la salud del entorno de vexillum",
		Usage: `Informar sobre la salud del entorno de vexillum. Solo lectura.

Además de las herramientas que maneja vexillum, para un proyecto inicializado
doctor informa el estado del bloque de vexillum en AGENTS.md o CLAUDE.md
(absent, current, stale, drifted o malformed), si CLAUDE.md importa AGENTS.md
cuando el bloque está en AGENTS.md, cada skill first-party (instalada y al día,
stale, editada a mano o faltante), si los perfiles de modelo de models.json son
válidos, y si el modo yolo está activado (el commander aterriza una mission
terminada sin preguntar).

También revisa el hook Stop del sentinel, el que despierta al commander cuando
un soldier termina: si está registrado en .claude/settings.json, si su comando
encuentra un binario vx al correr desde un entorno vacío (solo HOME y
PATH=/usr/bin:/bin, como lo tiene el shell de un hook que nunca leyó tu
perfil), y si hay un sentinel corriendo. Un hook que no puede encontrar vx es
una advertencia que nombra la solución; nunca hace fallar a doctor.

La línea del sentinel advierte, y nunca falla, cuando hay más de un proceso
sentinel vivo (competirían por las mismas tareas), cuando el sentinel vivo es
de una versión distinta a la de este binario, o cuando el binario desde el que
arrancó fue reemplazado en disco desde entonces (un rebuild informa la misma
versión, "dev"). Un sentinel anterior al seguimiento de versiones se informa
de la misma manera. Cada advertencia nombra el pid que hay que detener; el
siguiente vx dispatch, prompt o await del hook Stop arranca entonces uno al
día.

Uso:
  vx doctor
`,
	},

	"dispatch": {
		Summary: "Despachar un soldier (mission o scout) a un camp aislado",
		Usage: `Despachar un soldier (mission o scout) a un camp aislado.

Uso:
  vx dispatch <prompt> [--kind mission|scout] [--profile <name>] [--model <model>] [--effort <level>]

Una mission cambia código y entrega algo para aterrizar; un scout solo
investiga y reporta (por defecto: mission).

--model y --effort se pasan tal cual al CLI real de claude (--model: haiku,
sonnet, opus, fable; --effort: low, medium, high, xhigh, max). vexillum solo
valida que el valor sea uno que claude acepta: nunca elige uno por vos.

--profile <name> busca el modelo y el effort en la tabla de perfiles ('vx
models' la lista: <project>/.vexillum/models.json sobre el
~/.vexillum/models.json opcional sobre los valores por defecto incluidos). Un
--model o --effort explícito sigue ganándole al perfil. Un perfil desconocido
es un error que lista los válidos; "default" selecciona la entrada por defecto
de la tabla. Qué perfil conviene a una tarea lo decidís vos. Ver la skill
vexillum.

Corre una sesión real e interactiva de Claude Code en un pane de herdr, dentro
de un git worktree nuevo, aislado del árbol de trabajo propio de este proyecto,
con --dangerously-skip-permissions: el soldier tiene acceso total al host bajo
el usuario del sistema operativo que invoca (sin contenedor, chroot ni otro
sandbox); el worktree solo acota dónde caen sus commits, no lo que puede leer,
escribir o exfiltrar en otro lugar de la máquina. Nada llega al historial real
del proyecto hasta que 'vx land' se aprueba explícitamente; nunca despaches
contra un prompt, un repositorio o una máquina donde leer estado sensible del
host sería un problema. Requiere HERDR_WORKSPACE_ID: corré esto desde un pane
administrado por herdr.

Vuelve rápido: solo espera una breve sonda de arranque, no la tarea
completa del soldier. Un prompt trivial puede terminar dentro de esa ventana e
informar su resultado de inmediato; cualquier otro se deja corriendo e inicia
automáticamente un sentinel (si no hay uno ya vigilando este proyecto) para
registrar su estado final. Ver 'vx sentinel'.
`,
	},

	"models": {
		Summary: "Listar los perfiles de modelo y effort que puede usar un dispatch",
		Usage: `Listar los perfiles de modelo y effort que puede usar un dispatch. Solo lectura.

Uso:
  vx models

Imprime la tabla de perfiles combinada: los valores por defecto incluidos,
sobrescritos por el ~/.vexillum/models.json global opcional, sobrescritos por
el .vexillum/models.json de este proyecto. Cada perfil muestra su modelo y su
effort y una línea "when" que dice cuándo se aplica; al final se listan la
entrada por defecto de la tabla y los archivos que se leyeron. Pasá un perfil
a 'vx dispatch --profile <name>'.

Esto es una tabla de consulta, no un router: qué perfil conviene a una tarea es
un juicio del commander, hecho a partir de las líneas "when". Un archivo de
modelos que no es JSON válido, o que nombra un modelo o un effort que claude
no acepta, es un error que nombra el archivo y el campo.
`,
	},

	"yolo": {
		Summary: "Activar o desactivar el modo yolo para este proyecto, o imprimir si está activado",
		Usage: `Activar o desactivar el modo yolo para este proyecto, o imprimir si está activado.

Uso:
  vx yolo [on|off|status]

Con yolo activado, el commander aterriza una mission terminada y verificada de
inmediato con 'vx land' y libera su camp en el mismo turno, en lugar de
preguntarle primero al general. Viene desactivado por defecto, y sin argumento
el comando imprime el estado actual igual que 'status'.

'status' imprime "on" u "off" y sale con 0. Una configuración ausente es off.
Un .vexillum/yolo.json que no es válido es un error, nunca un "off" silencioso.

Yolo nunca cubre 'vx ship' (abrir un pull request real sigue siendo decisión
del general), el push, un scout, ni un land que se rechaza o diverge: eso
siempre se le informa al general. Tampoco descarta trabajo jamás.

La configuración se guarda en .vexillum/yolo.json y se commitea con el
proyecto, como models.json. 'vx land' rechaza un checkout sucio, así que
commiteá el archivo después de cambiarlo. Activarlo requiere un proyecto
preparado con 'vx init'.
`,
	},

	"redispatch": {
		Summary: "Volver a despachar una tarea interrumpida desde su prompt original",
		Usage: `Volver a despachar una tarea interrumpida desde su prompt original.

Uso:
  vx redispatch <task-id>

Solo se puede volver a despachar una tarea con status "interrupted". Esto es un
re-dispatch, no una reanudación: el camp sucio de la tarea (el árbol de trabajo
y los commits que nunca aterrizó) se descarta, se crea un camp nuevo y la
mission se relanza desde su prompt original como si se acabara de despachar:
no se recupera nada del trabajo parcial ni de la sesión de agente del soldier
muerto. Requiere HERDR_WORKSPACE_ID: corré esto desde un pane administrado por
herdr.
`,
	},

	"decide": {
		Summary: "Responder la pregunta abierta de una tarea blocked para que pueda continuar",
		Usage: `Responder la pregunta abierta de una tarea blocked para que pueda continuar.

Uso:
  vx decide <task-id> <answer>
  vx decide <task-id> --dismiss

Solo se puede responder una tarea con status "blocked". Entrega <answer> al
pane de herdr todavía abierto del soldier y lo registra contra la decisión
estructurada de la tarea, no solo como más prosa en la transcripción. Esa
decisión es el campo "decision" de 'vx status --json'. Si la pregunta abierta vino del selector
AskUserQuestion de Claude Code, <answer> (el texto exacto de una opción, o su
número renderizado, contando desde 1) se entrega como una única pulsación de
tecla, el mismo mecanismo que usaría una persona al elegir en ese menú, salvo
que se resuelva a "Type something.", que recurre a texto plano como todas las
demás decisiones.

Una vez entregada la respuesta, también se registra en la tarea como una
enmienda (campo "amendments" en 'vx status --json'), junto con la pregunta que
respondió, para que la revisión del tribunal de una mission la cuente como
parte de la intención de la mission. La elección de un selector se registra
como el texto de la opción. --dismiss no registra nada.

Si el soldier termina rápido, el status de la tarea se actualiza de inmediato:
running si el soldier sigue trabajando, blocked otra vez si hace otra
pregunta, done o failed si terminó tan rápido. Si no, la tarea queda running
para que el sentinel registre el resultado final, igual que un dispatch
nuevo.

--dismiss limpia una tarea que se marcó blocked por error (el soldier nunca
estuvo preguntando nada) sin enviarle nada: ni un prompt ni una pulsación de
tecla. La tarea pasa a ser lo que su pane dice que es: running si el soldier
sigue trabajando, done (o unconfirmed) si terminó, interrupted si el pane ya
no existe. Se rechaza si el pane está realmente bloqueado por una pregunta. La
pregunta descartada queda en la tarea, marcada como dismissed, y no la vuelve
a bloquear; 'vx status --json' la muestra. Usalo cuando 'vx prompt' o 'vx ship'
rechacen una tarea por estar blocked y no haya nada que responder.

Si la tarea en realidad está "interrupted" (su pane de herdr ya no existe;
vexillum solo todavía no lo había notado), esto la marca interrupted y se
rechaza: ya no queda nada contra qué responder. Usá 'vx redispatch' en su
lugar.
`,
	},

	"prompt": {
		Summary: "Enviar un prompt de seguimiento a un soldier que ya terminó",
		Usage: `Enviar un prompt de seguimiento a un soldier que ya terminó.

Uso:
  vx prompt <task-id> <text>

Usalo en lugar de enviarle el prompt al pane del soldier a mano (herdr agent
prompt): vuelve la tarea a running antes de entregar el prompt y se asegura de
que un sentinel esté vigilando, así el próximo fin del soldier se
registra y vuelve a despertar al commander. Un re-prompt que lo saltea puede
terminar sin ningún aviso, porque el sentinel solo informa una tarea que sale
de "running".

Solo se puede enviar un prompt a una tarea con status "done", "shipped" o
"unconfirmed" cuyo camp no haya sido liberado. El soldier de una tarea shipped
sigue abierto, esperando que se haga merge de su pull request: enviarle un
prompt pide trabajo de seguimiento. La tarea pasa a running mientras el
soldier trabaja y vuelve como done, no shipped, ya que los commits nuevos
todavía no están en el pull request; corré 'vx ship <task-id>' de nuevo para
empujarlos al mismo pull request. Una tarea "blocked" se responde con 'vx
decide' (o, si se marcó blocked por error, se limpia con 'vx decide <task-id>
--dismiss'), una "interrupted" necesita 'vx redispatch', y una "running" ya
está trabajando: esperá a que termine.

<text> se entrega al soldier tal cual. También se registra en la tarea como
una enmienda, con una marca de tiempo y su comando de origen, una vez
entregado; las enmiendas son el campo "amendments" de 'vx status --json'. La
revisión del tribunal de una mission juzga el cambio contra el prompt del dispatch seguido de estas
instrucciones posteriores, así que lo que pediste acá no se informa como no
solicitado. El prompt del dispatch en sí nunca se modifica. Las enmiendas las
escriben solo este comando y 'vx decide', en el archivo de estado de la tarea,
nunca en un archivo del camp. Cada una se limita a 2000 caracteres y una tarea
conserva las 20 más recientes.

Si el soldier termina rápido, el status de la tarea se actualiza de inmediato:
running si el soldier sigue trabajando, blocked si hace una pregunta, done si
terminó tan rápido. Si no, la tarea queda running para que el sentinel
registre el resultado final, igual que un dispatch nuevo.
`,
	},

	"pending": {
		Summary: "Registrar y limpiar las decisiones pendientes del propio commander",
		Usage: `Registrar y limpiar las decisiones pendientes del propio commander.

Son cosas que esperan la aprobación del general (despachar una mission ya
diseñada, aterrizar una terminada, hacer ship de un PR) y que, si no, vivirían
solo en la cabeza del commander, perdidas si la sesión se corta.

Uso:
  vx pending add <text> [--option <label>]... [--recommend <n>]
  vx pending list [--json]
  vx pending clear <id>

add registra <text> como una nueva decisión pendiente e imprime su id. Dale
las respuestas entre las que el general puede elegir con un --option <label>
por respuesta, en orden, y marcá la que recomendás con --recommend <n> (índice
desde 1 dentro de las opciones; necesita al menos un --option). Poné "--" antes
de un texto que empiece con "--". clear elimina una cuando el general ya
decidió. list las imprime de la más antigua a la más nueva, con las opciones
numeradas y la recomendada marcada.

Esta es una categoría distinta de una tarea blocked: allá un soldier necesita
al general y 'vx decide' la responde; acá es el commander quien necesita al
general. Una
mission done que espera 'vx land' no necesita ninguna entrada, la skill /muster
lo deriva de git; usá esto para decisiones que no existen en ningún otro lado.

Los items persisten como <project root>/pending/<id>.json, escritos de forma
atómica, así que sobreviven a una sesión cortada. --json imprime un único
objeto JSON:

  {
    "schema_version": 1,
    "generated_at": "2026-09-24T12:00:00Z",
    "pending": [
      {"schema_version": 1, "id": "1a2b3c4d", "text": "...",
       "options": ["...", "..."], "recommended": 1, "created_at": "..."}
    ]
  }

"options" y "recommended" se omiten en un item registrado sin ellos. La skill
/muster lee esto para mostrar cada item como una tarjeta en su sección
"Decisions for you".
`,
	},

	"status": {
		Summary: "Informar sobre la tropa: cada mission y scout del proyecto actual",
		Usage: `Informar sobre la tropa: cada mission y scout del proyecto actual, en el
estado en que esté. Solo lectura.

Uso:
  vx status [--json]

Sin --json, imprime una línea por tarea (la actualizada más recientemente
primero).

--json imprime en cambio un único objeto JSON:

  {
    "schema_version": 3,
    "generated_at": "2026-09-24T12:00:00Z",
    "project_root": "/abs/path/to/this project's ~/.vexillum namespace",
    "tasks": [ ... ]
  }

"tasks" es exactamente el registro de tarea que el propio vexillum persiste en
<project root>/tasks/*.json: este comando no le agrega nada, así que un
consumidor (por ejemplo la skill /muster) lee la misma forma
con la que razona el propio vexillum, nunca una reinterpretación aparte. El
agrupamiento, el filtrado y los juicios (qué cuenta como "needs attention",
qué está en la sesión actual, el enriquecimiento con PRs de GitHub) se dejan a
ese consumidor, no se deciden acá.

Una tarea con status "blocked" lleva su pregunta abierta en su campo
"decision" (question, options, asked_at y, una vez que corrió 'vx decide',
answer/answered_at), no solo la palabra "blocked". Sin --json, la pregunta de
una tarea blocked también se imprime en su propia línea con sangría.
`,
	},

	"land": {
		Summary: "Aterrizar el trabajo de una mission terminada en la rama base de este proyecto",
		Usage: `Aterrizar el trabajo de una mission terminada en la rama base de este proyecto.

Uso:
  vx land <task-id>

Avanza con fast-forward el propio checkout de este proyecto hasta la rama de la
mission. Se rechaza (sin tocar nada) a menos que este checkout esté limpio y el
merge sea un fast-forward limpio: nunca fuerza ni hace rebase de nada.

Una vez que ese merge fast-forward tiene éxito, land libera automáticamente el
camp de la mission de vuelta al pool y cierra también su pane de herdr: la
misma lógica de liberación que usa 'vx release', que solo limpia un camp que ya
está limpio y aterrizado, así que esto no relaja esa salvaguarda. Un merge que
se rechaza (checkout sucio, o rama divergida) nunca toca el camp. En el raro
caso de que el merge tenga éxito pero esa liberación automática falle después,
land informa ambos resultados con claridad (el merge NO se deshace) y deja el
camp para que 'vx release <task-id>' lo reintente a mano.

Para una tarea que ya se hizo ship por el pipeline de tribunal propio de
vexillum ('vx ship'), en cambio hace merge del pull request real en GitHub: el
PR, no la rama propia del camp, es la fuente de verdad una vez que el general
(o CI, o un revisor) pudo haber empujado más commits directamente a él en
GitHub. Requiere "gh". Se rechaza a menos que el pull request esté abierto, no
sea un draft, sea mergeable y todos los checks estén en verde; el merge queda
atado al head exacto que se acaba de verificar. Este camino nunca libera
automáticamente: la rama sigue en curso hasta que el general haga merge del PR
real.
`,
	},

	"ship": {
		Summary: "Hacer ship de una mission terminada por el pipeline de tribunal propio de vexillum, abriendo un pull request real",
		Usage: `Hacer ship de una mission terminada por el pipeline de tribunal propio de
vexillum, abriendo un pull request real.

Uso:
  vx ship <task-id> [--fix] [--max-rounds <n>] [--timeout <duration>]
                    [--title <text>] [--body <text> | --body-file <path>]

Corre, en orden, dentro del propio camp de la mission: lint, tests, una
revisión adversarial del diff y una verificación de la documentación,
deteniéndose en el primer paso que falla e informándolo, sin pushear ni abrir
nada. Cada paso corre de forma sincrónica; "vx ship" no vuelve hasta que todo
el pipeline terminó, no hay nada externo que seguir después.

La revisión corre en una sesión nueva de Claude Code que no tiene nada del
contexto del autor: lee el diff por sí misma con git, asume que el cambio está
mal e intenta romperlo, e informa hallazgos estructurados (archivo, línea,
severidad, acción, un escenario de falla concreto, sitios hermanos) más los
archivos que realmente leyó. Cuando la mission tiene un prompt, cada
componente que introdujo el cambio también se juzga contra él, seguido de las
instrucciones que el general dio después mediante 'vx prompt' y 'vx decide'
(registradas en la tarea como enmiendas), y todo lo que ninguno de ellos pidió
se informa como una advertencia cuyo remedio es quitarlo. Cualquier hallazgo
de severidad error o warning bloquea el ship (incluidos los que necesitan una
decisión humana); los hallazgos info no bloquean y se agregan al cuerpo del
pull request. Un archivo cambiado que el revisor no informó haber leído, una
respuesta que no es un JSON de hallazgos válido después de los reintentos, o
un timeout hacen fallar el paso, nunca pasar. Los hallazgos se imprimen en la
salida de este comando.

Por defecto una revisión que bloquea solo rechaza. Con --fix, los hallazgos
que el revisor marcó auto-fix se entregan a un fixer headless en el camp, su
trabajo se commitea, y lint, tests y la revisión corren de nuevo con un
revisor nuevo que trata los commits del fixer como código sin revisar. Los
hallazgos que necesitan una decisión humana (ask-user) nunca se corrigen: el
ship se rechaza con ellos. El ciclo está acotado por --max-rounds (rondas de
corrección, por defecto 2); agotarlo sin una revisión que pase rechaza el
ship.

  --fix                 habilita el ciclo revisión -> corrección -> revisión
  --max-rounds <n>      máximo de rondas de corrección con --fix (por defecto 2)
  --timeout <duration>  tope absoluto para cada ejecución del revisor o del
                        fixer, por ejemplo 30m (por defecto 20m); excederlo
                        hace fallar el paso

Una vez que todos los pasos pasan, pushea la rama del camp de la mission al
remoto real ("origin") y abre el pull request por sí mismo con "gh pr create",
de forma determinista, sin que ningún soldier ni agente decida si esto ocurre
ni cuándo, ya que es la única acción de vexillum con un efecto real e
irreversible fuera de la máquina. Requiere "gh" ('vx doctor' informa si está
instalado).

El pull request es público, y su texto se produce sin ningún paso manual. La
revisión adversarial, que lee todo el diff y los commits sin contexto del
autor, también propone un título al estilo conventional commit (de unos 72
caracteres) y una descripción corta y factual (qué cambió y por qué, de 3 a 8
líneas), escrita a partir del diff y nunca del prompt de la mission. vx ship
los usa como el título y la sección What. Cuando la revisión los omite o
devuelve unos inválidos, el título es el asunto del único commit de la rama,
o del commit feat, fix o docs más nuevo, o de su primer commit, y What son los
asuntos de los commits como lista de viñetas. El cuerpo también tiene el diff
stat y las áreas de primer nivel tocadas, una línea Verification (los pasos
del tribunal que pasaron, las rondas de revisión y las de corrección), los
hallazgos info de la revisión bajo Tribunal notes, y un pie de una línea que
nombra la mission. Todo lo que viene del camp o del revisor se sanea primero:
se descartan las líneas con una ruta absoluta del home, un puerto de localhost
o algo que parece un secreto, igual que cualquier línea que cite el prompt de
la mission; el texto del revisor que no pasa la verificación no se publica
cortado a medias, lo reemplaza el texto derivado.

Overrides opcionales, solo cuando el texto propuesto no es el que querés:

  --title <text>        usa este título para el pull request
  --body <text>         usa este texto como la sección What
  --body-file <path>    igual, leído desde un archivo ("-" lee la entrada
                        estándar); no se puede combinar con --body

Un cuerpo provisto se sanea como todo lo demás; el diff stat, Verification,
Tribunal notes y el pie se mantienen.

Una mission que ya hizo ship puede volver a hacerlo, para empujar commits de
seguimiento al mismo PR abierto: "done" y "shipped" son los status de partida
válidos, y un re-ship igual corre primero el pipeline de tribunal completo. El
soldier de una mission shipped queda abierto hasta que se hace merge del PR,
así que el trabajo de seguimiento se pide con 'vx prompt <task-id> <text>' (la
tarea pasa a running y después vuelve a done) y se entrega con 'vx ship
<task-id>' de nuevo. Siempre que ya exista un PR para la rama, ya sea que la
tarea esté done o shipped, ship lo reutiliza en lugar de abrir un segundo: pushea
a la misma rama y refresca el título y la descripción del PR a partir de la
nueva revisión (o de --title y --body). Un PR que ya tuvo merge o se cerró no
se reutiliza: ship se rechaza antes de correr nada.

Un soldier que hizo rebase o amend de una rama que ship ya pusheó hace que el
siguiente push no sea fast-forward. Después de cada push exitoso ship registra
en la tarea la punta pusheada. Cuando un push se rechaza por no ser
fast-forward, lee la punta de origin para la rama: si esa punta es exactamente
la registrada, la reescritura es trabajo del propio vexillum y ship reintenta
una vez con un lease fijado a ese commit ('git push
--force-with-lease=<branch>:<sha>', solo la rama de la mission, nunca la rama
base ni ninguna otra ref), imprimiendo una línea que dice que reescribió la
rama del PR y de qué commit a qué commit. Si la punta es cualquier otra cosa
(alguien más pusheó) o no hay un push registrado (una tarea de la que se hizo
ship antes de que esto se registrara), ship nunca fuerza: falla y dice qué
tiene origin y cómo decidir: 'git fetch origin <branch>' e inspeccionarla, o
autorizar un push manual con lease y volver a hacer ship. Una vez que se hizo
ship, aterrizá el PR con 'vx land <task-id>' en lugar de hacer 'vx land' del
camp localmente.
`,
	},

	"release": {
		Summary: "Liberar el camp de un soldier de vuelta al pool una vez que su trabajo aterrizó",
		Usage: `Liberar el camp de un soldier de vuelta al pool una vez que su trabajo aterrizó.

Uso:
  vx release <task-id> [--force] [--discard]

Se rechaza a menos que el camp esté limpio y (para una mission) aterrizado.
Para un scout, además se rechaza a menos que exista su reporte final en
~/.vexillum/projects/<project>/reports/<agent-name>.md: el reporte es el
producto de trabajo del scout, igual que el de una mission es su commit
aterrizado. --force omite la verificación del reporte (nunca la del camp
limpio y aterrizado): una salida de emergencia explícita y registrada, nunca
silenciosa.

Una mission shipped aterriza cuando se hace merge de su pull request, así que
release verifica eso. Cuando los commits del camp todavía no están en la rama
base, el rechazo indica que hagas merge del pull request y corras git pull en
la rama base en el checkout del proyecto, y que reintentes. Si gh está
instalado y con sesión iniciada, release también le pregunta a GitHub si el
pull request tuvo merge (solo para una tarea shipped, y nunca es obligatorio):
un pull request con merge cuenta como aterrizado una vez que su commit de merge
está en la rama base local, incluso cuando commits posteriores en la base tocan
las mismas líneas y la verificación de contenido rechazaría para siempre.
Mientras no se haya hecho pull de la base, el rechazo lo dice.

--discard libera el camp incluso cuando esa verificación falla, o cuando el
camp tiene cambios sin commitear. Imprime exactamente qué descartó: los
commits sin aterrizar (hash y asunto) y los cambios sin commitear, que se
resetean. Usalo solo cuando el general confirmó que el trabajo del camp ya
está en la base o se abandona: nunca por tu propio criterio. Los commits
descartados siguen siendo alcanzables desde la rama del camp hasta que se
reutilice el slot.

Si tiene éxito, devuelve el worktree al pool para reutilizarlo y cierra el
pane de herdr, y después poda: borra la rama local vexillum/<task-id> de la
tarea con la semántica de git branch -d (nunca -D) cuando la rama es
ancestro de la rama base, o cuando la tarea está shipped, gh informa que su
pull request tuvo merge y se hizo pull de la base. Si no, o cuando la rama
está checked out en otro worktree, la rama se conserva y una línea dice por qué
y qué hacer. También corre git worktree prune en el repositorio del proyecto, e
imprime lo que se podó en una línea corta. --discard borra una rama solo bajo
esas mismas reglas, así que una rama sin aterrizar sobrevive a él.
`,
	},

	"sentinel": {
		Summary: "Vigilar a los soldiers despachados y registrar los cambios de status",
		Usage: `Vigilar a los soldiers despachados y registrar los cambios de status.

Uso:
  vx sentinel         Hace polling para siempre (en primer plano; correlo en
                      segundo plano)
  vx sentinel drain   Imprime y confirma los wakes pendientes ahora mismo
  vx sentinel await   Bloquea hasta que llegue un wake, o hasta el timeout
                      (para el hook Stop asíncrono que configura 'vx init')

El sentinel consulta cada tarea "running" rastreada en el status en vivo de
herdr, en todos los proyectos que haya visto alguna vez (un proceso sentinel
por máquina). Cuando una termina (done o blocked), persiste el cambio y
registra un wake durable, con alcance al proyecto propio de esa tarea. "drain" y "await" resuelven en qué proyecto
actuar a partir del directorio actual (el toplevel de git, con el mismo
namespace con el que "vx dispatch" arma los camps de un proyecto): si no es un
repo, o si el toplevel es en sí un camp administrado por vexillum, no hay nada
que drenar acá, así que ambos son no-ops silenciosos en lugar de un error.

"drain" revisa una sola vez, al instante: {"decision":"block",...} si ya hay
algo pendiente para ese proyecto, {} en caso contrario. "await" es lo que el
hook Stop asíncrono llama en realidad: bloquea (revisando de nuevo cada pocos
segundos) hasta que aparece un wake o se acaba el tiempo, así que el commander
se despierta incluso si su turno ya había terminado antes de que un soldier
terminara, y no solo cuando un wake ya está pendiente justo en el momento en
que termina un turno. "await" sale de inmediato, sin esperar, fuera de un pane
administrado por herdr (sin HERDR_WORKSPACE_ID): el hook se commitea en el
proyecto y por eso alcanza también a los turnos de Claude Code de cualquier
otra herramienta, que no tienen ninguna tarea para que el sentinel rastree.

Un wake también puede ser feedback de forum: el listener de forum guarda el prompt de un usuario en el inbox del
proyecto y registra un wake de forum, que tanto "drain" como "await" informan
como "forum session <file>: N new messages (ended: false). Run vx forum
inbox." Es el mismo hook y la misma entrega de una sola vez; un wake de forum cuyos mensajes ya fueron
confirmados se descarta. Ver "vx forum" para el listener.

Corre un sentinel por máquina, garantizado por un lock que el kernel suelta
cuando el proceso muere, así que un crash nunca deja un reclamo viejo. El
sentinel registra el build desde el que arrancó (versión y archivo ejecutable)
en ~/.vexillum/sentinel.json y lo revisa después de cada pasada: cuando ese
archivo fue reemplazado en disco (un rebuild, un upgrade) termina la pasada,
suelta su lock y sale, para que un sentinel viejo nunca siga reescribiendo
estado con lógica vieja. "vx dispatch", "vx prompt" y cada "await" arrancan
uno nuevo desde el binario nuevo; "vx doctor" advierte sobre un sentinel que
está desactualizado o duplicado.

Cada "await" se registra bajo ~/.vexillum/sentinel-awaiters/ y nunca sobrevive
al hook que lo lanzó: sale apenas su proceso padre desaparece, y un "await"
más nuevo de la misma sesión y proyecto reemplaza al del turno anterior. Al
arrancar "vx sentinel" o cualquier "await" también se detienen los huérfanos
que dejó una sesión muerta. A un proceso solo se le envía una señal después de
verificar que es un "vx sentinel await", nunca por nombre.
`,
	},

	"forum": {
		Summary: "Abrir un artefacto HTML local para revisión visual y recoger el feedback del usuario",
		Usage: `Abrir un artefacto HTML local para revisión visual y recoger el feedback del usuario.

Uso:
  vx forum <html-file> [--no-open] [--reopen] [--port <n>]
  vx forum poll <html-file> [--reply <text> | --reply-file <path|->] [--timeout <duration>]
  vx forum poll --all [--reply-to <html-file> (--reply <text> | --reply-file <path|->)] [--timeout <duration>]
  vx forum inbox [--ack <uid>...]
  vx forum reply <html-file> (--reply <text> | --reply-file <path|->)
  vx forum end <html-file>
  vx forum stop

forum <html-file> abre (o retoma) la sesión de revisión de ese archivo y vuelve
de inmediato, imprimiendo la URL de la sesión y el siguiente paso; un servidor
local por usuario sigue corriendo en segundo plano en 127.0.0.1 y se detiene
solo cuando no hay nada conectado. El artefacto recibe window.forum.queuePrompt
y window.forum.sendQueuedPrompts, y el usuario puede chatear, encolar mensajes
y enviarlos al agente desde el navegador. Un diagrama Mermaid escrito como
<div class="mermaid">...</div> se convierte en una pizarra editable. Las
sesiones se identifican por la ruta absoluta del archivo.

  --no-open   no abre el navegador
  --reopen    reabre una sesión que el usuario terminó desde el navegador
              (solo cuando el usuario pidió más revisión)
  --port      puerto donde enlazar si el servidor todavía no está corriendo

forum poll bloquea hasta que el usuario envía feedback, termina la sesión, o
deja el navegador desconectado más allá de un período de gracia (status
browser_disconnected; la sesión se puede retomar). El feedback entregado se
consume. --reply muestra la respuesta en markdown del agente en el panel de
conversación del navegador antes de volver a esperar; --reply-file la lee de
un archivo (- es stdin). --timeout devuelve el status timeout si no llega nada
a tiempo. Correlo de nuevo después de cada respuesta. Ver la skill forum para el formato
exacto de la salida.

forum poll --all escucha todas las sesiones abiertas a la vez, así que un solo
poll cubre varias ventanas de revisión. Cada llamada entrega el feedback de una
sesión (la que lleva más tiempo esperando) y la nombra en la salida (session y
file); other_sessions_pending dice cuántas más están esperando, y la siguiente
llamada las entrega. Una sesión que termina mientras espera se informa una vez
como ended, y no_sessions significa que no hay nada abierto. Las sesiones que
se abren mientras espera se suman. --reply-to nombra la sesión a la que
responde un --reply, ya que el poll en sí no tiene archivo.

La entrega se confirma, no se supone: un poll toma en lease sus prompts y el
comando los confirma una vez que se escribió su salida. Si un poll muere antes,
volver a correr poll entrega otra vez los mismos prompts, marcados como
redelivered, así que no se pierde nada; salteá cualquier uid que ya hayas
aplicado.

forum <html-file> corrido dentro de un proyecto de vexillum también registra
ese proyecto como el dueño de la sesión y se asegura de que el listener de
forum esté corriendo. El listener es un proceso pequeño en segundo plano (sin
modelo, sin nada que pueda decidir) que sostiene el poll por vos: cada prompt
que envía el usuario se escribe en el inbox durable del proyecto, el hook Stop
despierta al commander con una línea "forum session <file>: N new messages", y
el navegador muestra la ronda como retransmitida (con el tiempo transcurrido, y
una nota cuando no se conoce ninguna sesión del commander) hasta que haya una
respuesta real, un cambio del artefacto o el Stop waiting del usuario. Una
sesión abierta fuera de cualquier proyecto no tiene commander al que despertar:
el listener la deja en paz, y forum poll funciona para ella como antes. El
listener sale solo cuando no hay ninguna sesión abierta o todas las ventanas de
revisión desaparecieron.

forum inbox imprime los prompts sin leer de este proyecto (correlo desde el
proyecto) en el formato de salida del poll, acotado en tamaño, con los
adjuntos solo por ruta. No marca nada como leído: forum inbox --ack <uid>...
confirma los uids que manejaste (los que se imprimieron), y un prompt sin
confirmar se muestra de nuevo. forum reply publica la respuesta en markdown del
agente (--reply, o --reply-file; - es stdin) en el panel de revisión y responde
la ronda sin bloquear, así que un commander que no hace poll puede responder.

forum end termina la sesión como el agente (un forum <html-file> a secas la
reabre después). forum stop detiene el listener y el servidor en segundo plano.
`,
	},

	"banner": {
		Summary: "Publicar un artefacto HTML en una URL pública, o actualizar uno ya publicado",
		Usage: `Publicar un artefacto HTML en una URL pública, o actualizar uno ya publicado.

Uso:
  vx banner <file.html> [--private | --password <pw>]
  vx banner <file.html> --site <id> --update-key <key> [--private | --password <pw>]
  vx banner --unpublish --site <id> --update-key <key>

La primera forma publica una página nueva: los assets locales (imágenes, CSS,
JS en el mismo árbol de directorios que <file.html>) se incrustan en el HTML
como URIs data:, las referencias remotas (http/https, un CDN, Google Fonts) se
dejan tal cual, y el resultado se envía con POST a un servicio de hosting HTML
de terceros (ht-ml.app por defecto): no hace falta ninguna cuenta ni API key.
La respuesta trae la URL de la página y una update_key.

Detalles de la incrustación:
  - Los candidatos de srcset (img, y source dentro de picture) se incrustan
    cada uno.
  - Las reglas @import de CSS se siguen y se reemplazan por la hoja de estilos
    importada, hasta 8 niveles de profundidad (los ciclos se omiten con una
    advertencia).
  - Las referencias file: se reemplazan por about:blank (con una advertencia),
    así que una ruta local del sistema de archivos nunca se publica.
  - Un asset local de más de 10 MB, o uno que haría que el total incrustado
    pase de 25 MB, se deja como referencia con una advertencia en lugar de
    incrustarse. Los topes, en bytes, se cambian con:
      VEXILLUM_BANNER_MAX_ASSET_BYTES   tope por asset (por defecto 10485760)
      VEXILLUM_BANNER_MAX_BUNDLE_BYTES  tope de la página completa (por defecto 26214400)
  - Una página que nunca pinta su propio fondo (sin background en
    html/body/:root, sin clase bg-*, sin data-theme, sin hoja de estilos)
    recibe una advertencia, ya que el texto puede quedar invisible sobre la
    superficie propia del host. Nunca bloquea la publicación.

update_key es la ÚNICA credencial que puede volver a tocar esa página: se
imprime una sola vez, justo después de publicar, y vexillum no la guarda en
ningún lado. Guardala vos, ahora mismo. Si la perdés, esa página nunca más se
podrá actualizar ni despublicar, por nadie y por ningún motivo: no existe una
recuperación del tipo "forgot my key".

Si el pedido de publicación falla de forma ambigua (un timeout o un 5xx: todo
caso en el que no queda claro si el servidor llegó a crear la página), tratá la
update_key como perdida aunque no se haya impreso nada: reintentar NO es
seguro, porque cada POST crea una página nueva en lugar de confirmar una
anterior. Si eso pasa, este comando te lo dice explícitamente.

La segunda forma vuelve a publicar una página existente en el mismo lugar:
pasá los mismos --site y --update-key que recibiste cuando se publicó por
primera vez, junto con el archivo nuevo a publicar ahí.

--unpublish reemplaza la página en --site por una página placeholder detrás de
una contraseña recién generada que se descarta inmediatamente después del
pedido (nadie, incluido vos, la verá jamás): no existe un borrado real, la URL
sigue respondiendo, solo muestra el placeholder. Volver a correr este comando
con los mismos --site y --update-key (una republicación corriente) hace visible
otra vez el contenido original ahí en cualquier momento.

Flags de contraseña (solo tienen sentido al publicar o republicar contenido,
no con --unpublish):

  --private        genera localmente una contraseña de la página y la imprime
                   una sola vez
  --password <pw>   protege la página con una contraseña que ya elegiste

--password, --site y --update-key rechazan un valor vacío (típicamente una
variable de shell sin definir) y un valor que empiece con "--" (el siguiente
flag, tragado por accidente). Usá --flag=<value> si un valor realmente empieza
con "--".

Cualquiera de los dos fija la contraseña de la página en el mismo pedido que la
publica. No hay forma de volver pública una página que ya es privada: el
backend ignora en silencio un intento de borrar una contraseña, así que este
comando no ofrece un flag que informaría mal una página como pública mientras
sigue protegida.
`,
	},
}

// localizeCommands returns cmds with each Summary and Usage replaced by its
// Spanish text, so the page and index renderers stay locale-agnostic. It
// fails, naming the command, when one has no Spanish text yet.
func localizeCommands(cmds []cli.Command) ([]cli.Command, error) {
	out := make([]cli.Command, len(cmds))
	for i, c := range cmds {
		es, ok := commandsES[c.Name]
		if !ok || es.Summary == "" || es.Usage == "" {
			return nil, fmt.Errorf("command %q has no Spanish text: add it to commandsES in tools/docgen/cli_es.go", c.Name)
		}
		out[i] = cli.Command{Name: c.Name, Summary: es.Summary, Usage: es.Usage}
	}
	return out, nil
}
