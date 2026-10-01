import { source } from "@/lib/source";
import { createFromSource } from "fumadocs-core/search/server";

// Exported once at build time; the browser downloads and queries it.
export const revalidate = false;
export const { staticGET: GET } = createFromSource(source);
