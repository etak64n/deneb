// The API of deneb, as the dashboard calls it with the session of the administrator.

export type VideoStatus = "waiting" | "queued" | "processing" | "ready" | "failed";
export type CaptionsStatus = "none" | "queued" | "processing" | "ready" | "failed";

export interface Rendition {
  name: string;
  width: number;
  height: number;
  bitrate: number;
}

export interface Playback {
  hls_url: string;
  poster_url: string;
  storyboard_url: string;
  captions_url?: string;
  embed_url: string;
  expires_at: string;
}

export interface Video {
  id: string;
  title: string;
  status: VideoStatus;
  error?: string;
  progress: number;
  duration: number;
  width: number;
  height: number;
  source_size: number;
  renditions: Rendition[];
  captions: { status: CaptionsStatus; language?: string };
  created_at: string;
  ready_at: string | null;
  playback?: Playback;
}

export interface Upload {
  url: string;
  metadata: Record<string, string>;
  expires_at: string;
}

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  secret?: string;
  created_at: string;
  last_used_at: string | null;
}

export interface Webhook {
  id: string;
  url: string;
  secret?: string;
  created_at: string;
}

export interface Delivery {
  id: string;
  event: string;
  status: "pending" | "delivered" | "failed";
  attempts: number;
  response_status: number;
  last_error?: string;
  created_at: string;
  delivered_at: string | null;
}

export interface User {
  id: string;
  email: string;
}

export interface AuthState {
  setup_required: boolean;
  user: User | null;
  captions: boolean;
}

/** An error of the API, with the status of the response. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

let signedOut: (() => void) | null = null;

/** Sets what happens when the session ends while the dashboard is open, such as after it expires. */
export function onSignedOut(handler: () => void): void {
  signedOut = handler;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    credentials: "same-origin",
    // deneb refuses changes from the dashboard without this header, which other sites cannot send.
    headers: { "X-Deneb-Client": "dashboard", ...(body === undefined ? {} : { "Content-Type": "application/json" }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status === 204) return undefined as T;
  // A proxy in front of deneb can answer with a page of its own rather than JSON.
  const json = response.headers.get("Content-Type")?.includes("application/json") ?? false;
  const data = json ? ((await response.json()) as unknown) : null;
  if (!response.ok) {
    if (response.status === 401 && !path.startsWith("/auth/")) signedOut?.();
    const message = (data as { error?: { message?: string } } | null)?.error?.message ?? `${response.status} ${response.statusText}`;
    throw new ApiError(response.status, message);
  }
  return data as T;
}

export const api = {
  authState: () => request<AuthState>("GET", "/auth/state"),
  setup: (email: string, password: string) => request<{ user: User }>("POST", "/auth/setup", { email, password }),
  login: (email: string, password: string) => request<{ user: User }>("POST", "/auth/login", { email, password }),
  logout: () => request<void>("POST", "/auth/logout"),

  listVideos: (limit: number, cursor?: string) =>
    request<{ videos: Video[]; next_cursor: string }>("GET", `/v1/videos?limit=${limit}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`),
  getVideo: (id: string) => request<{ video: Video }>("GET", `/v1/videos/${encodeURIComponent(id)}`),
  createVideo: (title: string) => request<{ video: Video; upload: Upload }>("POST", "/v1/videos", { title }),
  renameVideo: (id: string, title: string) => request<{ video: Video }>("PATCH", `/v1/videos/${encodeURIComponent(id)}`, { title }),
  deleteVideo: (id: string) => request<void>("DELETE", `/v1/videos/${encodeURIComponent(id)}`),

  listApiKeys: () => request<{ api_keys: ApiKey[] }>("GET", "/v1/api-keys"),
  createApiKey: (name: string) => request<{ api_key: ApiKey }>("POST", "/v1/api-keys", { name }),
  deleteApiKey: (id: string) => request<void>("DELETE", `/v1/api-keys/${encodeURIComponent(id)}`),

  listWebhooks: () => request<{ webhooks: Webhook[] }>("GET", "/v1/webhooks"),
  createWebhook: (url: string) => request<{ webhook: Webhook }>("POST", "/v1/webhooks", { url }),
  deleteWebhook: (id: string) => request<void>("DELETE", `/v1/webhooks/${encodeURIComponent(id)}`),
  listDeliveries: (id: string) => request<{ deliveries: Delivery[] }>("GET", `/v1/webhooks/${encodeURIComponent(id)}/deliveries`),
};

/** A message for an error of any kind. */
export function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
