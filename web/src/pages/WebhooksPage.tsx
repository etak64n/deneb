// The endpoints that deneb tells about videos: ready, failed, and captions ready.

import { Plus } from "lucide-react";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, messageOf, type Webhook } from "../api.ts";
import { Button, CodeBox, Dialog, ErrorBox, PageHeader, Skeleton, table } from "../components/ui.tsx";
import { formatDate } from "../format.ts";
import { Link, useTitle } from "../router.tsx";

export function WebhooksPage() {
  useTitle("Webhooks");
  const [hooks, setHooks] = useState<Webhook[] | null>(null);
  const [error, setError] = useState("");
  const [adding, setAdding] = useState(false);

  const load = useCallback(() => {
    api
      .listWebhooks()
      .then(({ webhooks }) => {
        setError("");
        setHooks(webhooks);
      })
      .catch((reason: unknown) => setError(messageOf(reason)));
  }, []);
  useEffect(load, [load]);

  return (
    <div className="grid gap-6">
      <PageHeader
        title="Webhooks"
        actions={
          <Button variant="primary" onClick={() => setAdding(true)}>
            <Plus aria-hidden="true" />
            Add endpoint
          </Button>
        }
      />
      {error && <ErrorBox>{error}</ErrorBox>}
      {hooks === null ? (
        !error && <Skeleton className="h-24" />
      ) : (
        hooks.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className={table.headRow}>
                  <th className={table.head}>URL</th>
                  <th className={`${table.head} w-32`}>Created</th>
                </tr>
              </thead>
              <tbody>
                {hooks.map((hook) => (
                  <tr key={hook.id} className={table.row}>
                    <td className={`${table.cell} break-all`}>
                      <Link to={`/webhooks/${encodeURIComponent(hook.id)}`} className="font-semibold text-strong hover:text-accent">
                        {hook.url}
                      </Link>
                    </td>
                    <td className={`${table.cell} numeric text-label`}>{formatDate(hook.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      )}
      <AddEndpointDialog open={adding} onClose={() => setAdding(false)} onAdded={load} />
    </div>
  );
}

function AddEndpointDialog({ open, onClose, onAdded }: { open: boolean; onClose: () => void; onAdded: () => void }) {
  const [url, setUrl] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function close() {
    setUrl("");
    setSecret("");
    setError("");
    onClose();
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    api
      .createWebhook(url)
      .then(({ webhook }) => {
        setSecret(webhook.secret ?? "");
        onAdded();
      })
      .catch((reason: unknown) => setError(messageOf(reason)))
      .finally(() => setBusy(false));
  }

  return (
    <Dialog open={open} onClose={close} title={secret ? "Copy the signing secret" : "Add endpoint"} dismissible={!secret}>
      {secret ? (
        <div className="grid gap-4">
          <p className="text-sm text-label">This is the only time the secret is shown. It signs every request in the Deneb-Signature header.</p>
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
            <span className="text-sm font-semibold text-strong">Endpoint URL</span>
            <input
              type="url"
              value={url}
              required
              autoFocus
              placeholder="https://example.com/webhooks/deneb"
              onChange={(event) => setUrl(event.target.value)}
              className="field"
            />
          </label>
          {error && <ErrorBox>{error}</ErrorBox>}
          <div className="flex justify-end gap-2">
            <Button onClick={close}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={busy}>
              Add endpoint
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
