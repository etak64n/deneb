// One endpoint and its recent deliveries.

import { useCallback, useEffect, useState } from "react";
import { api, messageOf, type Delivery, type Webhook } from "../api.ts";
import { Button, ConfirmDialog, ErrorBox, PageHeader, Section, Skeleton, table } from "../components/ui.tsx";
import { formatDateTime } from "../format.ts";
import { Link, navigate, useTitle } from "../router.tsx";

const DELIVERY_TONES: Record<Delivery["status"], string> = {
  pending: "bg-surface-subdued text-label shadow-[0_0_0_1px_var(--ds-border)]",
  delivered: "bg-positive-subtle text-positive shadow-[0_0_0_1px_var(--ds-positive-border)]",
  failed: "bg-critical/10 text-critical-ink",
};

const DELIVERY_LABELS: Record<Delivery["status"], string> = {
  pending: "Retrying",
  delivered: "Delivered",
  failed: "Failed",
};

export function WebhookPage({ id }: { id: string }) {
  const [hook, setHook] = useState<Webhook | null>(null);
  const [deliveries, setDeliveries] = useState<Delivery[] | null>(null);
  const [error, setError] = useState("");
  const [deleting, setDeleting] = useState(false);
  useTitle(hook?.url ?? "Webhook");

  const load = useCallback(() => {
    Promise.all([api.listWebhooks(), api.listDeliveries(id)])
      .then(([{ webhooks }, { deliveries: list }]) => {
        const found = webhooks.find((candidate) => candidate.id === id);
        if (found === undefined) {
          setError("This endpoint does not exist.");
          return;
        }
        setError("");
        setHook(found);
        setDeliveries(list);
      })
      .catch((reason: unknown) => setError(messageOf(reason)));
  }, [id]);
  useEffect(load, [load]);

  return (
    <div className="grid gap-8">
      <div className="grid gap-2">
        <Link to="/webhooks" className="w-fit text-sm font-semibold text-accent hover:text-accent-hover">
          Webhooks
        </Link>
        <PageHeader
          title={<span className="break-all">{hook?.url ?? "Endpoint"}</span>}
          actions={
            hook && (
              <Button className="text-critical" onClick={() => setDeleting(true)}>
                Delete
              </Button>
            )
          }
        />
      </div>
      {error && <ErrorBox>{error}</ErrorBox>}
      <Section title="Deliveries" actions={<Button onClick={load}>Refresh</Button>}>
        {deliveries === null ? (
          !error && <Skeleton className="h-24" />
        ) : (
          deliveries.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className={table.headRow}>
                    <th className={table.head}>Event</th>
                    <th className={table.head}>Status</th>
                    <th className={`${table.head} text-right`}>Attempts</th>
                    <th className={`${table.head} text-right`}>Response</th>
                    <th className={table.head}>Error</th>
                    <th className={table.head}>Created</th>
                  </tr>
                </thead>
                <tbody>
                  {deliveries.map((delivery) => (
                    <tr key={delivery.id} className={table.row}>
                      <td className={`${table.cell} font-mono text-xs`}>{delivery.event}</td>
                      <td className={table.cell}>
                        <span className={`inline-flex h-5 items-center rounded-xs px-1.5 text-xs whitespace-nowrap ${DELIVERY_TONES[delivery.status]}`}>
                          {DELIVERY_LABELS[delivery.status]}
                        </span>
                      </td>
                      <td className={`${table.cell} numeric text-right`}>{delivery.attempts}</td>
                      <td className={`${table.cell} numeric text-right`}>{delivery.response_status > 0 ? delivery.response_status : ""}</td>
                      <td className={`${table.cell} max-w-xs truncate text-label`} title={delivery.last_error}>
                        {delivery.status === "delivered" ? "" : (delivery.last_error ?? "")}
                      </td>
                      <td className={`${table.cell} numeric whitespace-nowrap text-label`}>{formatDateTime(delivery.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        )}
      </Section>
      <ConfirmDialog
        open={deleting}
        title="Delete this endpoint?"
        confirmLabel="Delete"
        onClose={() => setDeleting(false)}
        onConfirm={() => api.deleteWebhook(id).then(() => navigate("/webhooks"))}
      />
    </div>
  );
}
