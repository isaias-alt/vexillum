import { createMDX } from "fumadocs-mdx/next";

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  async rewrites() {
    // `/docs/<page>.md` is the same page as plain Markdown, for agents. The
    // Spanish tree has its own prefix, so each locale gets its own rule.
    return [
      {
        source: "/docs/:slug*.md",
        destination: "/llms.mdx/en/:slug*/content.md",
      },
      {
        source: "/es/docs/:slug*.md",
        destination: "/llms.mdx/es/:slug*/content.md",
      },
    ];
  },
};

const withMDX = createMDX();

export default withMDX(config);
