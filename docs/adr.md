# ADR - Vexillum

Registro de decisiones de arquitectura. Cada una: la decisión, por qué, qué alternativa se descartó y por qué. Sirve para no reabrir debates ya cerrados y para que cualquier agente que trabaje en el repo (incluido Claude Code) no proponga desandar algo sin ver primero el motivo.

Formato: **Decisión** / **Razón** / **Alternativa descartada**.

---

## ADR-01 - Lenguaje: Go

**Decisión:** el binario se escribe en Go.

**Razón:** la meta de distribución (instalar por brew o curl, multiplataforma, sin exigir runtime en la máquina del usuario) se cumple nativamente con un binario estático de Go. Además, la plomería del proyecto (procesos, concurrencia, estado en disco) es terreno fuerte de Go. Si en el futuro se reactivan los secondmates remotos, un binario estático que viaja por scp y corre sin dependencias es muy superior a exigir un runtime en cada host.

**Alternativa descartada:** Node/TypeScript, que es el terreno conocido del autor. Se descartó porque su modelo de distribución es npm (Node requerido en la máquina), incompatible con el "un comando y anda" por brew/curl que es un objetivo firme. El costo aceptado: Go es lenguaje nuevo para el autor.

## ADR-02 - Empaquetado: binario que scaffoldea, no repo que se clona (Opción A)

**Decisión:** vexillum es un binario instalable que scaffoldea config y estado en el proyecto/host. No es un repositorio que el usuario clona y habita.

**Razón:** el modelo "clonar un repo y trabajar adentro" (el de firstmate) fue el disparador de incomodidad original. Un binario resuelve eso: se instala una vez y se invoca desde cualquier proyecto.

**Alternativa descartada:** el modelo repo-como-programa de firstmate, donde el repo clonado ES el conjunto de instrucciones que el harness ejecuta. Se descartó por la fricción de habitar un repo ajeno y mantenerlo actualizado a mano. Nota: este modelo sí es más transparente (el AGENTS.md está a la vista); esa transparencia se preserva igual porque el scaffold escribe el AGENTS.md de producto en el proyecto del usuario.

## ADR-03 - Harness único: Claude Code (Pi diferido)

**Decisión:** en toda la v1 el único harness soportado es Claude Code.

**Razón:** soportar varios harnesses obliga a una abstracción sobre mecanismos de supervisión distintos (Stop hook en Claude, watcher extension en Pi) desde el día uno. Con un solo harness, la supervisión es un solo camino y el código se achica drásticamente. Es la principal fuente de minimalismo real del proyecto.

**Alternativa descartada:** multi-harness desde el arranque (como firstmate, que soporta Claude, Grok, Pi, Codex, OpenCode). Se difirió Pi a post-v1: la costura para una interfaz `Harness` se deja preparada, pero la abstracción no se construye hasta que exista el segundo harness.

## ADR-04 - Backend de sesión: herdr primario, tmux de control

**Decisión:** herdr es el backend de sesión primario. tmux se mantiene implementable en paralelo detrás de la misma abstracción, como backend de control y comparación.

**Razón:** herdr, como herramienta, es sólida (multiplexor en Rust, persistencia con detach, estado working/blocked/idle por pane, y una socket API por la que se puede consultar quién está bloqueado). Esa API de estado nativo hace la supervisión zero-token más limpia y barata que sondear a ciegas, que es como se hace sobre tmux. tmux se mantiene como backend de control porque sus caminos de supervisión están más maduros y verificados: sirve de referencia contra la cual comparar cuando herdr se comporte raro.

**Alternativa descartada:** tmux como único backend (el default verificado de firstmate). Se descartó como primario porque su modelo obliga a sondear el estado desde afuera, mientras herdr lo expone por API. No se descartó del todo: queda como backend de control, no se elimina.

**Matiz registrado:** la persistencia de herdr restaura el layout visual y puede resumir sesiones soportadas, pero los procesos originales no sobreviven a un reinicio. El estado de dominio (qué camp, qué task shape, cuánto avanzó) NO lo guarda herdr; lo persiste vexillum. herdr da la mitad visual del restart-proof; la otra mitad es responsabilidad del binario.

## ADR-05 - La inteligencia del commander vive en markdown, no en Go

**Decisión:** el razonamiento del commander (qué despachar, cómo rutear) vive en un AGENTS.md de producto, editable, que Claude Code interpreta. El binario Go hace plomería determinista: crear worktrees, lanzar procesos, persistir estado, consultar al sentinel.

**Razón:** con Claude Code como harness, el modelo es bueno leyendo y siguiendo un AGENTS.md; reimplementar ese ruteo en Go sería pelear contra la corriente y triplicar el trabajo. La parte difícil del proyecto es la plomería, no la inteligencia; concentrar el esfuerzo de Go ahí. Además, un AGENTS.md editable permite que el usuario ajuste el comportamiento sin tocar el binario.

**Alternativa descartada:** poner la inteligencia de ruteo en el código Go (binario decide todo, harness solo ejecuta tareas concretas). Da control determinístico y testeable, pero mucho más código y reimplementa lo que el modelo ya hace gratis. Se justificaría si esto fuera un producto crítico con SLA; no lo es.

## ADR-06 - Un solo vocabulario, en inglés

**Decisión:** los términos del dominio (commander, soldier, mission, scout, camp, sentinel) van en inglés, en código y en la voz del agente. Sin tabla de traducción español/inglés. El agente detecta en qué idioma le escribe el usuario y responde en ese idioma, pero no traduce los términos del dominio.

**Razón:** mantener dos vocabularios sincronizados (commander/comandante, soldier/soldado) es deuda desde la primera línea y una fuente de mezclas incoherentes en el markdown que el modelo interpreta. El sabor romano ya está en los nombres; no hace falta que el agente hable español para que se sienta romano.

**Alternativa descartada:** vocabulario bilingüe con traducción de cada término a la voz en español. Se descartó por la fricción de mantenimiento y porque, de cara a un futuro público con usuarios no hispanohablantes, la voz en español es fricción, no color.

## ADR-07 - Nombre: vexillum, mundo romano-militar

**Decisión:** el binario se llama `vexillum` (el estandarte de la unidad romana). Mundo romano-militar: commander, soldiers, general, mission, scout, camp, sentinel.

**Razón:** el mundo náutico está ocupado por firstmate (captain/crew/fleet/ship), la referencia de la que parte el proyecto; cualquier nombre de mar leería como derivado. La jerarquía militar es transparente y universal (nadie necesita glosario). El filtro de nombre fue doble: libre en brew y sin colisión de producto en la categoría de orquestación de agentes / dev tooling, porque la herramienta puede volverse pública.

**Alternativa descartada:** `legion` (primer favorito) cayó por colisión directa: ya existen orquestadores de agentes homónimos, uno con `legion init` sobre git worktrees + tmux, casi calcado. `castra` y `aquila` cayeron por colisiones similares. Mundos previos descartados: náutico (es el de firstmate), la metáfora militar entendida como copia estructural de firstmate (se aceptó igual, a conciencia, porque el vocabulario se separa limpio aunque el esqueleto de jerarquía sea parecido), guaraní y forja japonesa (nombres difíciles de tipear para no hispano/anglohablantes), Hefesto griego (largo, y la metáfora de forja mapea a fabricar, no a coordinar).

**Precio aceptado:** dos letras más que un nombre corto ideal y un alias corto pobre (`vex`), a cambio de un nombre limpio en la categoría que aguanta el futuro público.

## ADR-08 - Construcción por capas

**Decisión:** se construye en cuatro capas incrementales, cada una usable y testeable sola, sin pasar a la siguiente hasta probar la anterior. Capa 1 CLI base (init + doctor), Capa 2 estado en disco, Capa 3 un proceso, Capa 4 concurrencia.

**Razón:** el orden va de lo determinista-y-solo a lo concurrente-y-difícil. Aísla el riesgo: cuando algo falla en una capa temprana, la causa está acotada. La concurrencia, que es el pico de dificultad, se enfrenta al final, con el resto ya sólido.

**Alternativa descartada:** arrancar por la orquestación (lo más atractivo). Se descartó porque mete todas las dificultades a la vez (concurrencia, procesos, estado concurrente) sin base probada debajo.

## ADR-09 - Lieutenant (orquestador secundario persistente) diferido de la v1

**Decisión:** el lieutenant (un segundo commander persistente), local y remoto por SSH, queda fuera de la v1. Se reabre después de la Capa 4.

**Razón:** depende de que el commander único exista y sea sólido primero (es, literalmente, "un segundo commander"). Sus partes difíciles (coordinación entre dos commanders, estado compartido y reconciliado) no las simplifica Go; Go solo facilita el transporte remoto, que es el problema chico. Meterlo antes carga el diseño con decisiones que no se ejecutan hasta mucho después.

**Alternativa descartada:** incluirlo en la v1 apoyándose en que "Go lo hace autosostenido". Se descartó: el argumento del binario estático solo cubre el transporte (el 33% fácil), no la coordinación ni el estado compartido (el 66% difícil, que queda igual de difícil en cualquier lenguaje).

## ADR-10 - Lieutenant cerrado

**Decisión:** el lieutenant, que ADR-09 dejaba abierto para reconsiderar "después de la Capa 4", queda cerrado - no es un diferimiento más, es un cierre. No se reabre solo porque la Capa 4 (o la v2 entera) ya esté sólida, que era la condición que ADR-09 planteaba.

**Razón:** el cuello de botella real del uso personal es la cuota del plan de Claude, no la capacidad de despachar o supervisar agentes. Un segundo commander - local o remoto - consume de esa misma cuota y no agrega capacidad; el problema que un lieutenant resolvería (más paralelismo) no es el problema que efectivamente limita. El camino remoto además exige acotar los proyectos a solo-ship (nada de land local desde la máquina remota) y una durabilidad de endpoint (sesión GUI viva, o credenciales de vuelta en keychain) más cara de operar y mantener de lo que ADR-09 había estimado.

**Alternativa descartada:** reabrir el diseño ahora que la Capa 4 y la v2 completa están resueltas (exactamente la condición que ADR-09 dejó planteada para reconsiderar). Se descartó: tener una base sólida no cambia el argumento de fondo - la cuota sigue siendo el límite real, y un lieutenant no la mueve un bit.

**Se reabre solo** ante un límite distinto de la cuota del plan (por ejemplo, un límite de paralelismo real que la cuota no explique).
