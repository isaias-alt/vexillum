export type TocItem = { href: string; label: string };

export function PageWithToc({
  toc,
  children,
}: {
  toc: TocItem[];
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-[1fr_240px] gap-x-12 max-[1040px]:grid-cols-1">
      <div className="min-w-0 max-w-[720px] py-12 pb-[110px]">{children}</div>
      <div className="sticky top-[81px] self-start border-l border-border py-12 pl-6 text-sm max-[1040px]:hidden">
        <div className="mb-3.5 text-xs tracking-[0.06em] text-text-dim uppercase">
          on this page
        </div>
        {toc.map((item) => (
          <a
            key={item.href}
            href={item.href}
            className="block py-1.5 text-text-muted hover:text-text"
          >
            {item.label}
          </a>
        ))}
      </div>
    </div>
  );
}
