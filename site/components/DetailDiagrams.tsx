import type { LoopCopy } from "@/lib/loop-strings";
import { Arrow, ArrowDefs } from "./diagram-parts";

// The two detail diagrams under "The command loop". Both are drawn in a
// 320-unit-wide viewBox, the width of a phone's content column, and capped
// at 380px on wider screens, so their labels render at 11-15px everywhere and
// every box is sized for the longest label it holds in either language.

const TITLE = "fill-text font-serif text-[14px] font-semibold";
const SUB = "fill-text-muted font-mono text-[11px]";
const LINE = "fill-text-secondary font-mono text-[11px]";
const BOX = "fill-surface stroke-border-strong";
const SVG = "mx-auto h-auto w-full max-w-[380px]";

function Card({
  title,
  sub,
  children,
}: {
  title: string;
  sub: string;
  children: React.ReactNode;
}) {
  return (
    <figure className="m-0 flex min-w-0 flex-col rounded-xl border border-border bg-surface p-5 sm:p-6">
      <figcaption>
        <h3 className="mb-2 font-serif text-[20px] leading-tight font-semibold text-text">
          {title}
        </h3>
        <p className="mb-5 text-[13px] leading-[1.7] text-text-secondary">
          {sub}
        </p>
      </figcaption>
      <div className="mt-auto">{children}</div>
    </figure>
  );
}

/** vx land against vx ship: what each does to a finished mission. */
export function LandVsShip({ t }: { t: LoopCopy["details"]["land"] }) {
  const id = "land-ship";
  const l = t.labels;
  return (
    <Card title={t.title} sub={t.sub}>
      <svg viewBox="0 0 320 300" role="img" aria-label={t.alt} className={SVG}>
        <ArrowDefs id={id} />

        <rect
          x="100"
          y="4"
          width="120"
          height="36"
          rx="10"
          className={BOX}
          strokeWidth="1.5"
        />
        <text x="160" y="27" textAnchor="middle" className={TITLE}>
          {l.done}
        </text>

        <Arrow id={id} d="M160 40 V58 H82 V76" />
        <Arrow id={id} d="M160 58 H238 V76" />

        {/* the two ways out */}
        <rect
          x="8"
          y="78"
          width="148"
          height="28"
          rx="8"
          strokeWidth="1.5"
          className="fill-accent-soft stroke-accent"
        />
        <text
          x="82"
          y="96"
          textAnchor="middle"
          className="fill-accent font-mono text-[12px] font-medium"
        >
          {l.land}
        </text>
        <rect
          x="164"
          y="78"
          width="148"
          height="28"
          rx="8"
          strokeWidth="1.5"
          className="fill-accent-soft stroke-accent"
        />
        <text
          x="238"
          y="96"
          textAnchor="middle"
          className="fill-accent font-mono text-[12px] font-medium"
        >
          {l.ship}
        </text>

        <Arrow id={id} d="M82 106 V120" />
        <Arrow id={id} d="M238 106 V120" />

        {/* land: you review it yourself */}
        <rect
          x="8"
          y="122"
          width="148"
          height="96"
          rx="10"
          className={BOX}
          strokeWidth="1.5"
        />
        <text x="82" y="166" textAnchor="middle" className={TITLE}>
          {l.reviewTitle}
        </text>
        <text x="82" y="184" textAnchor="middle" className={SUB}>
          {l.reviewSub}
        </text>

        {/* ship: the tribunal */}
        <rect
          x="164"
          y="122"
          width="148"
          height="96"
          rx="10"
          className={BOX}
          strokeWidth="1.5"
        />
        <text x="238" y="150" textAnchor="middle" className={TITLE}>
          {l.tribunalTitle}
        </text>
        <text x="238" y="171" textAnchor="middle" className={LINE}>
          {l.tribunalA}
        </text>
        <text x="238" y="189" textAnchor="middle" className={LINE}>
          {l.tribunalB}
        </text>

        <Arrow id={id} d="M82 218 V236" />
        <Arrow id={id} d="M238 218 V236" />

        {/* the outcome */}
        <rect
          x="8"
          y="238"
          width="148"
          height="56"
          rx="10"
          className={BOX}
          strokeWidth="1.5"
        />
        <text x="82" y="263" textAnchor="middle" className={TITLE}>
          {l.ffTitle}
        </text>
        <text x="82" y="281" textAnchor="middle" className={SUB}>
          {l.ffSub}
        </text>
        <rect
          x="164"
          y="238"
          width="148"
          height="56"
          rx="10"
          className="fill-surface stroke-bronze"
          strokeWidth="1.5"
        />
        <text x="238" y="263" textAnchor="middle" className={TITLE}>
          {l.prTitle}
        </text>
        <text x="238" y="281" textAnchor="middle" className={SUB}>
          {l.prSub}
        </text>
      </svg>
    </Card>
  );
}

/** Restart-proof state: a new process is rebuilt from the files on disk. */
export function RestartProof({ t }: { t: LoopCopy["details"]["state"] }) {
  const id = "restart";
  const l = t.labels;
  const ghost = "fill-sunken stroke-border-strong";
  return (
    <Card title={t.title} sub={t.sub}>
      <svg viewBox="0 0 320 256" role="img" aria-label={t.alt} className={SVG}>
        <ArrowDefs id={id} />

        {/* the processes before the restart */}
        <rect
          x="8"
          y="4"
          width="140"
          height="44"
          rx="10"
          strokeWidth="1.5"
          strokeDasharray="5 4"
          className={ghost}
        />
        <text
          x="78"
          y="23"
          textAnchor="middle"
          className="fill-text-muted font-serif text-[14px] font-semibold"
        >
          {l.sentinel}
        </text>
        <text x="78" y="39" textAnchor="middle" className={SUB}>
          {l.before}
        </text>
        <rect
          x="172"
          y="4"
          width="140"
          height="44"
          rx="10"
          strokeWidth="1.5"
          strokeDasharray="5 4"
          className={ghost}
        />
        <text
          x="242"
          y="23"
          textAnchor="middle"
          className="fill-text-muted font-serif text-[14px] font-semibold"
        >
          {l.commander}
        </text>
        <text x="242" y="39" textAnchor="middle" className={SUB}>
          {l.before}
        </text>

        {/* restart */}
        <Arrow id={id} d="M60 48 V90" tone="muted" dashed />
        <Arrow id={id} d="M260 48 V90" tone="muted" dashed />
        <text x="160" y="73" textAnchor="middle" className={LINE}>
          {l.restart}
        </text>

        {/* the new processes */}
        <rect
          x="8"
          y="92"
          width="140"
          height="44"
          rx="10"
          className="fill-surface stroke-bronze"
          strokeWidth="2"
        />
        <text x="78" y="119" textAnchor="middle" className={TITLE}>
          {l.sentinel}
        </text>
        <rect
          x="172"
          y="92"
          width="140"
          height="44"
          rx="10"
          className="fill-surface stroke-accent"
          strokeWidth="2"
        />
        <text x="242" y="119" textAnchor="middle" className={TITLE}>
          {l.commander}
        </text>

        {/* reconciled from disk */}
        <Arrow id={id} d="M60 196 V140" />
        <Arrow id={id} d="M260 196 V140" />
        <text
          x="160"
          y="171"
          textAnchor="middle"
          className="fill-accent font-mono text-[11px]"
        >
          {l.reconciled}
        </text>

        {/* the state on disk */}
        <rect
          x="8"
          y="198"
          width="304"
          height="52"
          rx="10"
          strokeWidth="1.5"
          className="fill-accent-soft stroke-accent"
        />
        <text
          x="160"
          y="220"
          textAnchor="middle"
          className="fill-text font-serif text-[15px] font-semibold"
        >
          {l.diskTitle}
        </text>
        <text x="160" y="238" textAnchor="middle" className={SUB}>
          {l.diskSub}
        </text>
      </svg>
    </Card>
  );
}
