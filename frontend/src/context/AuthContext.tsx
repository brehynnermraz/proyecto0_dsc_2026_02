"use client";

import { createContext, useContext, useEffect, useState, ReactNode } from "react";

const STORAGE_KEY = "okf_token";

interface AuthContextValue {
  token: string | null;
  ready: boolean; // true una vez que se leyó localStorage, evita parpadeo/redirects prematuros
  setToken: (token: string) => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    setTokenState(localStorage.getItem(STORAGE_KEY));
    setReady(true);
  }, []);

  function setToken(next: string) {
    localStorage.setItem(STORAGE_KEY, next);
    setTokenState(next);
  }

  function logout() {
    localStorage.removeItem(STORAGE_KEY);
    setTokenState(null);
  }

  return (
    <AuthContext.Provider value={{ token, ready, setToken, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth debe usarse dentro de AuthProvider");
  return ctx;
}
