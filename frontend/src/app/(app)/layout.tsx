import Nav from "@/components/Nav";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen flex-col bg-neutral-50 text-neutral-900">
      <Nav />
      <main className="flex flex-1 items-center px-6 py-10 sm:px-12 lg:px-20">
        <div className="w-full">{children}</div>
      </main>
    </div>
  );
}
