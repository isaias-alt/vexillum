// Building blocks of the landing's hand-authored SVG diagrams. Colors are the
// site's tokens (Tailwind's fill-*/stroke-* over --color-*), so every diagram
// follows the light and dark themes with no per-theme file.

/** JetBrains Mono's advance as a fraction of the font size. */
const MONO_ADVANCE = 0.6;

/** Width in px of `text` set in the mono face at `size` px. */
export function monoWidth(text: string, size = 11) {
  return text.length * size * MONO_ADVANCE;
}

export type Tone = "accent" | "bronze" | "muted";

const MARKER_FILL: Record<Tone, string> = {
  accent: "fill-accent",
  bronze: "fill-bronze",
  muted: "fill-text-muted",
};

const STROKE: Record<Tone, string> = {
  accent: "stroke-accent",
  bronze: "stroke-bronze",
  muted: "stroke-text-muted",
};

/** Arrowhead markers. `id` keeps them unique per SVG on the page. */
export function ArrowDefs({ id }: { id: string }) {
  return (
    <defs>
      {(Object.keys(MARKER_FILL) as Tone[]).map((tone) => (
        <marker
          key={tone}
          id={`${id}-${tone}`}
          viewBox="0 0 8 8"
          refX="7"
          refY="4"
          markerWidth="8"
          markerHeight="8"
          markerUnits="userSpaceOnUse"
          orient="auto"
        >
          <path d="M0 0.5 L8 4 L0 7.5 z" className={MARKER_FILL[tone]} />
        </marker>
      ))}
    </defs>
  );
}

/** A line or path with an arrowhead at its end. */
export function Arrow({
  id,
  d,
  tone = "accent",
  dashed = false,
}: {
  id: string;
  d: string;
  tone?: Tone;
  dashed?: boolean;
}) {
  return (
    <path
      d={d}
      fill="none"
      strokeWidth="1.5"
      strokeDasharray={dashed ? "4 4" : undefined}
      markerEnd={`url(#${id}-${tone})`}
      className={STROKE[tone]}
    />
  );
}

/** A plain line, no arrowhead: the shared trunk and bus of a fan. */
export function Rail({
  d,
  tone = "accent",
  dashed = false,
}: {
  d: string;
  tone?: Tone;
  dashed?: boolean;
}) {
  return (
    <path
      d={d}
      fill="none"
      strokeWidth="1.5"
      strokeDasharray={dashed ? "4 4" : undefined}
      className={STROKE[tone]}
    />
  );
}

/** A soldier: a helmeted bust carrying a vexillum, the pennant that names the
 *  project. 32 wide and 28 tall, drawn from its top-left corner. */
export function Soldier({ x, y }: { x: number; y: number }) {
  return (
    <g transform={`translate(${x} ${y})`}>
      {/* pennant */}
      <path
        d="M24 1 V16"
        fill="none"
        strokeWidth="1.5"
        strokeLinecap="round"
        className="stroke-bronze"
      />
      <path d="M24.5 2 L32 5 L24.5 8 Z" className="fill-bronze" />
      {/* shoulders */}
      <path
        d="M3 28 C3 21 7 19 12 19 C17 19 21 21 21 28 Z"
        className="fill-accent"
      />
      {/* face */}
      <rect
        x="7.5"
        y="10"
        width="9"
        height="9"
        rx="3"
        strokeWidth="1.5"
        className="fill-surface stroke-accent"
      />
      {/* helmet */}
      <path
        d="M4 12 C4 5 8 1 12 1 C16 1 20 5 20 12 Z"
        className="fill-accent"
      />
    </g>
  );
}

/** A numbered marker tying a spot of the diagram to its caption. */
export function Badge({ x, y, n }: { x: number; y: number; n: number }) {
  return (
    <g transform={`translate(${x} ${y})`}>
      <circle r="9.5" className="fill-accent" />
      <text
        textAnchor="middle"
        dy="0.36em"
        className="fill-accent-contrast font-serif text-[12px]"
      >
        {n}
      </text>
    </g>
  );
}

/** A small outlined label, right-aligned to `right` (or centered on it). */
export function Tag({
  text,
  x,
  y,
  align = "end",
  tone = "bronze",
}: {
  text: string;
  x: number;
  y: number;
  align?: "end" | "middle";
  tone?: "bronze" | "accent";
}) {
  const width = monoWidth(text) + 16;
  const left = align === "end" ? x - width : x - width / 2;
  return (
    <g>
      <rect
        x={left}
        y={y}
        width={width}
        height="18"
        rx="9"
        strokeWidth="1"
        className={
          tone === "bronze"
            ? "fill-bronze-soft stroke-bronze"
            : "fill-accent-soft stroke-accent"
        }
      />
      <text
        x={left + width / 2}
        y={y + 12.5}
        textAnchor="middle"
        className={`font-mono text-[11px] ${tone === "bronze" ? "fill-bronze" : "fill-accent"}`}
      >
        {text}
      </text>
    </g>
  );
}
