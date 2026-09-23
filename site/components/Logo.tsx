export function LogoMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      fill="none"
      className={className}
      aria-hidden="true"
    >
      <line
        x1="16"
        y1="3"
        x2="16"
        y2="27"
        stroke="currentColor"
        strokeWidth="2.4"
        strokeLinecap="round"
      />
      <line
        x1="7"
        y1="9"
        x2="25"
        y2="9"
        stroke="currentColor"
        strokeWidth="2.4"
        strokeLinecap="round"
      />
      <path
        d="M10,9.6 L22,9.6 L22,23 L20,25.5 L18,23 L16,25.5 L14,23 L12,25.5 L10,23 Z"
        fill="currentColor"
      />
      <circle cx="16" cy="3" r="1.5" fill="currentColor" />
    </svg>
  );
}
