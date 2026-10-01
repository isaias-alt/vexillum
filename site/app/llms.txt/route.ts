import { llmsIndex } from "@/lib/llms";

export const revalidate = false;

export async function GET() {
  return new Response(await llmsIndex("en"), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
}
