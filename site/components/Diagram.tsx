import Image from "next/image";

// A diagram exported by `pnpm diagrams` (scripts/export-diagrams.mjs): one SVG
// per theme under public/diagrams, switched by the site's own data-theme
// attribute. next/image serves an .svg as is, so the page carries no Excalidraw
// code. width and height are the SVG's viewBox size.
export function Diagram({
  name,
  width,
  height,
  alt,
  caption,
}: {
  /** File stem under public/diagrams, e.g. "architecture.en". */
  name: string;
  width: number;
  height: number;
  alt: string;
  caption: string;
}) {
  const image = "mx-auto my-0 h-auto w-full max-w-[640px]";
  return (
    <figure className="my-8 border-0 bg-transparent!">
      <Image
        src={`/diagrams/${name}.light.svg`}
        width={width}
        height={height}
        alt={alt}
        className={`${image} dark:hidden`}
      />
      <Image
        src={`/diagrams/${name}.dark.svg`}
        width={width}
        height={height}
        alt={alt}
        className={`${image} hidden dark:block`}
      />
      <figcaption className="mt-3 text-center text-[13px] text-text-muted">
        {caption}
      </figcaption>
    </figure>
  );
}
