// The first page: it creates the administrator on the first visit, and signs them in after.

import { useState, type FormEvent } from "react";
import { api, messageOf } from "../api.ts";
import { Button, ErrorBox } from "../components/ui.tsx";

export function AuthPage({ setup, onSignedIn }: { setup: boolean; onSignedIn: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    (setup ? api.setup(email, password) : api.login(email, password))
      .then(onSignedIn)
      .catch((reason: unknown) => {
        setError(messageOf(reason));
        setBusy(false);
      });
  }

  return (
    <main className="grid min-h-screen place-items-center bg-surface-subdued px-4 py-10">
      <form onSubmit={handleSubmit} className="grid w-full max-w-sm gap-5 rounded-lg bg-surface p-6 shadow-[0_0_0_1px_var(--ds-hairline)]">
        <div className="grid gap-3">
          <img src="/logo.svg" alt="" className="size-8" />
          <h1 className="text-figure font-bold text-heading">{setup ? "Create the administrator" : "Sign in to deneb"}</h1>
        </div>
        <label className="grid gap-1.5">
          <span className="text-sm font-semibold text-strong">Email</span>
          <input
            type="email"
            required
            autoComplete={setup ? "email" : "username"}
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            className="field"
          />
        </label>
        <label className="grid gap-1.5">
          <span className="text-sm font-semibold text-strong">Password</span>
          <input
            type="password"
            required
            minLength={setup ? 12 : undefined}
            autoComplete={setup ? "new-password" : "current-password"}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            className="field"
          />
          {setup && <span className="text-xs text-subdued">At least 12 characters</span>}
        </label>
        {error && <ErrorBox>{error}</ErrorBox>}
        <Button type="submit" variant="primary" disabled={busy} className="w-full">
          {setup ? "Create and sign in" : "Sign in"}
        </Button>
      </form>
    </main>
  );
}
