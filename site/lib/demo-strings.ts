// Copy of the landing's "see it in action" section (components/InAction.tsx):
// five hand-written scripts of a commander session, in English and Spanish.
//
// A frame is one line of the session and `t` is the millisecond, counted from
// the start of its step, at which it appears. A `prompt` is typed one
// character at a time (InAction's CHAR_MS), so the next frame must start after
// it has finished typing. The tool and output lines are the real stdout of the
// vx commands (the same in both languages), and the replies are the commander
// speaking in the tone the product skill gives it: it calls the user "general"
// and keeps technical terms in English.
//
// A `shot` frame is not a line of the transcript: it swaps the screenshot shown
// in the browser window next to it (a real screenshot of the real UI, kept in
// public/demo). A step with no shot frames has no browser window at all.

export type FrameKind = "prompt" | "reply" | "tool" | "out" | "hook" | "shot";

export interface DemoFrame {
  t: number;
  kind: FrameKind;
  /** The line of the session, or for a `shot` the image path under public/. */
  text: string;
  /** Alt text of a `shot`. */
  alt?: string;
}

export interface DemoStep {
  title: string;
  blurb: string;
  /** Docs page the step links to, relative to the docs root. */
  docs: string;
  /** Address shown in the browser window that displays the step's shots. */
  browserUrl?: string;
  frames: DemoFrame[];
}

export interface DemoCopy {
  eyebrow: string;
  title: string;
  sub: string;
  replay: string;
  paneTitle: string;
  steps: DemoStep[];
}

const A = "7c1e9a40d3b2f658";
const B = "e41b07d9a52c3f86";
const C = "b93d5e1a7f0c2468";

const DISPATCH_MISSION =
  'Bash(vx dispatch "refactor the payment module" --kind mission --model sonnet)';
const DISPATCH_SCOUT = 'Bash(vx dispatch "trace the memory leak" --kind scout)';
const OUT_MISSION = `task_id=${A} kind=mission camp_slot=1`;
const OUT_MISSION_RUNNING = "still running past the quick-settle probe";
const OUT_SCOUT = `task_id=${B} kind=scout camp_slot=2`;

const WAKE_HOOK = "Stop hook: vx sentinel";
const WAKE_HEADER = "A vexillum soldier's status changed:";
const WAKE_LINE = `- scout ${B}: running -> done`;
const STATUS_MISSION = `${A}  mission running     vexillum/${A} refactor the payment module`;
const STATUS_SCOUT = `${B}  scout   done        vexillum/${B} trace the memory leak`;

const LAND_CMD = `Bash(vx land ${A})`;
const LAND_OUT = `landed: fast-forwarded ~/project to vexillum/${A}`;
const LAND_STRUCK = "struck: camp returned to the pool, herdr pane closed.";

const SHIP_CMD = `Bash(vx ship ${C})`;
const SHIP_STEPS = ["[ok] lint", "[ok] tests", "[ok] review", "[ok] docs"];
const SHIP_OUT = `tribunal passed, pushed vexillum/${C}, pull request: https://github.com/acme/shop/pull/214`;

const FORUM_CMD = "Bash(vx forum .vexillum/forum/leak-fix.html)";
const FORUM_OUT = [
  "status: open",
  "url: http://127.0.0.1:51237/session/83b329918b347776",
  "listener: running",
];
const FORUM_HOOK_HEADER = "Forum feedback is waiting for you:";
const FORUM_HOOK_LINE =
  "forum session /Users/dev/project/.vexillum/forum/leak-fix.html: 1 new message (ended: false). Run vx forum inbox.";
const INBOX_CMD = "Bash(vx forum inbox)";
const INBOX_OUT = [
  "unread_prompts: 1",
  "status: feedback",
  "prompts[1]:",
  "  - uid: pr_5d2a91c40be3f817",
  "    tag: decision",
  "    prompt: |",
  "      Fix for the leak: Close the sockets in a finally block",
  "      Context data:",
  "      {",
  '        "question": "leak-fix",',
  '        "answer": "Close the sockets in a finally block"',
  "      }",
];
const FORUM_URL = "127.0.0.1:51237/session/83b329918b347776";

const SHOT_ALT = {
  en: [
    "The forum page open in the browser: three options for the connection leak fix, A, B and C, and an empty conversation panel.",
    "Option A, close the sockets in a finally block, picked and queued, with the Send to Agent button active.",
    "The same page after the answer: the pick is marked answered in round 1 and the commander's reply appears in the conversation panel.",
  ],
  es: [
    "La página de forum abierta en el navegador: tres opciones para arreglar la fuga de conexiones, A, B y C, y el panel de conversación vacío.",
    "La opción A, cerrar los sockets en un bloque finally, elegida y en cola, con el botón Send to Agent activo.",
    "La misma página tras la respuesta: la elección figura respondida en la ronda 1 y la respuesta del commander aparece en el panel de conversación.",
  ],
};

function shot(lang: "en" | "es", n: 0 | 1 | 2, t: number): DemoFrame {
  const name = ["1-open", "2-picked", "3-answered"][n];
  return {
    t,
    kind: "shot",
    text: `/demo/forum-${name}-${lang}.webp`,
    alt: SHOT_ALT[lang][n],
  };
}

const en: DemoCopy = {
  eyebrow: "see it in action",
  title: "The whole loop, as a conversation.",
  sub: "Five moments from a commander session in Claude Code. Click one, or just watch.",
  replay: "replay",
  paneTitle: "~/project - commander",
  steps: [
    {
      title: "Dispatch",
      blurb:
        "Ask for work in plain words. The commander sends a mission and a scout, each into its own camp.",
      docs: "/concepts/missions-and-scouts",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "dispatch a soldier to refactor the payment module, and a scout to trace the memory leak",
        },
        {
          t: 2300,
          kind: "reply",
          text: "On it, general. One mission and one scout, coming up.",
        },
        { t: 3200, kind: "tool", text: DISPATCH_MISSION },
        { t: 3900, kind: "out", text: OUT_MISSION },
        { t: 4100, kind: "out", text: OUT_MISSION_RUNNING },
        { t: 5000, kind: "tool", text: DISPATCH_SCOUT },
        { t: 5700, kind: "out", text: OUT_SCOUT },
        {
          t: 6700,
          kind: "reply",
          text: "Both soldiers are in the field, general. The mission works in camp 1, the scout in camp 2 (it only investigates, it won't touch your code). Keep talking to me, I'll hear from the sentinel when either one settles.",
        },
      ],
    },
    {
      title: "Sentinel wakes",
      blurb:
        "The sentinel watches in the background and wakes the commander only when a soldier settles.",
      docs: "/concepts/sentinel",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "I'm stepping away, tell me when something settles",
        },
        {
          t: 1500,
          kind: "reply",
          text: "Understood, general. The sentinel is on watch and I'll stay quiet until a soldier settles.",
        },
        { t: 3600, kind: "hook", text: WAKE_HOOK },
        { t: 4200, kind: "out", text: WAKE_HEADER },
        { t: 4400, kind: "out", text: WAKE_LINE },
        { t: 5400, kind: "tool", text: "Bash(vx status)" },
        { t: 6100, kind: "out", text: STATUS_MISSION },
        { t: 6300, kind: "out", text: STATUS_SCOUT },
        {
          t: 7300,
          kind: "reply",
          text: "The scout is back, general. The leak is in the connection pool: PaymentClient never closes its sockets on retry. Want me to send a mission to fix it?",
        },
      ],
    },
    {
      title: "Forum",
      blurb:
        "A page opens in your browser, you answer with a click, and your answer wakes the commander.",
      docs: "/guides/forum",
      browserUrl: FORUM_URL,
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "show me the options for the leak fix, I want to pick one",
        },
        {
          t: 1700,
          kind: "reply",
          text: "Opening a page with the three options, general.",
        },
        { t: 2700, kind: "tool", text: FORUM_CMD },
        { t: 3400, kind: "out", text: FORUM_OUT[0] },
        shot("en", 0, 3400),
        { t: 3600, kind: "out", text: FORUM_OUT[1] },
        { t: 3800, kind: "out", text: FORUM_OUT[2] },
        {
          t: 4800,
          kind: "reply",
          text: "The page is open, general. Pick one there and press Send to Agent.",
        },
        shot("en", 1, 5000),
        { t: 6800, kind: "hook", text: WAKE_HOOK },
        { t: 7400, kind: "out", text: FORUM_HOOK_HEADER },
        { t: 7600, kind: "out", text: FORUM_HOOK_LINE },
        { t: 8600, kind: "tool", text: INBOX_CMD },
        { t: 9300, kind: "out", text: INBOX_OUT[0] },
        { t: 9500, kind: "out", text: INBOX_OUT[1] },
        { t: 9700, kind: "out", text: INBOX_OUT[2] },
        { t: 9900, kind: "out", text: INBOX_OUT[3] },
        { t: 10100, kind: "out", text: INBOX_OUT[4] },
        { t: 10300, kind: "out", text: INBOX_OUT[5] },
        { t: 10500, kind: "out", text: INBOX_OUT[6] },
        { t: 10700, kind: "out", text: INBOX_OUT[7] },
        { t: 10900, kind: "out", text: INBOX_OUT[8] },
        { t: 11100, kind: "out", text: INBOX_OUT[9] },
        { t: 11300, kind: "out", text: INBOX_OUT[10] },
        { t: 11500, kind: "out", text: INBOX_OUT[11] },
        {
          t: 12700,
          kind: "reply",
          text: "Got it, general, the finally block. Sending a mission to camp 3.",
        },
        shot("en", 2, 12900),
      ],
    },
    {
      title: "Land",
      blurb:
        "A finished mission fast-forwards your checkout and its camp goes back to the pool.",
      docs: "/guides/land-vs-ship#land",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "the refactor looks good, land it",
        },
        {
          t: 1200,
          kind: "reply",
          text: "Landing it now, general.",
        },
        { t: 2000, kind: "tool", text: LAND_CMD },
        { t: 2700, kind: "out", text: LAND_OUT },
        { t: 2900, kind: "out", text: LAND_STRUCK },
        {
          t: 3900,
          kind: "reply",
          text: "Landed, general. The refactor is in your checkout and camp 1 is free for the next soldier.",
        },
      ],
    },
    {
      title: "Ship",
      blurb:
        "Ship runs lint, tests, an adversarial review and a docs check, then opens a real pull request.",
      docs: "/guides/land-vs-ship#ship",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "the leak fix is done too, ship it as a pull request",
        },
        {
          t: 1500,
          kind: "reply",
          text: "Sending it through the tribunal, general. Nothing leaves the machine until every step passes.",
        },
        { t: 3000, kind: "tool", text: SHIP_CMD },
        { t: 3800, kind: "out", text: SHIP_STEPS[0] },
        { t: 4200, kind: "out", text: SHIP_STEPS[1] },
        { t: 4900, kind: "out", text: SHIP_STEPS[2] },
        { t: 5300, kind: "out", text: SHIP_STEPS[3] },
        { t: 5800, kind: "out", text: SHIP_OUT },
        {
          t: 6800,
          kind: "reply",
          text: "Pull request 214 is open, general, and it passed lint, tests, the adversarial review and the docs check. The soldier stays in its camp until you merge it, in case the review asks for changes.",
        },
      ],
    },
  ],
};

const es: DemoCopy = {
  eyebrow: "mirá cómo funciona",
  title: "Todo el ciclo, como una conversación.",
  sub: "Cinco momentos de una sesión con el commander en Claude Code. Hacé clic en uno, o simplemente mirá.",
  replay: "repetir",
  paneTitle: "~/project - commander",
  steps: [
    {
      title: "Despachar",
      blurb:
        "Pedí el trabajo con palabras simples. El commander manda una mission y un scout, cada uno a su propio camp.",
      docs: "/concepts/missions-and-scouts",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "despachá un soldier para refactorizar el módulo de pagos y un scout para rastrear la pérdida de memoria",
        },
        {
          t: 2500,
          kind: "reply",
          text: "A la orden, general. Una mission y un scout en camino.",
        },
        { t: 3400, kind: "tool", text: DISPATCH_MISSION },
        { t: 4100, kind: "out", text: OUT_MISSION },
        { t: 4300, kind: "out", text: OUT_MISSION_RUNNING },
        { t: 5200, kind: "tool", text: DISPATCH_SCOUT },
        { t: 5900, kind: "out", text: OUT_SCOUT },
        {
          t: 6900,
          kind: "reply",
          text: "Los dos soldiers están en el campo, general. La mission trabaja en el camp 1 y el scout en el camp 2 (solo investiga, no toca tu código). Seguí hablándome, el sentinel me avisa cuando alguno termine.",
        },
      ],
    },
    {
      title: "El sentinel avisa",
      blurb:
        "El sentinel vigila en segundo plano y despierta al commander solo cuando un soldier termina.",
      docs: "/concepts/sentinel",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "me voy un rato, avisame cuando algo termine",
        },
        {
          t: 1400,
          kind: "reply",
          text: "Entendido, general. El sentinel está de guardia y me quedo callado hasta que un soldier termine.",
        },
        { t: 3600, kind: "hook", text: WAKE_HOOK },
        { t: 4200, kind: "out", text: WAKE_HEADER },
        { t: 4400, kind: "out", text: WAKE_LINE },
        { t: 5400, kind: "tool", text: "Bash(vx status)" },
        { t: 6100, kind: "out", text: STATUS_MISSION },
        { t: 6300, kind: "out", text: STATUS_SCOUT },
        {
          t: 7300,
          kind: "reply",
          text: "El scout volvió, general. La pérdida está en el connection pool: PaymentClient nunca cierra sus sockets al reintentar. ¿Mando una mission para arreglarlo?",
        },
      ],
    },
    {
      title: "Forum",
      blurb:
        "Se abre una página en tu navegador, respondés con un clic y tu respuesta despierta al commander.",
      docs: "/guides/forum",
      browserUrl: FORUM_URL,
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "mostrame las opciones para arreglar la pérdida, quiero elegir una",
        },
        {
          t: 1900,
          kind: "reply",
          text: "Abro una página con las tres opciones, general.",
        },
        { t: 2900, kind: "tool", text: FORUM_CMD },
        { t: 3600, kind: "out", text: FORUM_OUT[0] },
        shot("es", 0, 3600),
        { t: 3800, kind: "out", text: FORUM_OUT[1] },
        { t: 4000, kind: "out", text: FORUM_OUT[2] },
        {
          t: 5000,
          kind: "reply",
          text: "La página está abierta, general. Elegí una ahí y apretá Send to Agent.",
        },
        shot("es", 1, 5200),
        { t: 7000, kind: "hook", text: WAKE_HOOK },
        { t: 7600, kind: "out", text: FORUM_HOOK_HEADER },
        { t: 7800, kind: "out", text: FORUM_HOOK_LINE },
        { t: 8800, kind: "tool", text: INBOX_CMD },
        { t: 9500, kind: "out", text: INBOX_OUT[0] },
        { t: 9700, kind: "out", text: INBOX_OUT[1] },
        { t: 9900, kind: "out", text: INBOX_OUT[2] },
        { t: 10100, kind: "out", text: INBOX_OUT[3] },
        { t: 10300, kind: "out", text: INBOX_OUT[4] },
        { t: 10500, kind: "out", text: INBOX_OUT[5] },
        { t: 10700, kind: "out", text: INBOX_OUT[6] },
        { t: 10900, kind: "out", text: INBOX_OUT[7] },
        { t: 11100, kind: "out", text: INBOX_OUT[8] },
        { t: 11300, kind: "out", text: INBOX_OUT[9] },
        { t: 11500, kind: "out", text: INBOX_OUT[10] },
        { t: 11700, kind: "out", text: INBOX_OUT[11] },
        {
          t: 12900,
          kind: "reply",
          text: "Entendido, general, el bloque finally. Mando una mission al camp 3.",
        },
        shot("es", 2, 13100),
      ],
    },
    {
      title: "Land",
      blurb:
        "Una mission terminada avanza tu checkout con fast-forward y su camp vuelve al pool.",
      docs: "/guides/land-vs-ship#land",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "el refactor está bien, hacé el land",
        },
        {
          t: 1200,
          kind: "reply",
          text: "Aterrizando, general.",
        },
        { t: 2000, kind: "tool", text: LAND_CMD },
        { t: 2700, kind: "out", text: LAND_OUT },
        { t: 2900, kind: "out", text: LAND_STRUCK },
        {
          t: 3900,
          kind: "reply",
          text: "Aterrizó, general. El refactor ya está en tu checkout y el camp 1 queda libre para el próximo soldier.",
        },
      ],
    },
    {
      title: "Ship",
      blurb:
        "Ship corre lint, tests, una review adversarial y un chequeo de docs, y después abre un pull request real.",
      docs: "/guides/land-vs-ship#ship",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "el fix de la pérdida también está listo, hacé ship como pull request",
        },
        {
          t: 1700,
          kind: "reply",
          text: "Lo paso por el tribunal, general. Nada sale de la máquina hasta que pase cada paso.",
        },
        { t: 3100, kind: "tool", text: SHIP_CMD },
        { t: 3900, kind: "out", text: SHIP_STEPS[0] },
        { t: 4300, kind: "out", text: SHIP_STEPS[1] },
        { t: 5000, kind: "out", text: SHIP_STEPS[2] },
        { t: 5400, kind: "out", text: SHIP_STEPS[3] },
        { t: 5900, kind: "out", text: SHIP_OUT },
        {
          t: 6900,
          kind: "reply",
          text: "El pull request 214 está abierto, general, y pasó lint, tests, la review adversarial y el chequeo de docs. El soldier queda en su camp hasta que lo mergees, por si la review pide cambios.",
        },
      ],
    },
  ],
};

export const demoCopy = { en, es };
