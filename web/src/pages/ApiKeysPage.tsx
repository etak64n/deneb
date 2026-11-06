// The keys of the API. A new key shows its secret once; the list shows only its start.

import { Plus } from "lucide-react";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, messageOf, type ApiKey } from "../api.ts";
import { Button, CodeBox, ConfirmDialog, Dialog, ErrorBox, PageHeader, Skeleton, table } from "../components/ui.tsx";
import { formatDate } from "../format.ts";

export function ApiKeysPage() {
  const [keys, setKeys] = useState<ApiKey[] | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);

  const load = useCallback(() => {
    api
      .listApiKeys()
      .then(({ api_keys }) => setKeys(api_keys))
      .catch((reason: unknown) => setError(messageOf(reason)));
  }, []);
  useEffect(load, [load]);

  return (
    <div className="grid gap-6">
      <PageHeader
        title="API keys"
        actions={
          <Button variant="primary" onClick={() => setCreating(true)}>
            <Plus aria-hidden="true" />
            Create key
          </Button>
        }
      />
      {error && <ErrorBox>{error}</ErrorBox>}
      {keys === null ? (
        !error && <Skeleton className="h-24" />
      ) : (
        keys.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className={table.headRow}>
                  <th className={table.head}>Name</th>
                  <th className={table.head}>Key</th>
                  <th className={table.head}>Created</th>
                  <th className={table.head}>Last used</th>
                  <th className={`${table.head} w-24`}>
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {keys.map((key) => (
                  <tr key={key.id} className={table.row}>
                    <td className={`${table.cell} font-semibold`}>{key.name}</td>
                    <td className={`${table.cell} font-mono text-xs text-label`}>{`${key.prefix}…`}</td>
                    <td className={`${table.cell} numeric text-label`}>{formatDate(key.created_at)}</td>
                    <td className={`${table.cell} numeric text-label`}>{key.last_used_at ? formatDate(key.last_used_at) : ""}</td>
                    <td className={`${table.cell} text-right`}>
                      <Button variant="ghost" className="text-critical" onClick={() => setRevoking(key)}>
                        Revoke
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      )}
      <CreateKeyDialog open={creating} onClose={() => setCreating(false)} onCreated={load} />
      <ConfirmDialog
        open={revoking !== null}
        title={`Revoke “${revoking?.name ?? ""}”?`}
        confirmLabel="Revoke"
        onClose={() => setRevoking(null)}
        onConfirm={async () => {
          if (revoking === null) return;
          await api.deleteApiKey(revoking.id);
          setRevoking(null);
          load();
        }}
      >
        <p className="text-sm text-label">Requests with this key stop working at once.</p>
      </ConfirmDialog>
    </div>
  );
}

function CreateKeyDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function close() {
    setName("");
    setSecret("");
    setError("");
    onClose();
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    api
      .createApiKey(name)
      .then(({ api_key }) => {
        setSecret(api_key.secret ?? "");
        onCreated();
      })
      .catch((reason: unknown) => setError(messageOf(reason)))
      .finally(() => setBusy(false));
  }

  return (
    <Dialog open={open} onClose={close} title={secret ? "Copy the key" : "Create key"}>
      {secret ? (
        <div className="grid gap-4">
          <p className="text-sm text-label">This is the only time the key is shown.</p>
          <CodeBox text={secret} />
          <div className="flex justify-end">
            <Button variant="primary" onClick={close}>
              Done
            </Button>
          </div>
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="grid gap-4">
          <label className="grid gap-1.5">
            <span className="text-sm font-semibold text-strong">Name</span>
            <input value={name} required maxLength={100} autoFocus onChange={(event) => setName(event.target.value)} className="field" />
          </label>
          {error && <ErrorBox>{error}</ErrorBox>}
          <div className="flex justify-end gap-2">
            <Button onClick={close}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={busy}>
              Create
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
