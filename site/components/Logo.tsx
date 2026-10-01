// The brand mark: lapis on the dark tile, identical to app/icon.svg so the
// page header, the footer and the favicon read as one mark in both themes.
export const MARK_LAPIS = "#6FA1CB";
export const MARK_GROUND = "#15171A";

export function LogoMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      fill="none"
      className={className}
      aria-hidden="true"
    >
      <rect width="32" height="32" rx="6" fill={MARK_GROUND} />
      <line
        x1="16"
        y1="4"
        x2="16"
        y2="27"
        stroke={MARK_LAPIS}
        strokeWidth="2.6"
        strokeLinecap="round"
      />
      <line
        x1="8"
        y1="10"
        x2="24"
        y2="10"
        stroke={MARK_LAPIS}
        strokeWidth="2.6"
        strokeLinecap="round"
      />
      <path
        d="M11,10.6 L21,10.6 L21,22.5 L19.5,24.5 L18,22.5 L16,24.5 L14,22.5 L12.5,24.5 L11,22.5 Z"
        fill={MARK_LAPIS}
      />
      <circle cx="16" cy="4" r="1.6" fill={MARK_LAPIS} />
    </svg>
  );
}
