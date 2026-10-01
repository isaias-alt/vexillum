import { THEME_INIT_SCRIPT } from "@/lib/theme";

// The blocking theme script, rendered by the server as raw HTML. It runs while
// the document is parsed, before the first paint. A <script> element created
// by React would be re-created on the client when the [lang] layout remounts
// (language switch) and React warns about it; markup set through innerHTML is
// inert on the client, and the server-rendered script has already run.
export function ThemeScript() {
  return (
    <div
      hidden
      dangerouslySetInnerHTML={{
        __html: `<script>${THEME_INIT_SCRIPT}</script>`,
      }}
    />
  );
}
