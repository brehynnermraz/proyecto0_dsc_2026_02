"use client";

import Image from "next/image";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/context/AuthContext";

export default function Nav() {
  const { token, ready, logout } = useAuth();
  const router = useRouter();

  function handleLogout() {
    logout();
    router.push("/login");
  }

  return (
    <header className="border-b border-neutral-200 bg-white">
      <div className="flex items-center justify-between px-6 py-3 sm:px-12 lg:px-20">
        <Link href="/" aria-label="OKF Bundler">
          <Image src="/logo.png" alt="OKF Bundler" width={32} height={32} priority />
        </Link>
        {ready && (
          <nav className="flex items-center gap-4 text-sm">
            {token ? (
              <button
                onClick={handleLogout}
                aria-label="Cerrar sesión"
                title="Cerrar sesión"
                className="flex h-8 w-8 items-center justify-center rounded-full text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900"
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <path d="M10 17l5-5-5-5" strokeLinecap="round" strokeLinejoin="round" />
                  <path d="M15 12H3" strokeLinecap="round" strokeLinejoin="round" />
                  <path d="M10 3h8a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-8" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </button>
            ) : (
              <>
                <Link href="/login" className="text-neutral-600 hover:text-neutral-900">
                  Ingresar
                </Link>
                <Link href="/register" className="text-neutral-600 hover:text-neutral-900">
                  Registrarse
                </Link>
              </>
            )}
          </nav>
        )}
      </div>
    </header>
  );
}
