import AuthIllustration from "@/components/AuthIllustration";

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen overflow-hidden bg-white">
      <div className="flex w-full flex-col justify-center overflow-y-auto px-6 py-12 sm:px-12 md:w-1/2 lg:px-20">
        <div className="mx-auto w-full max-w-sm">{children}</div>
      </div>

      <div className="hidden min-h-0 w-1/2 p-4 md:block">
        <AuthIllustration caption="Convierte tus documentos en bundles de conocimiento OKF." />
      </div>
    </div>
  );
}
