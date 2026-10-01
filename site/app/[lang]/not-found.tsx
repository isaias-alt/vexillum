import Link from "next/link";

export default function NotFound() {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-4 px-6 py-24 text-center">
      <div className="text-[11px] tracking-[0.08em] text-text-muted uppercase">
        404
      </div>
      <h1 className="font-serif text-[32px] font-semibold text-text">
        Page not found
      </h1>
      <Link href="/docs" className="btn btn-secondary">
        docs
      </Link>
    </main>
  );
}
