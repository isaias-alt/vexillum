import type { LoopCopy } from "@/lib/loop-strings";
import {
  Arrow,
  ArrowDefs,
  Badge,
  Rail,
  Soldier,
  Tag,
} from "./diagram-parts";

type Labels = LoopCopy["labels"];

// Text styles shared by both layouts. Sizes are SVG user units: each layout's
// viewBox is as wide as the space it is drawn in, so they render near 1:1.
const NODE_TITLE = "fill-text font-serif text-[16px] font-semibold";
const NODE_SUB = "fill-text-muted font-mono text-[11px]";
const LABEL = "fill-text-secondary font-mono text-[11px]";
const BOX = "fill-surface stroke-border-strong";
const CAMP = "fill-sunken stroke-border-strong";

interface CampSpec {
  name: (l: Labels) => string;
  kind: (l: Labels) => string;
}

const CAMPS: CampSpec[] = [
  { name: (l) => `${l.camp} 1`, kind: (l) => l.mission },
  { name: (l) => `${l.camp} 2`, kind: (l) => l.scout },
  { name: (l) => l.campN, kind: (l) => l.mission },
];

/** Wide layout (from the lg breakpoint): the loop runs left to right. */
function Wide({ l, alt }: { l: Labels; alt: string }) {
  const id = "loop-wide";
  const campX = 510;
  const campW = 210;
  const campTop = [60, 172, 284];
  const campH = 96;
  return (
    <svg
      viewBox="0 40 960 436"
      role="img"
      aria-label={alt}
      className="mx-auto hidden h-auto w-full max-w-[960px] lg:block"
    >
      <ArrowDefs id={id} />

      {/* general <-> commander */}
      <rect x="16" y="180" width="130" height="80" rx="10" className={BOX} strokeWidth="1.5" />
      <text x="81" y="216" textAnchor="middle" className={NODE_TITLE}>{l.general}</text>
      <text x="81" y="237" textAnchor="middle" className={NODE_SUB}>{l.generalSub}</text>

      <rect x="236" y="180" width="140" height="80" rx="10" className="fill-surface stroke-accent" strokeWidth="2" />
      <text x="306" y="216" textAnchor="middle" className={NODE_TITLE}>{l.commander}</text>
      <text x="306" y="237" textAnchor="middle" className={NODE_SUB}>{l.commanderSub}</text>

      <Arrow id={id} d="M146 205 H234" />
      <text x="190" y="197" textAnchor="middle" className={LABEL}>{l.ask}</text>
      <Arrow id={id} d="M236 241 H148" />
      <text x="192" y="259" textAnchor="middle" className={LABEL}>{l.report}</text>
      <Badge x={190} y={170} n={1} />

      {/* commander decides: land or ship */}
      <Arrow id={id} d="M306 180 V118" />
      <rect x="250" y="72" width="112" height="46" rx="8" className={BOX} strokeWidth="1.5" />
      <text x="306" y="91" textAnchor="middle" className={LABEL}>vx land</text>
      <text x="306" y="108" textAnchor="middle" className={LABEL}>vx ship</text>
      <Badge x={334} y={149} n={6} />

      {/* commander -> camps: the fan-out */}
      <Rail d="M376 220 H466 M466 108 V332" />
      <Arrow id={id} d="M466 108 H508" />
      <Arrow id={id} d="M466 220 H508" />
      <Arrow id={id} d="M466 332 H508" />
      <text x="421" y="210" textAnchor="middle" className={LABEL}>{l.dispatch}</text>
      <Badge x={421} y={186} n={2} />

      {/* camps, each with its soldier */}
      {CAMPS.map((camp, i) => {
        const top = campTop[i];
        return (
          <g key={i}>
            <rect
              x={campX}
              y={top}
              width={campW}
              height={campH}
              rx="12"
              strokeWidth="1.5"
              strokeDasharray="6 4"
              className={CAMP}
            />
            <text x={campX + 16} y={top + 26} className={NODE_SUB}>
              {camp.name(l)}
            </text>
            <Tag text={camp.kind(l)} x={campX + campW - 14} y={top + 12} />
            <Soldier x={campX + 18} y={top + 46} />
            <text x={campX + 62} y={top + 61} className="fill-text font-serif text-[15px] font-semibold">
              {l.soldier}
            </text>
            <text x={campX + 62} y={top + 78} className={NODE_SUB}>
              {l.campSub}
            </text>
          </g>
        );
      })}
      <Badge x={campX} y={campTop[0]} n={3} />

      {/* sentinel polls the camps */}
      <rect x="814" y="180" width="130" height="80" rx="10" className="fill-surface stroke-bronze" strokeWidth="2" />
      <text x="879" y="216" textAnchor="middle" className={NODE_TITLE}>{l.sentinel}</text>
      <text x="879" y="237" textAnchor="middle" className={NODE_SUB}>{l.sentinelSub}</text>
      <Rail d="M814 220 H758 M758 108 V332" tone="muted" dashed />
      <Arrow id={id} d="M758 108 H724" tone="muted" dashed />
      <Arrow id={id} d="M758 220 H724" tone="muted" dashed />
      <Arrow id={id} d="M758 332 H724" tone="muted" dashed />
      <text x="786" y="210" textAnchor="middle" className={LABEL}>{l.polls}</text>
      <Badge x={758} y={84} n={4} />

      {/* sentinel wakes the commander */}
      <Arrow
        id={id}
        tone="bronze"
        d="M879 260 V428 Q879 440 867 440 H318 Q306 440 306 428 V262"
      />
      <text x="592" y="430" textAnchor="middle" className="fill-bronze font-mono text-[11px]">
        {`${l.wakeA} ${l.wakeB}`}
      </text>
      <text x="592" y="462" textAnchor="middle" className={NODE_SUB}>{l.wakeNote}</text>
      <Badge x={592} y={402} n={5} />
    </svg>
  );
}

/** Narrow layout (below lg): the same loop, top to bottom, three camps abreast. */
function Narrow({ l, alt }: { l: Labels; alt: string }) {
  const id = "loop-narrow";
  const campW = 84;
  const campTop = 216;
  const campH = 120;
  const campX = [24, 118, 212];
  return (
    <svg
      viewBox="0 0 320 452"
      role="img"
      aria-label={alt}
      className="mx-auto h-auto w-full max-w-[420px] lg:hidden"
    >
      <ArrowDefs id={id} />

      {/* general <-> commander */}
      <rect x="100" y="8" width="120" height="48" rx="10" className={BOX} strokeWidth="1.5" />
      <text x="160" y="29" textAnchor="middle" className="fill-text font-serif text-[15px] font-semibold">{l.general}</text>
      <text x="160" y="45" textAnchor="middle" className={NODE_SUB}>{l.generalSub}</text>

      <rect x="100" y="104" width="120" height="48" rx="10" className="fill-surface stroke-accent" strokeWidth="2" />
      <text x="160" y="125" textAnchor="middle" className="fill-text font-serif text-[15px] font-semibold">{l.commander}</text>
      <text x="160" y="141" textAnchor="middle" className={NODE_SUB}>{l.commanderSub}</text>

      <Arrow id={id} d="M140 56 V102" />
      <text x="132" y="84" textAnchor="end" className={LABEL}>{l.ask}</text>
      <Arrow id={id} d="M180 104 V58" />
      <text x="188" y="84" className={LABEL}>{l.report}</text>
      <Badge x={160} y={80} n={1} />

      {/* commander decides: land or ship */}
      <Arrow id={id} d="M220 128 H248" />
      <rect x="250.5" y="108" width="69" height="40" rx="8" className={BOX} strokeWidth="1.5" />
      <text x="285" y="125" textAnchor="middle" className={LABEL}>vx land</text>
      <text x="285" y="140" textAnchor="middle" className={LABEL}>vx ship</text>
      <Badge x={285} y={96} n={6} />

      {/* commander -> camps: the fan-out */}
      <Rail d="M160 152 V192 M66 192 H254" />
      <Arrow id={id} d="M66 192 V214" />
      <Arrow id={id} d="M160 192 V214" />
      <Arrow id={id} d="M254 192 V214" />
      <text x="168" y="184" className={LABEL}>{l.dispatch}</text>
      <Badge x={136} y={172} n={2} />

      {/* camps, each with its soldier */}
      {CAMPS.map((camp, i) => {
        const x = campX[i];
        const cx = x + campW / 2;
        return (
          <g key={i}>
            <rect
              x={x}
              y={campTop}
              width={campW}
              height={campH}
              rx="12"
              strokeWidth="1.5"
              strokeDasharray="6 4"
              className={CAMP}
            />
            <text x={cx} y={campTop + 21} textAnchor="middle" className={NODE_SUB}>
              {camp.name(l)}
            </text>
            <Soldier x={cx - 16} y={campTop + 34} />
            <text x={cx} y={campTop + 82} textAnchor="middle" className="fill-text font-serif text-[13px] font-semibold">
              {l.soldier}
            </text>
            <Tag text={camp.kind(l)} x={cx} y={campTop + 92} align="middle" />
          </g>
        );
      })}
      <Badge x={campX[0]} y={campTop} n={3} />

      {/* sentinel polls the camps */}
      <Rail d="M250.5 392 V368 M66 368 H254" tone="muted" dashed />
      <Arrow id={id} d="M66 368 V340" tone="muted" dashed />
      <Arrow id={id} d="M160 368 V340" tone="muted" dashed />
      <Arrow id={id} d="M254 368 V340" tone="muted" dashed />
      <text x="242" y="385" textAnchor="end" className={LABEL}>{l.polls}</text>
      <Badge x={38} y={368} n={4} />

      <rect x="181.5" y="392" width="138" height="48" rx="10" className="fill-surface stroke-bronze" strokeWidth="2" />
      <text x="250.5" y="413" textAnchor="middle" className="fill-text font-serif text-[15px] font-semibold">{l.sentinel}</text>
      <text x="250.5" y="429" textAnchor="middle" className={NODE_SUB}>{l.sentinelSub}</text>

      {/* sentinel wakes the commander */}
      <Arrow
        id={id}
        tone="bronze"
        d="M181.5 416 H22 Q10 416 10 404 V140 Q10 128 22 128 H98"
      />
      <text x="91" y="404" textAnchor="middle" className="fill-bronze font-mono text-[11px]">{l.wakeA}</text>
      <text x="91" y="433" textAnchor="middle" className="fill-bronze font-mono text-[11px]">{l.wakeB}</text>
      <Badge x={10} y={260} n={5} />
    </svg>
  );
}

/** "The command loop": the whole chain of command in one picture, with the
 *  numbered steps spelled out as captions underneath. */
export function CommandLoop({ t }: { t: LoopCopy }) {
  return (
    <figure className="m-0">
      <div className="rounded-xl border border-border bg-surface p-3 sm:p-6">
        <Wide l={t.labels} alt={t.alt} />
        <Narrow l={t.labels} alt={t.alt} />
      </div>
      <ol className="m-0 mt-6 grid list-none grid-cols-1 gap-x-8 gap-y-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
        {t.steps.map((step, i) => (
          <li key={step} className="flex min-w-0 gap-3">
            <span
              aria-hidden
              className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full bg-accent font-serif text-[12px] text-accent-contrast"
            >
              {i + 1}
            </span>
            <span className="text-[13px] leading-[1.6] text-text-secondary">
              {step}
            </span>
          </li>
        ))}
      </ol>
    </figure>
  );
}
