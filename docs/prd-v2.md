# PRD - Vexillum v2

## Propósito de este documento

La v1 quedó construida y verificada (build, vet, test con `-race`; verificación en vivo contra herdr 0.9.0). Este PRD define la v2. No reabre las decisiones marco de la v1 que siguen fijas; sí cierra dos que estaban entornadas, salda dos deudas conscientes que la v1 dejó abiertas, y agrega cuatro integraciones externas.

La v2 sigue siendo de uso personal. Eso importa para el nivel de anclaje que se exige: se prefieren checks baratos y deterministas que no dependan de infraestructura externa (CI con servicios reales) por sobre garantías caras que no se justifican para un solo usuario todavía.

A diferencia de la v1, la v2 no se organiza por capas. Las capas fueron un método de construcción incremental donde cada una era prerrequisito de la siguiente. La v2 son piezas de dos naturalezas distintas (saneamiento e integración) que no se apilan una sobre otra. Se documentan separadas por naturaleza, no numeradas como capas.

**Estado al día de hoy:** la v2 completa está resuelta - Parte A (A.1, A.2) y Parte B (B.1-B.4). Se deja todo registrado con su criterio de terminado, marcado como cerrado, para que el PRD siga siendo el registro de qué se hizo y bajo qué criterio. Pendiente real: la tanda de verificación en vivo (ver binnacles), diferida a propósito para el final.

---

## Glosario del producto (delta sobre v1)

El vocabulario de la v1 sigue vigente sin cambios (commander, soldier, general, mission, scout, camp, sentinel, `~/.vexillum/`). Términos nuevos que introduce la v2:

| Concepto | Término (código / CLI) |
|---|---|
| CLI agent-ergonomic que sigue el estándar axi.md, distribuido como Agent Skill | AXI |
| Soldier cuya mission requiere manejar un navegador | soldier-con-browser |
| Relanzar limpia una tarea que quedó a medias | re-dispatch |
| Gate de validación (review, test, lint, document) que termina abriendo el PR | no-mistakes |

Los AXI no son features del binario ni instrucciones escritas a mano en el markdown del commander. Son CLIs externos que se distribuyen como Agent Skills (formato agentskills.io): se instalan con `npx skills add ...`, el skill es solo discovery (le enseña al agente a invocar `npx -y <axi>`), y el CLI real llega on-demand la primera vez que se corre. El binario Go no los ejecuta ni los instala; a lo sumo `doctor` reporta si están instalados.

---

## Contexto y decisiones marco (delta sobre v1)

Las decisiones marco de la v1 siguen fijas. La v2 cierra estas:

- **herdr primario, definitivo.** En la v1 herdr era primario "por ahora" con tmux como backend de control. En la v2 esto deja de ser provisional: herdr es el primario y no se relitiga. Promover tmux a primario sale del roadmap activo (ver Diferidas).
- **Pi fuera de v2.** Pi como segundo harness requiere abstraer una interfaz `Harness`, y esa abstracción solo se valida con dos harnesses reales corriendo. El plan de Claude del autor no cubre Pi, así que Pi no se puede ejecutar. Construir la abstracción contra un implementador real y uno hipotético produce una costura adivinada, no probada. Pi queda diferido por imposibilidad de prueba, no por prioridad. La costura se deja preparada; la abstracción no se construye.
- **Lieutenant a v3.** El segundo commander (local y remoto por SSH) depende de un commander único sólido y agrega concurrencia sobre concurrencia. Las dos deudas que la Parte A salda son precondición suya. No entra en v2. Nota de la investigación de firstmate: el modelo de referencia (secondmate) NO reconcilia estado entre pares, jerarquiza (un primary rutea y supervisa, cada secondmate posee su estado local). Eso baja el costo estimado de la v3; el diseño de referencia vive en firstmate `docs/remote-secondmates.md`.

---

## Decisiones de integración de AXIs (transversales a la Parte B)

Salieron de investigar los repos reales de los AXIs y de cómo firstmate los teje. Aplican a las cuatro integraciones:

- **Distribución como Agent Skill, no como instrucción en markdown.** Los cuatro AXIs se instalan con `npx skills add kunchenguid/<axi>`. El skill es un stub de discovery; el agente aprende a usar el AXI usándolo. Esto corrige el supuesto original del PRD de que "la instrucción vive en el markdown del commander": vive en un skill instalado.
- **On-demand, no obligatorio en init.** Ningún AXI se descarga en `init`. Meter red en `init` degradaría el comando más básico (que hoy es puramente local de disco) y forzaría herramientas que una misión puede no necesitar. El commander instala el AXI que una misión requiere, cuando la requiere. `doctor` reporta cuáles están instalados, de forma informativa, no bloqueante.
- **Rol nuevo del binario: verificar skills, no instalarlas.** `doctor` chequea si los skills/AXIs esperados están y, si faltan, imprime el `npx skills add ...` exacto. La instalación la corre el general o el commander, no el binario. Simetría con lo que `doctor` ya hace con herdr y claude: reporta presencia, no instala.

---

## Parte A - Saneamiento de deudas de v1 (RESUELTA)

Estas dos piezas no agregaban capacidad de producto: cerraban agujeros que la v1 dejó abiertos de forma consciente. Eran prerrequisito real de la v3. Ambas están resueltas; se dejan registradas con su criterio de terminado.

### A.1 - Anclaje del contrato con herdr [RESUELTA]

**Problema.** El wrapper `CLI` de `internal/herdr` ejecuta `herdr` real y parsea su salida (exit codes, JSON de error, códigos como `agent_name_taken`). Ese parseo no tenía prueba automática repetible: los tests inyectan un fake que habla el dialecto esperado. El comportamiento real se verificó en vivo una vez, a mano, contra herdr 0.9.0, y esa versión no estaba anclada en ningún check ejecutable.

**Qué se hizo.** `doctor` ejecuta `herdr --version` y reporta la versión detectada; si no es 0.9.x, emite una advertencia explícita (no bloqueante).

**No entró (a conciencia).** Test de contrato que ejercite el `CLI` real contra herdr en CI. Requiere herdr y tmux en el runner, saca a CI de ser `go test` puro, y es sobreingeniería para uso personal hoy. Queda como opción futura si la v2 se abre a otros usuarios.

**Criterio de terminado (cumplido).** `doctor` en un entorno con herdr instalado reporta la versión real; con una versión distinta de 0.9.x, la advertencia aparece. La suposición implícita "estoy contra 0.9.0" es ahora un check visible.

**Limitación conocida y aceptada.** El check avisa "la versión cambió", no "el parseo sigue siendo correcto". Es un detector de cambio, no una verificación de contrato.

### A.2 - Re-dispatch de tareas interrupted [RESUELTA]

**Problema.** La v1 detectaba y reportaba una tarea `interrupted` pero no hacía nada con ella. El restart-proof era de detección, no de recuperación.

**Qué se hizo.** Re-dispatch, no resumption. Ante una tarea `interrupted`, se descarta el camp sucio, se crea un camp limpio, y se relanza la mission desde el prompt original. NO se retoma el trabajo parcial: ni el working tree a medias, ni la sesión del agente muerto. Se eligió esta forma porque es determinista y no depende del harness.

**Criterio de terminado (cumplido).** Una tarea `interrupted` se relanza con un comando y termina en el mismo estado final que si se hubiera lanzado bien la primera vez. El resultado no depende de cuánto trabajo había hecho el soldier muerto.

**Nota de nombre.** Se documenta como "re-dispatch" y no como "resumption" a propósito: resumption implica retomar, y esta feature no retoma.

**Dependencia abierta con B.3.** Cuando se implemente chrome-devtools-axi (soldier-con-browser), el criterio de A.2 se extiende: el camp limpio no debe dejar procesos de browser huérfanos. Esa extensión es trabajo de B.3, no de A.2, pero se anota acá porque toca esta pieza ya cerrada.

---

## Parte B - Integraciones (AXIs) [RESUELTA]

Cuatro integraciones externas. Rigen las Decisiones de integración de arriba (Agent Skill, on-demand, doctor verifica). Tres son de bajo acople con el core; una (chrome-devtools) toca la Capa 4. No hay orden obligatorio salvo el de la secuencia recomendada.

**Regla general.** Un AXI entra a Vexillum cuando una mission concreta lo necesita, no porque exista o sea oficial. El catálogo de axi.md no es un checklist a completar.

### B.1 - quota-axi

**Qué es.** AXI oficial, data-only y local-first. Reporta ventanas de cuota/uso de varios proveedores (Claude, Codex, Cursor, Copilot, Grok, y más). Emite no solo porcentaje restante sino señales derivadas: `runway` (proyección de agotamiento), `spendPriority`, y una señal de selección comparativa por scope. Está diseñado para "routing-aware agents", pero es estrictamente data-only: el ruteo lo hace el consumidor, nunca quota-axi.

**Valor en v2.** Visibilidad de cuota antes de despachar. Consulta informativa: el general pregunta, el commander corre quota-axi y muestra. No gatea despacho.

**Alcance.**
- Entra: el commander consulta cuota vía quota-axi cuando el general se lo pide; `doctor` reporta si está instalado.
- No entra: gate de admisión de tareas por cuota; enrutamiento automático; lógica de cuota en el binario Go.

**Nota a v3.** El gate de cuota (no despachar si no alcanza) y el enrutamiento multi-modelo son v3: chocan con la decisión marco de harness único en v2. quota-axi ya emite las señales de ruteo que ese gate futuro necesitaría; en v2 solo se consumen para mostrar. firstmate teje quota en el dispatch mismo (`fm-quota-choose`, `fm-quota-array-dispatch`) como referencia para cuando llegue.

**Acople.** Ninguno con el core.

### B.2 - lavish-axi

**Qué es.** AXI oficial. Editor de artefactos HTML generados por el agente: abre el HTML en un browser local, permite fijar elementos y rangos de texto, editar diagramas Mermaid como whiteboards, y mandar feedback estructurado de vuelta al agente. Trae playbooks de visualización de fábrica para casos comunes (planes de producto y técnicos, exploraciones de diseño).

**Valor.** Revisar un plan (arquitectura incluida) de forma interactiva en vez de punto por punto en un `plan.md`. El caso de uso de planificación técnica interactiva no hay que diseñarlo: lavish lo trae en sus playbooks.

**Alcance.**
- Entra: el commander corre un plan a través de lavish cuando el general se lo pide; `doctor` reporta si está instalado.
- No entra: lavish como canal permanente de interacción general-commander; lavish reemplazando el loop de texto; lógica de lavish en el binario Go. Se invoca bajo demanda, no cambia el modelo de interacción.

**Acople.** Ninguno con el core, siempre que se respete el "no entra": lavish es una herramienta que el commander dispara bajo demanda, no una segunda fuente de verdad del plan.

### B.3 - chrome-devtools-axi

**Qué es.** AXI oficial. Automatización de browser agent-ergonomic (navegar, clickear, llenar, extraer) sobre chrome-devtools-mcp, con operaciones combinadas y filtrado por query. Su propio skill le dice al agente cuándo NO usar browser (web search o páginas estáticas: usar fetch/curl), así que el browser se levanta solo cuando de verdad hace falta.

**Valor.** Habilita soldiers cuya mission requiere un navegador: probar cambios de frontend en vivo, investigar en la web, operar en plataformas como Vercel.

**Alcance.**
- Entra: un soldier maneja un browser vía chrome-devtools-axi dentro de su camp cuando la mission lo requiera; `doctor` reporta si está instalado.
- No entra: browser por defecto en toda mission (solo las que lo pidan).

**Acople con el core (único AXI que lo tiene).** Un soldier-con-browser agrega estado de ejecución vivo (un proceso de browser por soldier) que el restart-proof de la Capa 4 no contemplaba. Cuando un soldier queda `interrupted`, Vexillum cierra su pane de herdr, pero un browser lanzado dentro de ese pane no necesariamente se cierra solo.

**Requisito derivado (extiende A.2).** El re-dispatch debe cerrar también el browser vivo del soldier muerto al limpiar el camp. El criterio de terminado de B.3 incluye: el camp limpio no deja procesos de browser huérfanos.

### B.4 - no-mistakes (gate completo, contra GitHub) [RESUELTA]

**Qué es.** El gate de validación completo que corre antes de que un PR se cree y termina abriéndolo. Detalle completo, incluida la corrección de esta sección, en `docs/no-mistakes.md`.

**Corrección respecto de versiones previas de este PRD (investigado en esta sesión, no supuesto).** Dos correcciones, no una:
1. no-mistakes estaba descrito como "apertura de PR real, implementado con gh-axi" - eso ya se había corregido a "es un pipeline de etapas... gh-axi es un componente, no la feature entera". Esa segunda versión también estaba mal: **no-mistakes no necesita gh-axi para nada.** Investigado contra el repo real (`github.com/kunchenguid/no-mistakes`): pone un git remote local delante del real y abre el PR con su propia integración de GitHub una vez que el pipeline pasa. gh-axi queda fuera del alcance de B.4.
2. **no-mistakes no es una feature interna de firstmate configurada con `.no-mistakes.yaml`** como si fuera parte de su código - es su propio repo standalone (`kunchenguid/no-mistakes`), instalado por `curl` (no por `npx skills add`, no es un AXI en el sentido de las otras tres integraciones de esta Parte B). firstmate lo consume como cliente, igual que cualquier otro repo podría.

**Qué se hizo.** `vexillum doctor` reporta si el binario `no-mistakes` está instalado y si el proyecto ya corrió `no-mistakes init` (existe el remote git `no-mistakes`). Comando nuevo `vexillum ship <task-id>`: para una mission `done`, corre `git push no-mistakes <camp-branch>` desde el camp - determinista, sin juicio de agente de por medio. Si el proyecto no está gateado todavía, `ship` corre `no-mistakes init` él mismo la primera vez (decisión tomada con el general después de la primera pasada, ver `docs/no-mistakes.md` "Auto-gate en ship, no en init"): el setup "viene por defecto" sin que nadie tenga que acordarse de correrlo aparte, pero nada de red pasa hasta que el general efectivamente pide shippear algo - `vexillum init` se queda puramente local. No supervisa el pipeline después de eso.

**Decisión abierta, cerrada.** ¿Quién dispara la apertura del PR: el soldier desde su prompt, o el binario de forma determinista? **El binario**, vía `vexillum ship` - exactamente el mecanismo que el PRD ya intuía ("algo confiable no se deja al criterio del LLM"), ahora con un mecanismo real y determinista (`git push` plano) para hacerlo, en vez de una llamada a gh-axi con juicio de por medio.

**Alcance.**
- Entra: `doctor` reporta presencia del binario `no-mistakes` y si el proyecto está gateado; `vexillum ship <task-id>` empuja una mission `done` a través del gate.
- No entra: cualquier lógica de review/test/lint/docs en el binario Go (100% de `no-mistakes`); supervisión del pipeline post-push (el general usa las herramientas propias de `no-mistakes`); `no-mistakes init` desde `vexillum init` (vive en `ship`, lazy, en el primer uso); gh-axi (no es una dependencia real de este flujo); forges distintos de GitHub; manejo de merge del PR resultante.

**Acople.** Es la frontera externa más cara de la v2 y la única con efecto irreversible fuera de la máquina (un push real a través del gate). `vexillum ship` es un `git push` plano y determinista - el riesgo real vive del lado de `no-mistakes` (su propio pipeline y apertura de PR), no en lógica nueva de vexillum.

---

## Restricción heredada por v3

El lieutenant (v3) va a coordinar estado de tareas entre dos máquinas. Según el modelo de firstmate (secondmate), no es reconciliación entre pares sino jerarquía: un primary rutea y supervisa, cada secondmate posee su estado local. Aun así, cada vez que la v2 toque el struct `Task` (o el formato en disco de `~/.vexillum/tasks/`), la pregunta obligatoria es: ¿esto sobrevive a que haya dos de estos en dos máquinas distintas? No se pide diseñar para el lieutenant ahora. Se pide no tomar decisiones de formato que fuercen una migración en v3. Es una restricción, no trabajo.

---

## Secuencia recomendada (Parte B) [seguida al pie de la letra]

1. **B.1 (quota-axi).** Hecho. El más barato y aislado - fijó el patrón `doctor` + `knownAXIs`.
2. **B.2 (lavish-axi).** Hecho. Reusó el patrón de B.1, pero con dos diferencias reales encontradas al investigar (nombre de skill distinto del repo, sin `-g`) - confirmó que investigar cada AXI en vez de asumir el patrón anterior valía la pena.
3. **B.3 (chrome-devtools-axi).** Hecho. Tocó el core: `herdr tab create --env` namespacea el browser de cada soldier por task id, `redispatch` lo limpia al descartar un camp muerto.
4. **B.4 (no-mistakes).** Hecho, con `docs/no-mistakes.md` escrito y su decisión abierta cerrada antes de implementar, como pedía esta sección.

---

## Features diferidas (post-v2)

- **Lieutenant (orquestador secundario), local y remoto por SSH.** A v3. Diseño de referencia real en firstmate (`docs/remote-secondmates.md`, `docs/secondmate-parent-channel.md`): un firstmate home entero en otra máquina, primary dueño de routing y supervisión, secondmate dueño de su estado local. No reconcilia estado entre pares. Reabrir cuando la Parte B esté cerrada.
- **Gate de cuota y enrutamiento multi-modelo.** A v3. quota-axi ya emite las señales; el gate que las consume choca con harness único en v2.
- **Pi como segundo harness.** Diferido por imposibilidad de prueba: el plan de Claude del autor no cubre Pi. La costura se deja preparada; la abstracción no se construye hasta poder ejecutar Pi.
- **tmux como backend primario.** herdr es primario definitivo. Promover tmux sale del roadmap activo salvo que aparezca una limitación concreta de herdr. Hoy no existe.
- **Otros AXIs del catálogo.** Entran cuando una mission los pida, uno por uno. Nota: el catálogo tiene dos community relevantes a este dominio, cyber-mux (envuelve tmux/herdr/WezTerm) y vercel-axi (opera Vercel), por si aparecen misiones que los pidan.
- **Forges distintos de GitHub para no-mistakes.** GitLab (glab-axi), Forgejo (forgejo-axi), etc. Cada uno es una integración aparte.
