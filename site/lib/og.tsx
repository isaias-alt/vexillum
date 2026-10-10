import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { ImageResponse } from "next/og";
import { SITE_URL } from "@/lib/site";

export const OG_SIZE = { width: 1200, height: 630 } as const;
export const OG_CONTENT_TYPE = "image/png";

// Identity from site/design/forum-tokens.css (dark theme) and the brand sheet; the
// mark is app/icon.svg (lapis on the dark ground).
const BG = "#15171A";
const TEXT = "#E9EAEC";
const TEXT_SECONDARY = "#A0A3A9";
const TEXT_MUTED = "#6E7177";
const BORDER = "#292D33";
const LAPIS = "#6FA1CB";

// Fonts and the mark are read from the repo at build time (Satori takes ttf
// only), never fetched. The images are prerendered, so none of this runs at
// request time.
const root = process.cwd();
const assets = Promise.all([
  readFile(join(root, "assets/fonts/Spectral-SemiBold.ttf")),
  readFile(join(root, "assets/fonts/JetBrainsMono-Regular.ttf")),
  readFile(join(root, "assets/fonts/JetBrainsMono-Medium.ttf")),
  readFile(join(root, "app/icon.svg"), "utf8"),
]);

export interface OgCard {
  /** Small uppercase line above the title (the docs section). */
  label: string;
  title: string;
  /** One or two lines under the title. */
  description: string;
}

/** Trim to `max` characters at a word boundary, ending in an ellipsis. */
export function clip(text: string, max: number): string {
  const clean = text.replace(/\s+/g, " ").trim();
  if (clean.length <= max) return clean;
  const cut = clean.slice(0, max - 1);
  const space = cut.lastIndexOf(" ");
  const base = space > max * 0.6 ? cut.slice(0, space) : cut;
  return `${base.replace(/[\s,;:.\-]+$/, "")}…`;
}

const TITLE_WIDTH = 1000;
/** Average advance of Spectral SemiBold, in em (measured, with the -1.5px tracking). */
const GLYPH_EM = 0.5;

/** Break a title into lines and pick its size. Sentences each get their own
 * line ("One commander." / "Many soldiers"). A title that fits on one line at
 * a display size stays on one; a longer one wraps to two at a smaller size;
 * past that it is clipped. */
function fitTitle(title: string): { lines: string[]; size: number } {
  const sentences = title.split(/(?<=\.)\s+/);
  if (sentences.length > 1) {
    const longest = Math.max(...sentences.map((l) => l.length));
    return { lines: sentences, size: Math.min(112, Math.floor(TITLE_WIDTH / (longest * GLYPH_EM))) };
  }
  const single = Math.floor(TITLE_WIDTH / (title.length * GLYPH_EM));
  if (single >= 84) return { lines: [title], size: Math.min(single, 128) };
  const text = clip(title, 64);
  const perLine = Math.ceil(text.length / 2) + 2;
  return {
    lines: [text],
    size: Math.max(60, Math.min(92, Math.floor(TITLE_WIDTH / (perLine * GLYPH_EM)))),
  };
}

/** One title line: words in a wrapping row (Satori has no inline flow), with
 * the lapis dot glued to the last word of the last line (not after a clipping ellipsis). */
function Line({ text, size, last }: { text: string; size: number; last: boolean }) {
  const words = text.split(" ");
  return (
    <div style={{ display: "flex", flexWrap: "wrap", columnGap: size * 0.24 }}>
      {words.map((word, i) => (
        <div key={i} style={{ display: "flex" }}>
          {word}
          {last && i === words.length - 1 && !word.endsWith("\u2026") && (
            <span style={{ color: LAPIS }}>.</span>
          )}
        </div>
      ))}
    </div>
  );
}

export async function renderOg(card: OgCard): Promise<ImageResponse> {
  const [spectral, mono, monoMedium, icon] = await assets;
  const mark = `data:image/svg+xml;base64,${Buffer.from(icon).toString("base64")}`;
  const title = fitTitle(card.title.trim().replace(/\.$/, ""));
  const host = SITE_URL.replace(/^https?:\/\//, "");

  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          background: BG,
          color: TEXT,
          padding: "64px 80px",
          border: `1px solid ${BORDER}`,
        }}
      >
        {/* top: section label left, mark and name right */}
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <div
            style={{
              display: "flex",
              fontFamily: "JetBrains Mono",
              fontWeight: 500,
              fontSize: 22,
              letterSpacing: 3,
              textTransform: "uppercase",
              color: LAPIS,
            }}
          >
            {card.label}
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
            <div
              style={{
                display: "flex",
                fontFamily: "JetBrains Mono",
                fontSize: 24,
                color: TEXT_SECONDARY,
              }}
            >
              vexillum
            </div>
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src={mark} width={56} height={56} alt="" />
          </div>
        </div>

        {/* middle: title with its accent dot, then the description */}
        <div style={{ display: "flex", flexDirection: "column", gap: 28 }}>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              fontFamily: "Spectral",
              fontWeight: 600,
              fontSize: title.size,
              lineHeight: 1.08,
              letterSpacing: -1.5,
              color: TEXT,
            }}
          >
            {title.lines.map((line, i) => (
              <Line
                key={i}
                text={line}
                size={title.size}
                last={i === title.lines.length - 1}
              />
            ))}
          </div>
          <div
            style={{
              display: "flex",
              fontFamily: "JetBrains Mono",
              fontWeight: 400,
              fontSize: 26,
              lineHeight: 1.5,
              color: TEXT_SECONDARY,
              maxWidth: 940,
              textWrap: "balance",
            }}
          >
            {clip(card.description, 150)}
          </div>
        </div>

        {/* bottom: accent bar and host */}
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <div
            style={{
              display: "flex",
              width: 72,
              height: 6,
              borderRadius: 3,
              background: LAPIS,
            }}
          />
          <div
            style={{
              display: "flex",
              fontFamily: "JetBrains Mono",
              fontSize: 20,
              color: TEXT_MUTED,
            }}
          >
            {host}
          </div>
        </div>
      </div>
    ),
    {
      ...OG_SIZE,
      fonts: [
        { name: "Spectral", data: spectral, weight: 600, style: "normal" },
        { name: "JetBrains Mono", data: mono, weight: 400, style: "normal" },
        {
          name: "JetBrains Mono",
          data: monoMedium,
          weight: 500,
          style: "normal",
        },
      ],
    },
  );
}
