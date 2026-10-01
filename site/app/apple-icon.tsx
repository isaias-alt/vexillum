import { ImageResponse } from "next/og";

export const size = { width: 180, height: 180 };
export const contentType = "image/png";

// The same blue (lapis) mark as app/icon.svg on its dark ground, but full-bleed: iOS
// applies its own rounded mask and fills transparent corners with black.
export default function AppleIcon() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          background: "#15171A",
        }}
      >
        <svg width="180" height="180" viewBox="0 0 32 32" fill="none">
          <line x1="16" y1="4" x2="16" y2="27" stroke="#6FA1CB" strokeWidth="2.6" strokeLinecap="round" />
          <line x1="8" y1="10" x2="24" y2="10" stroke="#6FA1CB" strokeWidth="2.6" strokeLinecap="round" />
          <path
            d="M11,10.6 L21,10.6 L21,22.5 L19.5,24.5 L18,22.5 L16,24.5 L14,22.5 L12.5,24.5 L11,22.5 Z"
            fill="#6FA1CB"
          />
          <circle cx="16" cy="4" r="1.6" fill="#6FA1CB" />
        </svg>
      </div>
    ),
    size,
  );
}
