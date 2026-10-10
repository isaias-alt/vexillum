"use client";

import {
  CodeBlock,
  CodeBlockTab,
  CodeBlockTabs,
  CodeBlockTabsList,
  CodeBlockTabsTrigger,
  Pre,
} from "fumadocs-ui/components/codeblock";
import { INSTALL_COMMANDS } from "@/lib/site";

const TABS = ["brew", "curl"] as const;

// The same brew|curl tabbed code block the docs install page renders
// (/docs/get-started/install#install-the-binary), so the hero and the docs
// look and behave alike: tabs on top, a scrollable command, a copy button.
export function InstallCommand({ className }: { className?: string }) {
  return (
    <div className={`mx-auto max-w-160 text-left ${className ?? ""}`}>
      <CodeBlockTabs defaultValue="brew">
        <CodeBlockTabsList>
          {TABS.map((t) => (
            <CodeBlockTabsTrigger key={t} value={t}>
              {t}
            </CodeBlockTabsTrigger>
          ))}
        </CodeBlockTabsList>
        {TABS.map((t) => (
          <CodeBlockTab key={t} value={t}>
            <CodeBlock>
              <Pre>
                <code>
                  <span className="line">{INSTALL_COMMANDS[t]}</span>
                </code>
              </Pre>
            </CodeBlock>
          </CodeBlockTab>
        ))}
      </CodeBlockTabs>
    </div>
  );
}
