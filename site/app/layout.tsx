import type { Metadata } from "next";
import { Spectral, JetBrains_Mono } from "next/font/google";
import "./globals.css";

const spectral = Spectral({
  variable: "--font-spectral",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
});

const jetbrainsMono = JetBrains_Mono({
  variable: "--font-jetbrains-mono",
  subsets: ["latin"],
  weight: ["400", "500", "600"],
});

export const metadata: Metadata = {
  title: {
    default: "vexillum",
    template: "%s · vexillum",
  },
  description:
    "vexillum orchestrates coding agents from your terminal. A commander dispatches soldiers into isolated camps, a sentinel watches for what needs your attention.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      className={`${spectral.variable} ${jetbrainsMono.variable}`}
    >
      <body className="min-h-screen bg-bg font-mono text-text antialiased">
        {children}
      </body>
    </html>
  );
}
