// Records a short demo of the dashboard of a running deneb: videos dropped on the
// library upload and encode, one plays with its captions, the seek bar previews its
// pictures, and the embed code is copied. It writes the recording as demo.webm.
//
// Usage: node demo.mjs <deneb URL> <folder of .mp4 clips> <output folder>
// The administrator is created first, with DENEB_SETUP_CODE, outside the recording.

import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { chromium } from "playwright";

const [base, clipsDir, out] = process.argv.slice(2);
if (!base || !clipsDir || !out) throw new Error("usage: node demo.mjs <deneb URL> <clips> <output folder>");
await mkdir(out, { recursive: true });
// The clips that are dropped, of those that run.sh makes; the spoken one gets captions.
const clips = ["Welcome to deneb.mp4", "Product tour.mp4", "Silent loop.mp4"];
const viewport = { width: 1280, height: 800 };

// Google Chrome rather than the Chromium of Playwright, which cannot decode H.264 or AAC.
const browser = await chromium.launch({ channel: "chrome", args: ["--autoplay-policy=no-user-gesture-required"] });

// Sign in outside the recording.
const setup = await browser.newContext({ viewport });
const first = await setup.newPage();
await first.goto(base);
await first.getByLabel("Email").fill("admin@example.com");
await first.getByLabel("Password").fill("correct horse battery");
await first.getByLabel("Setup code").fill(process.env.DENEB_SETUP_CODE ?? "");
await first.getByRole("button", { name: "Create and sign in" }).click();
await first.getByRole("button", { name: "Upload videos" }).waitFor();
const signedIn = await setup.storageState();
await setup.close();

// The page copies the embed code, which needs the clipboard.
const context = await browser.newContext({
  viewport,
  storageState: signedIn,
  permissions: ["clipboard-read", "clipboard-write"],
  recordVideo: { dir: out, size: viewport },
});
// Recordings show no pointer, so each page draws one where the mouse is, and a ring where it clicks.
await context.addInitScript(() => {
  window.addEventListener("DOMContentLoaded", () => {
    const pointer = document.createElement("div");
    pointer.innerHTML =
      '<svg width="26" height="26" viewBox="0 0 24 24"><path d="M4 2v17l4.5-4.2 3 6.7 2.8-1.2-3-6.6H17z" fill="#1a2c44" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/></svg>';
    pointer.style.cssText = "position:fixed;left:-40px;top:-40px;z-index:2147483647;pointer-events:none";
    document.body.append(pointer);
    document.addEventListener("mousemove", (event) => {
      pointer.style.left = `${event.clientX - 4}px`;
      pointer.style.top = `${event.clientY - 2}px`;
    }, true);
    document.addEventListener("mousedown", (event) => {
      const ring = document.createElement("div");
      ring.style.cssText = `position:fixed;left:${event.clientX - 14}px;top:${event.clientY - 14}px;width:28px;height:28px;border:2px solid #635bff;border-radius:50%;z-index:2147483646;pointer-events:none;transition:transform .4s,opacity .4s`;
      document.body.append(ring);
      requestAnimationFrame(() => {
        ring.style.transform = "scale(1.6)";
        ring.style.opacity = "0";
      });
      setTimeout(() => ring.remove(), 450);
    }, true);
  });
});
const page = await context.newPage();
const pause = (ms) => page.waitForTimeout(ms);

// Glides the mouse to the middle of an element, as a person would, and clicks it when asked.
async function point(locator, { click = false } = {}) {
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (box === null) throw new Error("the element is not on the page");
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 30 });
  if (click) {
    await pause(250);
    await page.mouse.down();
    await page.mouse.up();
  }
}

// A step that only shows something may skip, rather than stop the demo.
async function maybe(what, step) {
  try {
    await step();
  } catch (error) {
    console.warn(`skipped ${what}: ${error.message}`);
  }
}

console.log("The empty library");
await page.goto(base);
await page.getByRole("button", { name: "Upload videos" }).waitFor();
await page.mouse.move(1180, 160);
await pause(1200);

console.log("Drop the clips on the page");
const files = await Promise.all(clips.map(async (name) => ({ name, data: (await readFile(path.join(clipsDir, name))).toString("base64") })));
await page.evaluate((files) => {
  const transfer = new DataTransfer();
  for (const { name, data } of files) {
    const bytes = Uint8Array.from(atob(data), (char) => char.charCodeAt(0));
    transfer.items.add(new File([bytes], name, { type: "video/mp4" }));
  }
  window.demoFiles = transfer;
}, files);
await page.mouse.move(700, 420, { steps: 30 });
await page.evaluate(() => window.dispatchEvent(new DragEvent("dragenter", { dataTransfer: window.demoFiles })));
await pause(1200);
await page.evaluate(() => window.dispatchEvent(new DragEvent("drop", { dataTransfer: window.demoFiles, cancelable: true })));

console.log("Wait while they upload and encode");
await page.getByRole("region", { name: "Uploads" }).waitFor();
await page.waitForFunction(
  (count) => {
    const rows = [...document.querySelectorAll("tbody tr")];
    return rows.length === count && rows.every((row) => row.textContent.includes("Ready"));
  },
  clips.length,
  { timeout: 180_000, polling: 500 },
);
await page.waitForFunction(() => [...document.images].filter((image) => image.checkVisibility()).every((image) => image.complete && image.naturalWidth > 0));
await pause(1500);

console.log("Open the spoken video and play it with its captions");
await point(page.getByRole("link", { name: "Welcome to deneb" }), { click: true });
await page.getByText("Ready (English)").waitFor({ timeout: 120_000 });
await page.locator("video").waitFor({ state: "attached" });
await pause(1000);
await maybe("the play button", () => point(page.getByRole("button", { name: "Play", exact: true }).first(), { click: true }));
await page.locator("video").evaluate((video) => {
  for (const track of video.textTracks) if (track.kind === "subtitles") track.mode = "showing";
  return video.play();
});
await pause(3000);

console.log("Preview pictures on the seek bar");
await maybe("the seek bar", async () => {
  // The controls show while the mouse moves over the player.
  await point(page.locator("video"));
  const seek = page.locator(".media-time-slider").first();
  await seek.waitFor({ state: "visible", timeout: 3000 });
  const box = await seek.boundingBox();
  if (box === null) throw new Error("no seek bar");
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width * 0.2, y, { steps: 25 });
  await pause(600);
  await page.mouse.move(box.x + box.width * 0.75, y, { steps: 60 });
  await pause(400);
  // A still of the preview, to check it without playing the recording.
  await page.screenshot({ path: path.join(out, "seek-preview.png") });
  await pause(800);
});

console.log("Copy the embed code");
await page.locator("video").evaluate((video) => video.pause());
await maybe("the embed code", async () => {
  const copy = page.getByRole("heading", { name: "Embed" }).locator("xpath=..").getByRole("button", { name: "Copy" });
  await point(copy, { click: true });
  await pause(1800);
});

const video = page.video();
await context.close();
await video?.saveAs(path.join(out, "demo.webm"));
await video?.delete();
await browser.close();
console.log(`wrote ${path.join(out, "demo.webm")}`);
