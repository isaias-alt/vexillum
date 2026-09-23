import { NavBar } from "@/components/NavBar";
import { DocsSidebar } from "@/components/docs/DocsSidebar";

export default function DocsLayout({ children }: { children: React.ReactNode }) {
  return (
    <>
      <NavBar variant="docs" />
      <div className="mx-auto grid max-w-[1680px] grid-cols-[240px_1fr] gap-x-12 px-8 lg:px-12 max-md:grid-cols-1">
        <DocsSidebar />
        <div className="min-w-0">{children}</div>
      </div>
    </>
  );
}
