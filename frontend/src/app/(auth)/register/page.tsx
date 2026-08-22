"use client";

import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { register, login, ApiError } from "@/lib/api";
import { useAuth } from "@/context/AuthContext";
import PasswordInput from "@/components/PasswordInput";

export default function RegisterPage() {
  const { setToken } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await register(email, password);
      // el registro no devuelve token, así que se hace login inmediatamente
      const { token } = await login(email, password);
      setToken(token);
      router.push("/");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo crear la cuenta");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div>
      <h1 className="text-3xl font-semibold text-neutral-900">Crea tu cuenta</h1>
      <p className="mt-2 text-sm text-neutral-500">Empieza a convertir documentos en bundles OKF.</p>

      <form onSubmit={handleSubmit} className="mt-8 space-y-4">
        <input
          type="email"
          placeholder="Correo"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          className="w-full rounded-xl bg-neutral-100 px-4 py-3 text-sm text-neutral-900 placeholder-neutral-400 outline-none ring-[#171717]/40 focus:ring-2"
        />
        <div>
          <PasswordInput placeholder="Contraseña" value={password} onChange={setPassword} minLength={8} />
          <p className="mt-1.5 pl-1 text-xs text-neutral-400">Mínimo 8 caracteres</p>
        </div>

        {error && <p className="text-sm text-red-600">{error}</p>}

        <button
          type="submit"
          disabled={loading}
          className="w-full rounded-xl bg-[#171717] py-3 text-sm font-semibold text-white transition hover:bg-[#000000] disabled:opacity-50"
        >
          {loading ? "Creando cuenta..." : "Crear cuenta"}
        </button>
      </form>

      <p className="mt-6 text-center text-sm text-neutral-500">
        ¿Ya tienes cuenta?{" "}
        <Link href="/login" className="font-medium text-[#171717] hover:underline">
          Ingresa
        </Link>
      </p>
    </div>
  );
}
