// Records a short demo of the API of a running deneb, in a page with a terminal on the
// left and a site on the right. The terminal creates a video with curl, sends its file
// to its upload URL in one PUT, follows the encoding, receives the signed webhooks, and
// gets the signed URLs; the site then embeds the video, which plays with its captions. Every request,
// answer, and webhook is real; the terminal shows the answers filtered as its jq says.
// It writes the recording as demo.webm.
//
// Usage: node demo.mjs <deneb URL> <folder of .mp4 clips> <output folder>
// The administrator, a key of the API, and the webhook endpoint are created first,
// with DENEB_SETUP_CODE, outside the recording.

import { createHmac, timingSafeEqual } from "node:crypto";
import { mkdir, readFile } from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import { chromium, request } from "playwright";

const [base, clipsDir, out] = process.argv.slice(2);
if (!base || !clipsDir || !out) throw new Error("usage: node demo.mjs <deneb URL> <clips> <output folder>");
await mkdir(out, { recursive: true });
// deneb builds its URLs from the address it is asked at, and localhost reads better.
const deneb = base.replace("127.0.0.1", "localhost");
const clip = await readFile(path.join(clipsDir, "Welcome to deneb.mp4"));
// run.sh lets the container reach this host as host.docker.internal.
const hookPort = 9877;

console.log("Set up the administrator, a key, and a webhook endpoint");
const session = await request.newContext({ baseURL: deneb, extraHTTPHeaders: { "X-Deneb-Client": "demo" } });
async function json(response) {
  if (!response.ok()) throw new Error(`${response.url()}: ${response.status()} ${await response.text()}`);
  return response.json();
}
await json(await session.post("/auth/setup", { data: { email: "admin@example.com", password: "correct horse battery", setup_code: process.env.DENEB_SETUP_CODE ?? "" } }));
const key = (await json(await session.post("/v1/api-keys", { data: { name: "demo" } }))).api_key.secret;
const hook = (await json(await session.post("/v1/webhooks", { data: { url: `http://host.docker.internal:${hookPort}/hooks/deneb` } }))).webhook;
await session.dispose();

// The webhooks that arrive, with whether their signature matches the secret of the endpoint.
const events = [];
const receiver = http.createServer((req, res) => {
  let body = "";
  req.on("data", (chunk) => (body += chunk));
  req.on("end", () => {
    const fields = new Map((req.headers["deneb-signature"] ?? "").split(",").map((part) => part.split("=")));
    const expected = createHmac("sha256", hook.secret).update(`${fields.get("t")}.${body}`).digest("hex");
    const given = fields.get("v1") ?? "";
    events.push({ event: req.headers["deneb-event"], signed: given.length === expected.length && timingSafeEqual(Buffer.from(given), Buffer.from(expected)) });
    res.writeHead(204).end();
  });
});
await new Promise((resolve) => receiver.listen(hookPort, "0.0.0.0", resolve));

async function api(method, pathname, body) {
  const response = await fetch(deneb + pathname, {
    method,
    headers: { Authorization: `Bearer ${key}`, ...(body === undefined ? {} : { "Content-Type": "application/json" }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`${method} ${pathname}: ${response.status} ${await response.text()}`);
  return response.json();
}

// JSON as a terminal shows it, with its keys and strings in color.
function highlight(value) {
  const html = JSON.stringify(value, null, 2).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
  return html.replace(/"([^"]*)"(:?)/g, (_, text, colon) => (colon ? `<span class="key">"${text}"</span>:` : `<span class="string">"${text}"</span>`));
}

const pointerSvg =
  '<svg width="26" height="26" viewBox="0 0 24 24"><path d="M4 2v17l4.5-4.2 3 6.7 2.8-1.2-3-6.6H17z" fill="#1a2c44" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/></svg>';
const title = (lines) => `
  <div style="display:grid;justify-items:center;gap:16px;text-align:center">
    <img src="${deneb}/logo.svg" width="76" height="76" alt="">
    <div style="font-size:46px;line-height:54px;font-weight:700">deneb</div>
    ${lines}
  </div>`;
const tagline = '<div style="font-size:22px;line-height:30px;color:#5b6b80">A self-hosted video API in one Docker image</div>';

const scene = `<!doctype html>
<html><head><meta charset="utf-8"><style>
  * { box-sizing: border-box; }
  body { margin: 0; height: 100vh; overflow: hidden; background: #eef2f7; color: #1a2c44;
    font: 15px/1.5 -apple-system, "Segoe UI", Roboto, "Liberation Sans", Arial, sans-serif; }
  .stage { display: grid; grid-template-columns: 1.1fr 1fr; gap: 24px; height: 100%; padding: 78px 28px 28px; }
  .window { display: flex; flex-direction: column; min-width: 0; overflow: hidden; border-radius: 10px;
    box-shadow: 0 0 0 1px rgb(0 0 0 / .06), 0 12px 32px rgb(15 23 42 / .14); }
  .bar { display: flex; flex: none; align-items: center; gap: 8px; height: 36px; padding: 0 14px; font-size: 13px; }
  .dot { width: 11px; height: 11px; border-radius: 50%; background: #c8d0da; }
  .terminal { background: #0f1724; }
  .terminal .bar { background: #182235; color: #8b9bb4; }
  .terminal .dot { background: #2d3a50; }
  #term { flex: 1; margin: 0; padding: 14px 16px; overflow: hidden; color: #d6deeb; white-space: pre-wrap; word-break: break-all;
    font: 13.5px/1.55 "DejaVu Sans Mono", "Liberation Mono", Menlo, monospace; }
  #term > div + .command { margin-top: 10px; }
  .prompt, .ok { color: #7ee787; } .comment, .dim { color: #8b9bb4; } .key { color: #79c0ff; } .string { color: #a5d6ff; } .event { color: #ffa657; }
  .browser { background: #fff; }
  .browser .bar { background: #f3f5f8; border-bottom: 1px solid #e3e8ee; }
  .url { flex: 1; margin-left: 8px; padding: 3px 12px; border-radius: 6px; background: #fff; color: #5b6b80; box-shadow: 0 0 0 1px #e3e8ee; }
  .site { display: grid; align-content: start; gap: 14px; padding: 22px 26px; }
  .site h1 { margin: 0; font-size: 24px; line-height: 32px; }
  .line { height: 10px; border-radius: 5px; background: #e9edf2; }
  #player { display: grid; place-items: center; overflow: hidden; aspect-ratio: 16 / 9; border-radius: 8px; background: #e9edf2; color: #8792a2; font-size: 14px; }
  #player iframe { width: 100%; height: 100%; border: 0; }
  #caption { position: fixed; top: 20px; left: 50%; z-index: 10; transform: translateX(-50%); padding: 8px 18px; border-radius: 8px;
    background: #1a2c44; color: #fff; font-size: 17px; font-weight: 600; white-space: nowrap; box-shadow: 0 6px 20px rgb(0 0 0 / .18);
    opacity: 0; transition: opacity .25s; }
  #card { position: fixed; inset: 0; z-index: 20; display: grid; place-items: center; background: #fff; }
  #card code { margin-top: 8px; padding: 12px 18px; border-radius: 8px; background: #f3f5f8; font: 19px/26px "DejaVu Sans Mono", "Liberation Mono", monospace; }
  #pointer { position: fixed; left: -40px; top: -40px; z-index: 30; pointer-events: none; }
  .ring { position: fixed; z-index: 29; width: 28px; height: 28px; border: 2px solid #635bff; border-radius: 50%; pointer-events: none; transition: transform .4s, opacity .4s; }
</style></head><body>
<div class="stage">
  <div class="window terminal">
    <div class="bar"><span class="dot"></span><span class="dot"></span><span class="dot"></span><span style="margin-left:10px">Terminal</span></div>
    <pre id="term"></pre>
  </div>
  <div class="window browser">
    <div class="bar"><span class="dot"></span><span class="dot"></span><span class="dot"></span><div class="url">your-site.example/videos/welcome</div></div>
    <div class="site">
      <h1>Welcome to deneb</h1>
      <div class="line" style="width:92%"></div>
      <div class="line" style="width:74%"></div>
      <div id="player">The video appears here</div>
      <div class="line" style="width:86%"></div>
      <div class="line" style="width:62%"></div>
    </div>
  </div>
</div>
<div id="caption"></div>
<div id="card">${title(tagline)}</div>
<div id="pointer">${pointerSvg}</div>
<script>
  const term = document.getElementById("term");
  const element = (id) => document.getElementById(id);
  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  window.scene = {
    caption(text) {
      element("caption").textContent = text || "";
      element("caption").style.opacity = text ? "1" : "0";
    },
    card(html) {
      element("card").innerHTML = html || "";
      element("card").style.display = html ? "grid" : "none";
    },
    pointer(x, y) {
      element("pointer").style.left = (x - 4) + "px";
      element("pointer").style.top = (y - 2) + "px";
    },
    ring(x, y) {
      const ring = document.createElement("div");
      ring.className = "ring";
      ring.style.left = (x - 14) + "px";
      ring.style.top = (y - 14) + "px";
      document.body.append(ring);
      requestAnimationFrame(() => { ring.style.transform = "scale(1.6)"; ring.style.opacity = "0"; });
      setTimeout(() => ring.remove(), 450);
    },
    // Types a command after the prompt, one character at a time.
    async type(text, kind) {
      const line = document.createElement("div");
      line.className = "command";
      line.innerHTML = '<span class="prompt">$ </span>';
      const typed = document.createElement("span");
      if (kind) typed.className = kind;
      line.append(typed);
      term.append(line);
      for (const char of text) {
        typed.textContent += char;
        term.scrollTop = term.scrollHeight;
        await wait(char === "\\n" ? 140 : 14);
      }
    },
    // Prints a line, or replaces the line with the same id.
    print(html, id) {
      let line = id ? element(id) : null;
      if (!line) {
        line = document.createElement("div");
        if (id) line.id = id;
        term.append(line);
      }
      line.innerHTML = html;
      term.scrollTop = term.scrollHeight;
    },
    embed(url) {
      element("player").innerHTML = '<iframe allow="autoplay; fullscreen; picture-in-picture" src="' + url + '"></iframe>';
    },
  };
</script>
</body></html>`;

// Google Chrome rather than the Chromium of Playwright, which cannot decode H.264 or AAC.
const browser = await chromium.launch({ channel: "chrome", args: ["--autoplay-policy=no-user-gesture-required"] });
const viewport = { width: 1280, height: 720 };
const context = await browser.newContext({ viewport, recordVideo: { dir: out, size: viewport } });
const page = await context.newPage();
await page.setContent(scene);
const pause = (ms) => page.waitForTimeout(ms);
const call = (method, ...args) => page.evaluate(([method, args]) => window.scene[method](...args), [method, args]);

// The recording shows no pointer of its own, and the page cannot see the mouse over the
// embedded player, so the pointer of the page follows each move of the mouse from here.
let mouse = { x: 1100, y: 640 };
async function glide(x, y, steps = 30) {
  const from = mouse;
  for (let step = 1; step <= steps; step++) {
    const t = step / steps;
    const eased = t * t * (3 - 2 * t);
    const at = { x: from.x + (x - from.x) * eased, y: from.y + (y - from.y) * eased };
    await page.mouse.move(at.x, at.y);
    await call("pointer", at.x, at.y);
  }
  mouse = { x, y };
}
async function click(locator) {
  const box = await locator.boundingBox();
  if (box === null) throw new Error("the element is not on the page");
  await glide(box.x + box.width / 2, box.y + box.height / 2);
  await pause(200);
  await call("ring", mouse.x, mouse.y);
  await page.mouse.click(mouse.x, mouse.y);
}

console.log("The title");
await call("pointer", mouse.x, mouse.y);
await pause(2600);
await call("card", null);
await pause(500);

console.log("Create a video");
await call("caption", "1  Create a video, and get the URL for its file");
await call(
  "type",
  `UPLOAD_URL=$(curl -s -X POST $DENEB/v1/videos \\\n    -H "Authorization: Bearer $KEY" \\\n    -d '{"title": "Welcome to deneb"}' | jq -r .upload.url)`,
);
const created = await api("POST", "/v1/videos", { title: "Welcome to deneb" });
const id = created.video.id;
await pause(300);
await call("type", "echo $UPLOAD_URL");
await call("print", `<span class="string">${created.upload.url}</span>`);
await pause(1600);

console.log("Send the file in one PUT");
await call("caption", "2  Send the file to that URL in one PUT");
await call("type", `curl -T welcome.mp4 "$UPLOAD_URL" | jq '.video | {id, status}'`);
const put = await fetch(created.upload.url, { method: "PUT", body: clip });
if (put.status !== 200) throw new Error(`send the file: ${put.status} ${await put.text()}`);
const uploaded = (await put.json()).video;
await pause(300);
await call("print", highlight({ id: uploaded.id, status: uploaded.status }));
await pause(1600);

console.log("Follow the encoding and the webhooks");
await call("caption", "3  deneb encodes it, writes captions, and calls your webhook");
await call("type", "# deneb encodes it to HLS and writes its captions with Whisper", "comment");
const spinner = ["◐", "◓", "◑", "◒"];
let shown = 0;
let video = created.video;
for (let tick = 0; ; tick++) {
  video = (await api("GET", `/v1/videos/${id}`)).video;
  const state =
    video.status === "ready"
      ? `<span class="ok">✓</span> encoded  <span class="dim">${video.renditions.map((rendition) => rendition.name).join(" ")}</span>`
      : `<span class="event">${spinner[tick % 4]}</span> ${video.status === "processing" ? `encoding  ${Math.floor(video.progress * 100)}%` : video.status}`;
  await call("print", state, "state");
  if (video.status === "ready") {
    const captions =
      video.captions.status === "ready"
        ? `<span class="ok">✓</span> captions <span class="dim">${video.captions.language}</span>`
        : `<span class="event">${spinner[tick % 4]}</span> captions  ${video.captions.status}`;
    await call("print", captions, "captions");
  }
  for (; shown < events.length; shown++) {
    const { event, signed } = events[shown];
    await call("print", `<span class="event">←</span> POST /hooks/deneb  <span class="event">${event}</span>  ${signed ? '<span class="ok">signature ok</span>' : "bad signature"}`);
  }
  if (video.status === "failed") throw new Error(`the encoding failed: ${video.error}`);
  // Ready captions send a second webhook after video.ready.
  const settled = video.status === "ready" && video.captions.status !== "queued" && video.captions.status !== "processing";
  if (settled && shown >= (video.captions.status === "ready" ? 2 : 1)) break;
  if (tick > 600) throw new Error("timed out waiting for the video");
  await pause(250);
}
await pause(1200);

console.log("Get the playback URLs");
await call("caption", "4  Get the URLs that deneb signed");
await call("type", `curl -s $DENEB/v1/videos/${id} \\\n    -H "Authorization: Bearer $KEY" \\\n    | jq '.video | {status, captions, embed_url: .playback.embed_url}'`);
const ready = (await api("GET", `/v1/videos/${id}`)).video;
await pause(300);
await call("print", highlight({ status: ready.status, captions: ready.captions, embed_url: ready.playback.embed_url }));
await pause(1800);

console.log("Embed the video in the site");
await call("caption", "5  Embed it in any site, with its captions");
await call("embed", ready.playback.embed_url);
const frame = page.frameLocator("#player iframe");
await frame.locator("video").waitFor({ state: "attached", timeout: 30_000 });
await pause(800);
await click(frame.getByRole("button", { name: "Play", exact: true }).first()).catch(async () => {
  await frame.locator("video").evaluate((element) => element.play());
});
await frame.locator("video").evaluate((element) => {
  for (const track of element.textTracks) if (track.kind === "subtitles") track.mode = "showing";
  return element.play();
});
// Out of the way of the captions.
const player = await page.locator("#player").boundingBox();
await glide(player.x + player.width * 0.8, player.y + player.height + 60, 25);
await pause(3800);

console.log("Preview pictures on the seek bar");
await call("caption", "6  The seek bar previews every moment");
try {
  await glide(player.x + player.width / 2, player.y + player.height / 2, 20);
  const seek = frame.locator(".media-time-slider").first();
  await seek.waitFor({ state: "visible", timeout: 3000 });
  const box = await seek.boundingBox();
  const y = box.y + box.height / 2;
  await glide(box.x + box.width * 0.25, y, 20);
  await pause(400);
  await glide(box.x + box.width * 0.75, y, 50);
  await pause(300);
  // A still of the preview, to check it without playing the recording.
  await page.screenshot({ path: path.join(out, "seek-preview.png") });
  await pause(900);
} catch (error) {
  console.warn(`skipped the seek bar: ${error.message}`);
}

console.log("The command to run deneb");
await frame.locator("video").evaluate((element) => element.pause());
await call("caption", null);
await call("pointer", -40, -40);
await call("card", title(`${tagline}<code>docker run -p 8080:8080 ghcr.io/etak64n/deneb</code><div style="font-size:17px;color:#5b6b80">github.com/etak64n/deneb</div>`));
await pause(3200);

const recording = page.video();
await context.close();
await recording?.saveAs(path.join(out, "demo.webm"));
await recording?.delete();
await browser.close();
receiver.close();
console.log(`wrote ${path.join(out, "demo.webm")}`);
