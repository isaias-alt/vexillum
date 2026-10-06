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
// `view` and `act` frames are not lines of the transcript. A `view` frame
// switches the pane between the terminal and a mock of the forum screen in the
// browser (same box, cross-faded); `act` frames move the pointer in that mock
// and click. A step with no `view` frame has no browser phase at all.

export type FrameKind =
  "prompt" | "reply" | "tool" | "out" | "hook" | "view" | "act";

export interface DemoFrame {
  t: number;
  kind: FrameKind;
  /**
   * The line of the session. For a `view` frame, the pane to show: "terminal"
   * or "browser". For an `act` frame, what the pointer does in the browser:
   * "move:<target>" or "click:<target>" (target: option, queue or send).
   */
  text: string;
}

/**
 * The forum screen of a step (components/ForumMock.tsx). The forum's own chrome
 * (Annotate, Conversation, Send to Agent...) stays English, it is the product's
 * UI; only the artifact the commander wrote and its decision are translated.
 */
export interface BrowserCopy {
  url: string;
  file: string;
  title: string;
  pickTag: string;
  intro: string;
  cards: { title: string; badge?: string; text: string }[];
  question: string;
  choices: string[];
  queueLabel: string;
  /** What the picked option sends: the queued card and the "You" message. */
  decision: string;
  /** The message typed in the box and queued before the pick. */
  message: string;
}

export interface DemoStep {
  title: string;
  blurb: string;
  /** Docs page the step links to, relative to the docs root. */
  docs: string;
  /** What the browser view of the step shows (only for a step with `view` frames). */
  browser?: BrowserCopy;
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
  "forum session /Users/dev/project/.vexillum/forum/leak-fix.html: 2 new messages (ended: false). Run vx forum inbox.";
const INBOX_CMD = "Bash(vx forum inbox)";
// What `vx forum inbox` prints for the two queued prompts: the typed message
// first (tag "message"), then the decision (tag "decision"), whose prompt block
// carries the decision text, "Context data:" and the pretty-printed JSON of
// the form. Shortened: the note and shown_prompts lines are left out.
function inboxOut(message: string, decision: string, answer: string) {
  return [
    "unread_prompts: 2",
    "status: feedback",
    "prompts[2]:",
    "  - uid: pr_a41c7e02d93b6f58",
    "    tag: message",
    `    prompt: ${message}`,
    "  - uid: pr_5d2a91c40be3f817",
    "    tag: decision",
    "    prompt: |",
    `      ${decision}`,
    "      Context data:",
    "      {",
    '        "question": "leak-fix",',
    `        "answer": "${answer}"`,
    "      }",
  ];
}

const inboxEn = inboxOut(
  "keep the change as small as possible",
  "Fix for the leak: Close the sockets in a finally block",
  "Close the sockets in a finally block",
);
const inboxEs = inboxOut(
  "que el cambio sea lo más chico posible",
  "Arreglo para la fuga: Cerrar los sockets en un bloque finally",
  "Cerrar los sockets en un bloque finally",
);

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
      browser: {
        url: "127.0.0.1:47125",
        file: "leak-fix.html",
        title: "Fix options: the connection leak",
        pickTag: "pick one",
        intro:
          "The scout traced the leak to <code>PaymentClient</code>: on a retry it opens a new socket and never closes the old one. Three ways to fix it, from smallest to largest.",
        cards: [
          {
            title: "A. Close in finally",
            badge: "recommended",
            text: "Close the old socket in a finally.",
          },
          {
            title: "B. Pool with max age",
            text: "A small pool that retires old sockets.",
          },
          {
            title: "C. Reuse one session",
            text: "One session for every retry.",
          },
        ],
        question: "Which fix should the soldier build?",
        choices: [
          "A. Close the sockets in a finally block",
          "B. Pool with a max age",
          "C. Reuse one session",
        ],
        queueLabel: "Queue my pick",
        decision: "Fix for the leak: Close the sockets in a finally block",
        message: "keep the change as small as possible",
      },
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
        { t: 3600, kind: "out", text: FORUM_OUT[1] },
        { t: 3800, kind: "out", text: FORUM_OUT[2] },
        {
          t: 4800,
          kind: "reply",
          text: "The page is open, general. Pick one there and press Send to Agent.",
        },
        { t: 5800, kind: "view", text: "browser" },
        { t: 6500, kind: "act", text: "move:input" },
        { t: 7500, kind: "act", text: "click:input" },
        { t: 7700, kind: "act", text: "type:input" },
        { t: 9000, kind: "act", text: "move:addq" },
        { t: 10000, kind: "act", text: "click:addq" },
        { t: 10900, kind: "act", text: "move:option" },
        { t: 11900, kind: "act", text: "click:option" },
        { t: 12700, kind: "act", text: "move:queue" },
        { t: 13700, kind: "act", text: "click:queue" },
        { t: 14600, kind: "act", text: "move:send" },
        { t: 15600, kind: "act", text: "click:send" },
        { t: 17000, kind: "view", text: "terminal" },
        { t: 17800, kind: "hook", text: WAKE_HOOK },
        { t: 18400, kind: "out", text: FORUM_HOOK_HEADER },
        { t: 18600, kind: "out", text: FORUM_HOOK_LINE },
        { t: 19600, kind: "tool", text: INBOX_CMD },
        { t: 20300, kind: "out", text: inboxEn[0] },
        { t: 20500, kind: "out", text: inboxEn[1] },
        { t: 20700, kind: "out", text: inboxEn[2] },
        { t: 20900, kind: "out", text: inboxEn[3] },
        { t: 21100, kind: "out", text: inboxEn[4] },
        { t: 21300, kind: "out", text: inboxEn[5] },
        { t: 21500, kind: "out", text: inboxEn[6] },
        { t: 21700, kind: "out", text: inboxEn[7] },
        { t: 21900, kind: "out", text: inboxEn[8] },
        { t: 22100, kind: "out", text: inboxEn[9] },
        { t: 22300, kind: "out", text: inboxEn[10] },
        { t: 22500, kind: "out", text: inboxEn[11] },
        { t: 22700, kind: "out", text: inboxEn[12] },
        { t: 22900, kind: "out", text: inboxEn[13] },
        { t: 23100, kind: "out", text: inboxEn[14] },
        {
          t: 24200,
          kind: "reply",
          text: "Got it, general: the finally block, kept as small as possible. Sending a mission to camp 3.",
        },
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
      browser: {
        url: "127.0.0.1:47125",
        file: "leak-fix.html",
        title: "Opciones de arreglo: la fuga de conexiones",
        pickTag: "elegí una",
        intro:
          "El scout rastreó la fuga hasta <code>PaymentClient</code>: en un reintento abre un socket nuevo y nunca cierra el anterior. Tres formas de arreglarlo, de la más chica a la más grande.",
        cards: [
          {
            title: "A. Cerrar en finally",
            badge: "recomendada",
            text: "Cerrar el socket viejo en un finally.",
          },
          {
            title: "B. Pool con edad máxima",
            text: "Un pool chico que retira los sockets viejos.",
          },
          {
            title: "C. Reusar una sesión",
            text: "Una sola sesión en cada reintento.",
          },
        ],
        question: "¿Qué arreglo tiene que construir el soldier?",
        choices: [
          "A. Cerrar los sockets en un bloque finally",
          "B. Pool con edad máxima",
          "C. Reusar una sola sesión",
        ],
        queueLabel: "Poner mi elección en cola",
        decision:
          "Arreglo para la fuga: Cerrar los sockets en un bloque finally",
        message: "que el cambio sea lo más chico posible",
      },
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
        { t: 3800, kind: "out", text: FORUM_OUT[1] },
        { t: 4000, kind: "out", text: FORUM_OUT[2] },
        {
          t: 5000,
          kind: "reply",
          text: "La página está abierta, general. Elegí una ahí y apretá Send to Agent.",
        },
        { t: 6000, kind: "view", text: "browser" },
        { t: 6700, kind: "act", text: "move:input" },
        { t: 7700, kind: "act", text: "click:input" },
        { t: 7900, kind: "act", text: "type:input" },
        { t: 9200, kind: "act", text: "move:addq" },
        { t: 10200, kind: "act", text: "click:addq" },
        { t: 11100, kind: "act", text: "move:option" },
        { t: 12100, kind: "act", text: "click:option" },
        { t: 12900, kind: "act", text: "move:queue" },
        { t: 13900, kind: "act", text: "click:queue" },
        { t: 14800, kind: "act", text: "move:send" },
        { t: 15800, kind: "act", text: "click:send" },
        { t: 17200, kind: "view", text: "terminal" },
        { t: 18000, kind: "hook", text: WAKE_HOOK },
        { t: 18600, kind: "out", text: FORUM_HOOK_HEADER },
        { t: 18800, kind: "out", text: FORUM_HOOK_LINE },
        { t: 19800, kind: "tool", text: INBOX_CMD },
        { t: 20500, kind: "out", text: inboxEs[0] },
        { t: 20700, kind: "out", text: inboxEs[1] },
        { t: 20900, kind: "out", text: inboxEs[2] },
        { t: 21100, kind: "out", text: inboxEs[3] },
        { t: 21300, kind: "out", text: inboxEs[4] },
        { t: 21500, kind: "out", text: inboxEs[5] },
        { t: 21700, kind: "out", text: inboxEs[6] },
        { t: 21900, kind: "out", text: inboxEs[7] },
        { t: 22100, kind: "out", text: inboxEs[8] },
        { t: 22300, kind: "out", text: inboxEs[9] },
        { t: 22500, kind: "out", text: inboxEs[10] },
        { t: 22700, kind: "out", text: inboxEs[11] },
        { t: 22900, kind: "out", text: inboxEs[12] },
        { t: 23100, kind: "out", text: inboxEs[13] },
        { t: 23300, kind: "out", text: inboxEs[14] },
        {
          t: 24400,
          kind: "reply",
          text: "Entendido, general: el bloque finally, con el cambio lo más chico posible. Mando una mission al camp 3.",
        },
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
