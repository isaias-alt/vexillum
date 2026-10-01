import { i18n, type Lang } from "@/lib/i18n";

export interface Step {
  title: string;
  command: string;
  description: string;
  output: string[];
  /** Docs page the step links to, relative to the docs root. */
  docs: string;
}

export interface Dictionary {
  meta: { description: string };
  nav: { docs: string; github: string; getStarted: string };
  hero: {
    eyebrow: string;
    title: string;
    sub: string;
    cta: string;
  };
  minute: {
    eyebrow: string;
    title: string;
    sub: string;
    motto: string;
    steps: Step[];
    docsLink: string;
  };
  vocab: {
    eyebrow: string;
    title: string;
    items: { term: string; def: string }[];
  };
  dispatching: {
    eyebrow: string;
    title: string;
    sub: string;
    promptA: string;
    promptB: string;
    outputA: string[];
    outputB: string;
  };
  oss: {
    eyebrow: string;
    title: string;
    issue: string;
    contributing: string;
  };
  footer: { license: string; github: string; docs: string };
  notFound: { title: string; back: string };
}

const TASK_A = "7c1e9a40d3b2f658";
const TASK_B = "e41b07d9a52c3f86";

const en: Dictionary = {
  meta: {
    description:
      "vexillum orchestrates coding agents from your terminal. A commander dispatches soldiers into isolated camps, a sentinel watches for what needs your attention.",
  },
  nav: { docs: "docs", github: "github", getStarted: "get started" },
  hero: {
    eyebrow: "for coding agents",
    title: "One commander. Many soldiers.",
    sub: "vexillum orchestrates coding agents from your terminal. A commander dispatches soldiers into isolated camps, a sentinel watches for what needs your attention.",
    cta: "read the docs",
  },
  minute: {
    eyebrow: "a minute with vexillum",
    title: "One CLI for the whole chain of command.",
    sub: "Install, dispatch, watch and check in - every step below is a real command. Click one, or just watch.",
    motto: "gradus cursusque - the steps and the course",
    docsLink: "Docs",
    steps: [
      {
        title: "Install vexillum",
        command: "curl -fsSL https://vexillum.lucasco.dev/install | bash",
        description:
          "One binary, no runtime dependencies. Works the same over curl or brew.",
        output: [
          "[ok]    Checksum verified",
          "[ok]    Installed vexillum to /usr/local/bin/vexillum",
        ],
        docs: "/get-started/install",
      },
      {
        title: "Dispatch a mission",
        command: 'vexillum dispatch "refactor the payment module" --kind mission',
        description:
          "A soldier spins up in its own camp and executes the work end to end.",
        output: [
          `task_id=${TASK_A} kind=mission camp_slot=1 camp_branch=vexillum/${TASK_A}`,
          "status=running",
        ],
        docs: "/concepts/missions-and-scouts",
      },
      {
        title: "Dispatch a scout",
        command: 'vexillum dispatch "trace the memory leak" --kind scout',
        description:
          "Scouts investigate and report back - they never commit or push anything.",
        output: [
          `task_id=${TASK_B} kind=scout camp_slot=2 camp_branch=vexillum/${TASK_B}`,
          "status=running",
        ],
        docs: "/concepts/missions-and-scouts",
      },
      {
        title: "Hear from the sentinel",
        command: "vexillum sentinel drain",
        description:
          "The sentinel only wakes the commander when a soldier actually settles - done or blocked.",
        output: [
          `{"decision":"block","reason":"A vexillum soldier's status changed:`,
          `- mission ${TASK_A}: running -> done ..."}`,
        ],
        docs: "/concepts/sentinel",
      },
      {
        title: "Check the fleet",
        command: "vexillum status",
        description:
          "Every mission and scout in this project, most recently updated first.",
        output: [
          `${TASK_A}  mission running     vexillum/${TASK_A} refactor the payment module`,
          `${TASK_B}  scout   done        vexillum/${TASK_B} trace the memory leak`,
        ],
        docs: "/reference/cli/status",
      },
    ],
  },
  vocab: {
    eyebrow: "the vocabulary",
    title: "Every part of vexillum maps to a role in the field.",
    items: [
      {
        term: "general",
        def: "The human - you. Talks only to the commander; never manages soldiers directly.",
      },
      {
        term: "commander",
        def: "The orchestrator you run. Reads a mission, dispatches soldiers, and tracks their progress.",
      },
      {
        term: "soldier",
        def: "A subagent the commander runs. Each one works a task independently, inside its own camp.",
      },
      {
        term: "camp",
        def: "A soldier's isolated workspace - its own worktree, so parallel soldiers never collide.",
      },
      {
        term: "sentinel",
        def: "Watches running soldiers and flags the commander when one needs human input.",
      },
      {
        term: "mission / scout",
        def: "The two task types a commander can run: a mission executes work, a scout investigates and reports back.",
      },
    ],
  },
  dispatching: {
    eyebrow: "dispatching",
    title: "No new interface to learn.",
    sub: "You already talk to your commander in Claude Code. Ask it to dispatch a soldier, and it calls vexillum for you.",
    promptA: "dispatch a soldier to refactor the payment module",
    promptB:
      "also have someone trace that memory leak, just investigate for now",
    outputA: [
      `task_id=${TASK_A} kind=mission camp_slot=1`,
      "still running past the quick-settle probe",
    ],
    outputB: `task_id=${TASK_B} kind=scout camp_slot=2`,
  },
  oss: {
    eyebrow: "open source",
    title: "Found a bug? Want to help?",
    issue: "open an issue",
    contributing: "read CONTRIBUTING",
  },
  footer: { license: "MIT licensed", github: "github", docs: "docs" },
  notFound: { title: "Page not found", back: "back to the docs" },
};

const es: Dictionary = {
  meta: {
    description:
      "vexillum orquesta agentes de código desde tu terminal. Un commander despacha soldiers a camps aislados y un sentinel vigila lo que necesita tu atención.",
  },
  nav: { docs: "docs", github: "github", getStarted: "empezar" },
  hero: {
    eyebrow: "para agentes de código",
    title: "Un commander. Muchos soldiers.",
    sub: "vexillum orquesta agentes de código desde tu terminal. Un commander despacha soldiers a camps aislados y un sentinel vigila lo que necesita tu atención.",
    cta: "leer la documentación",
  },
  minute: {
    eyebrow: "un minuto con vexillum",
    title: "Un CLI para toda la cadena de mando.",
    sub: "Instalar, despachar, vigilar y consultar - cada paso es un comando real. Hacé clic en uno, o simplemente mirá.",
    motto: "gradus cursusque - los pasos y el rumbo",
    docsLink: "Documentación",
    steps: en.minute.steps.map((s, i) => ({
      ...s,
      ...[
        {
          title: "Instalá vexillum",
          description:
            "Un solo binario, sin dependencias de runtime. Funciona igual con curl o con brew.",
        },
        {
          title: "Despachá una mission",
          description:
            "Un soldier arranca en su propio camp y ejecuta el trabajo de punta a punta.",
        },
        {
          title: "Despachá un scout",
          description:
            "Los scouts investigan y reportan - nunca hacen commit ni push de nada.",
        },
        {
          title: "Escuchá al sentinel",
          description:
            "El sentinel solo despierta al commander cuando un soldier realmente termina o queda bloqueado.",
        },
        {
          title: "Revisá la flota",
          description:
            "Todas las missions y scouts de este proyecto, la más reciente primero.",
        },
      ][i],
    })),
  },
  vocab: {
    eyebrow: "el vocabulario",
    title: "Cada parte de vexillum tiene un rol en el campo.",
    items: [
      {
        term: "general",
        def: "El humano - vos. Habla solo con el commander; nunca maneja a los soldiers directamente.",
      },
      {
        term: "commander",
        def: "El orquestador que corrés. Lee una mission, despacha soldiers y sigue su progreso.",
      },
      {
        term: "soldier",
        def: "Un subagente que corre el commander. Cada uno trabaja una tarea de forma independiente, dentro de su propio camp.",
      },
      {
        term: "camp",
        def: "El espacio de trabajo aislado de un soldier - su propio worktree, así que soldiers en paralelo nunca chocan.",
      },
      {
        term: "sentinel",
        def: "Vigila a los soldiers en ejecución y avisa al commander cuando uno necesita una decisión humana.",
      },
      {
        term: "mission / scout",
        def: "Los dos tipos de tarea que puede correr un commander: una mission ejecuta trabajo, un scout investiga y reporta.",
      },
    ],
  },
  dispatching: {
    eyebrow: "despacho",
    title: "Ninguna interfaz nueva que aprender.",
    sub: "Ya hablás con tu commander en Claude Code. Pedile que despache un soldier y él llama a vexillum por vos.",
    promptA: "despachá un soldier para refactorizar el módulo de pagos",
    promptB:
      "y que alguien rastree esa pérdida de memoria, solo que investigue por ahora",
    outputA: en.dispatching.outputA,
    outputB: en.dispatching.outputB,
  },
  oss: {
    eyebrow: "código abierto",
    title: "¿Encontraste un bug? ¿Querés ayudar?",
    issue: "abrir un issue",
    contributing: "leer CONTRIBUTING",
  },
  footer: { license: "licencia MIT", github: "github", docs: "docs" },
  notFound: { title: "Página no encontrada", back: "volver a la documentación" },
};

const dictionaries: Record<Lang, Dictionary> = { en, es };

export function dictionary(lang: string): Dictionary {
  return dictionaries[lang as Lang] ?? dictionaries[i18n.defaultLanguage];
}

/** URL prefix of a locale: English is unprefixed, Spanish lives under /es. */
export function localePrefix(lang: string): string {
  return lang === i18n.defaultLanguage ? "" : `/${lang}`;
}
