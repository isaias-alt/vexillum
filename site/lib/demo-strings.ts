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

export type FrameKind = "prompt" | "reply" | "tool" | "out" | "hook";

export interface DemoFrame {
  t: number;
  kind: FrameKind;
  text: string;
}

export interface DemoStep {
  title: string;
  blurb: string;
  /** Docs page the step links to, relative to the docs root. */
  docs: string;
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
const LAND_RELEASED = "released: camp returned to the pool, herdr pane closed.";

const SHIP_CMD = `Bash(vx ship ${C})`;
const SHIP_STEPS = ["[ok] lint", "[ok] tests", "[ok] review", "[ok] docs"];
const SHIP_OUT = `tribunal passed, pushed vexillum/${C}, pull request: https://github.com/acme/shop/pull/214`;

const DOCTOR_CMD = "Bash(vx doctor)";
const DOCTOR_OUT = [
  "[ok] Claude Code",
  "[ok] herdr",
  "[ok] GitHub CLI (gh)",
  "[ok] project initialized",
  "[ok] skill vexillum - installed, current",
  "[ok] sentinel Stop hook - registered in .claude/settings.json",
  "Environment ready.",
];

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
        { t: 2900, kind: "out", text: LAND_RELEASED },
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
    {
      title: "Doctor",
      blurb:
        "One read-only check that the tools, the project and the sentinel hook are all in place.",
      docs: "/reference/cli/doctor",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "is everything wired up right?",
        },
        {
          t: 900,
          kind: "reply",
          text: "Let me check the camp, general.",
        },
        { t: 1700, kind: "tool", text: DOCTOR_CMD },
        { t: 2400, kind: "out", text: DOCTOR_OUT[0] },
        { t: 2600, kind: "out", text: DOCTOR_OUT[1] },
        { t: 2800, kind: "out", text: DOCTOR_OUT[2] },
        { t: 3000, kind: "out", text: DOCTOR_OUT[3] },
        { t: 3200, kind: "out", text: DOCTOR_OUT[4] },
        { t: 3400, kind: "out", text: DOCTOR_OUT[5] },
        { t: 3700, kind: "out", text: DOCTOR_OUT[6] },
        {
          t: 4700,
          kind: "reply",
          text: "All clear, general. Every required tool is in place and the sentinel hook will wake me when a soldier settles. The troop is ready to march.",
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
        { t: 2900, kind: "out", text: LAND_RELEASED },
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
    {
      title: "Doctor",
      blurb:
        "Un chequeo de solo lectura de que las herramientas, el proyecto y el hook del sentinel están en su lugar.",
      docs: "/reference/cli/doctor",
      frames: [
        {
          t: 0,
          kind: "prompt",
          text: "¿está todo bien configurado?",
        },
        {
          t: 800,
          kind: "reply",
          text: "Reviso el campamento, general.",
        },
        { t: 1600, kind: "tool", text: DOCTOR_CMD },
        { t: 2300, kind: "out", text: DOCTOR_OUT[0] },
        { t: 2500, kind: "out", text: DOCTOR_OUT[1] },
        { t: 2700, kind: "out", text: DOCTOR_OUT[2] },
        { t: 2900, kind: "out", text: DOCTOR_OUT[3] },
        { t: 3100, kind: "out", text: DOCTOR_OUT[4] },
        { t: 3300, kind: "out", text: DOCTOR_OUT[5] },
        { t: 3600, kind: "out", text: DOCTOR_OUT[6] },
        {
          t: 4600,
          kind: "reply",
          text: "Todo en orden, general. Cada herramienta requerida está en su lugar y el hook del sentinel me despierta cuando un soldier termina. La tropa está lista para marchar.",
        },
      ],
    },
  ],
};

export const demoCopy = { en, es };
