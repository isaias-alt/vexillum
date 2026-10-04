// Copy of the landing's "command loop" section (components/CommandLoop.tsx and
// components/DetailDiagrams.tsx): the labels drawn inside the SVGs, which have
// to stay short enough for their boxes, and the captions around them.
//
// Boxes are sized for these strings at JetBrains Mono's 0.6em advance, so a
// longer translation needs a wider box in the component, not a smaller font.

export interface LoopCopy {
  eyebrow: string;
  title: string;
  sub: string;
  /** One caption per numbered badge of the main diagram, in order. */
  steps: string[];
  alt: string;
  labels: {
    general: string;
    generalSub: string;
    commander: string;
    commanderSub: string;
    camp: string;
    campN: string;
    campSub: string;
    soldier: string;
    mission: string;
    scout: string;
    ask: string;
    report: string;
    dispatch: string;
    sentinel: string;
    sentinelSub: string;
    polls: string;
    wakeA: string;
    wakeB: string;
    wakeNote: string;
  };
  details: {
    land: {
      title: string;
      sub: string;
      alt: string;
      labels: {
        done: string;
        land: string;
        ship: string;
        reviewTitle: string;
        reviewSub: string;
        ffTitle: string;
        ffSub: string;
        tribunalTitle: string;
        tribunalA: string;
        tribunalB: string;
        prTitle: string;
        prSub: string;
      };
    };
    state: {
      title: string;
      sub: string;
      alt: string;
      labels: {
        sentinel: string;
        commander: string;
        before: string;
        restart: string;
        reconciled: string;
        diskTitle: string;
        diskSub: string;
      };
    };
  };
}

const en: LoopCopy = {
  eyebrow: "the command loop",
  title: "How one prompt becomes many soldiers.",
  sub: "You talk to one commander. It dispatches soldiers into isolated camps, and a sentinel watches them in the background so the commander only wakes when one settles.",
  steps: [
    "You tell the commander what you want.",
    "It dispatches soldiers with vx dispatch, one per task.",
    "Each soldier works alone in its own camp, a git worktree.",
    "The sentinel polls every camp in the background.",
    "When a soldier settles (done, blocked or interrupted) the sentinel wakes the commander.",
    "The commander reports to you, or asks whether to vx land or vx ship.",
  ],
  alt: "Diagram of the command loop. The general talks to the commander, which dispatches soldiers into camps. A sentinel polls the camps and wakes the commander when a soldier settles. The commander then reports, or decides to land or ship.",
  labels: {
    general: "general",
    generalSub: "you",
    commander: "commander",
    commanderSub: "Claude Code",
    camp: "camp",
    campN: "camp N",
    campSub: "own worktree",
    soldier: "soldier",
    mission: "mission",
    scout: "scout",
    ask: "ask",
    report: "report",
    dispatch: "vx dispatch",
    sentinel: "sentinel",
    sentinelSub: "in the background",
    polls: "polls",
    wakeA: "wakes the commander",
    wakeB: "when a soldier settles",
    wakeNote: "done, blocked or interrupted",
  },
  details: {
    land: {
      title: "Two ways out of a camp.",
      sub: "Land stays on your machine. Ship runs the tribunal and opens a real pull request.",
      alt: "A finished mission leaves its camp one of two ways. vx land: you review it yourself, then your checkout is fast-forwarded. vx ship: the tribunal runs lint, tests, review and docs, then the branch is pushed and a pull request is opened.",
      labels: {
        done: "mission done",
        land: "vx land",
        ship: "vx ship",
        reviewTitle: "you review",
        reviewSub: "no pipeline",
        ffTitle: "fast-forward",
        ffSub: "your checkout",
        tribunalTitle: "tribunal",
        tribunalA: "lint · tests",
        tribunalB: "review · docs",
        prTitle: "push + open PR",
        prSub: "reaches GitHub",
      },
    },
    state: {
      title: "Restart-proof state.",
      sub: "Everything lives on disk in ~/.vexillum, written atomically. Kill the commander or the sentinel and the next one picks up where it stopped.",
      alt: "The sentinel and the commander can restart at any time. Each new process is reconciled from the state on disk in ~/.vexillum, which holds tasks and wakes as atomically written JSON.",
      labels: {
        sentinel: "sentinel",
        commander: "commander",
        before: "before",
        restart: "restart",
        reconciled: "reconciled from disk",
        diskTitle: "~/.vexillum",
        diskSub: "tasks and wakes, atomic JSON",
      },
    },
  },
};

const es: LoopCopy = {
  eyebrow: "el ciclo de mando",
  title: "Cómo un prompt se vuelve muchos soldiers.",
  sub: "Le hablás a un solo commander. Él despacha soldiers a camps aislados y un sentinel los vigila en segundo plano, así el commander despierta solo cuando uno termina.",
  steps: [
    "Le decís al commander lo que querés.",
    "Despacha soldiers con vx dispatch, uno por tarea.",
    "Cada soldier trabaja solo en su propio camp, un git worktree.",
    "El sentinel sondea cada camp en segundo plano.",
    "Cuando un soldier termina (listo, bloqueado o interrumpido) el sentinel despierta al commander.",
    "El commander te reporta, o te pregunta si hace vx land o vx ship.",
  ],
  alt: "Diagrama del ciclo de mando. El general le habla al commander, que despacha soldiers a camps. Un sentinel sondea los camps y despierta al commander cuando un soldier termina. Después el commander reporta, o decide hacer land o ship.",
  labels: {
    general: "general",
    generalSub: "vos",
    commander: "commander",
    commanderSub: "Claude Code",
    camp: "camp",
    campN: "camp N",
    campSub: "worktree propio",
    soldier: "soldier",
    mission: "mission",
    scout: "scout",
    ask: "pedido",
    report: "reporte",
    dispatch: "vx dispatch",
    sentinel: "sentinel",
    sentinelSub: "en segundo plano",
    polls: "sondea",
    wakeA: "despierta al commander",
    wakeB: "al terminar un soldier",
    wakeNote: "listo, bloqueado o interrumpido",
  },
  details: {
    land: {
      title: "Dos formas de salir de un camp.",
      sub: "Land se queda en tu máquina. Ship corre el tribunal y abre un pull request real.",
      alt: "Una mission terminada sale de su camp de una de dos formas. vx land: la revisás vos y se avanza tu checkout con fast-forward. vx ship: el tribunal corre lint, tests, review y docs, y después se hace push de la rama y se abre un pull request.",
      labels: {
        done: "mission lista",
        land: "vx land",
        ship: "vx ship",
        reviewTitle: "revisás vos",
        reviewSub: "sin pipeline",
        ffTitle: "fast-forward",
        ffSub: "tu checkout",
        tribunalTitle: "tribunal",
        tribunalA: "lint · tests",
        tribunalB: "review · docs",
        prTitle: "push + abrir PR",
        prSub: "llega a GitHub",
      },
    },
    state: {
      title: "Estado a prueba de reinicios.",
      sub: "Todo vive en disco, en ~/.vexillum, escrito de forma atómica. Matá al commander o al sentinel y el siguiente retoma donde quedó.",
      alt: "El sentinel y el commander pueden reiniciarse en cualquier momento. Cada proceso nuevo se reconcilia con el estado en disco de ~/.vexillum, que guarda tasks y wakes como JSON escrito de forma atómica.",
      labels: {
        sentinel: "sentinel",
        commander: "commander",
        before: "antes",
        restart: "reinicio",
        reconciled: "reconciliado desde disco",
        diskTitle: "~/.vexillum",
        diskSub: "tasks y wakes, JSON atómico",
      },
    },
  },
};

export const loopCopy = { en, es };
