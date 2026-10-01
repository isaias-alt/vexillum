import { notFound } from "next/navigation";

// Any path the site does not serve ends up here, so the styled 404 in
// ../not-found.tsx is used instead of Next's unstyled default.
export default function CatchAll() {
  notFound();
}
