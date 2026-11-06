// Uses the dashboard of a running deneb in Google Chrome the way an administrator
// does: it creates the administrator, adds a webhook endpoint and an API key, uploads
// the clips through the page, plays one with its captions, opens its embed page, looks
// at the pages on a phone, and renames and deletes a video. It saves a screenshot of
// each step, and fails on an error in a page or a request that fails.
//
// Usage: node dashboard.mjs <deneb URL> <folder of .mp4 clips> <folder for screenshots>
// "Welcome to deneb.mp4" must be among the clips, and spoken in English.

import assert from "node:assert/strict";
import { mkdir, readdir } from "node:fs/promises";
import path from "node:path";
import { chromium } from "playwright";

const [base, clipsDir, out] = process.argv.slice(2);
if (!base || !clipsDir || !out) throw new Error("usage: node dashboard.mjs <deneb URL> <clips> <screenshots>");
await mkdir(out, { recursive: true });
const clips = (await readdir(clipsDir))
  .filter((name) => name.endsWith(".mp4"))
  .sort()
  .map((name) => path.join(clipsDir, name));
const hookURL = "http://host.docker.internal:9876/hook";

// What went wrong in any page, reported at the end so that every screenshot is taken.
const problems = [];
function watch(page) {
  page.on("console", (message) => {
    if (message.type() === "error") problems.push(`console error on ${page.url()}: ${message.text()}`);
  });
  page.on("pageerror", (error) => problems.push(`page error on ${page.url()}: ${error.message}`));
  page.on("response", (response) => {
    if (response.status() >= 400) problems.push(`${response.status()} for ${response.request().method()} ${response.url()}`);
  });
  page.on("requestfailed", (request) => {
    // A player cancels the segments it no longer needs, and a page its images when it goes.
    const reason = request.failure()?.errorText ?? "";
    if (reason !== "net::ERR_ABORTED") problems.push(`${reason} for ${request.method()} ${request.url()}`);
  });
}

async function until(check, what, timeout = 180_000) {
  const deadline = Date.now() + timeout;
  while (!(await check())) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`);
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
}

function shot(page, name, options = {}) {
  console.log(`  screenshot ${name}`);
  return page.screenshot({ path: path.join(out, `${name}.png`), ...options });
}

// Waits until the images in the page have loaded, so that no screenshot catches them empty.
async function imagesLoaded(page) {
  await page.waitForFunction(() => [...document.images].every((image) => image.complete && image.naturalWidth > 0));
}

// Plays the video of the page for a few seconds with its subtitles on, and pauses it.
async function play(page) {
  const video = page.locator("video");
  await video.evaluate((element) => {
    for (const track of element.textTracks) if (track.kind === "subtitles") track.mode = "showing";
    return element.play();
  });
  await page.waitForFunction(() => (document.querySelector("video")?.currentTime ?? 0) > 3, null, { timeout: 30_000 });
  await video.evaluate((element) => element.pause());
}

// Google Chrome rather than the Chromium of Playwright, which cannot decode H.264 or AAC.
const browser = await chromium.launch({ channel: "chrome", args: ["--autoplay-policy=no-user-gesture-required"] });
const desktop = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
const page = await desktop.newPage();
watch(page);

async function videos() {
  const response = await page.request.get(`${base}/v1/videos?limit=100`);
  assert.equal(response.status(), 200);
  return (await response.json()).videos;
}

console.log("Create the administrator");
await page.goto(base);
await page.getByRole("heading", { name: "Create the administrator" }).waitFor();
await imagesLoaded(page);
await shot(page, "01-setup");
await page.getByLabel("Email").fill("admin@example.com");
await page.getByLabel("Password").fill("correct horse battery");
await page.getByRole("button", { name: "Create and sign in" }).click();
await page.getByRole("button", { name: "Upload videos" }).waitFor();
await shot(page, "02-videos-empty");

console.log("Add a webhook endpoint");
const dialog = page.getByRole("dialog");
await page.getByRole("link", { name: "Webhooks", exact: true }).click();
await page.getByRole("button", { name: "Add endpoint" }).click();
await dialog.getByLabel("Endpoint URL").fill(hookURL);
await dialog.getByRole("button", { name: "Add endpoint" }).click();
await dialog.getByRole("heading", { name: "Copy the signing secret" }).waitFor();
assert.match((await dialog.locator("code").textContent()) ?? "", /^whsec_/);
await shot(page, "03-webhook-secret");
await dialog.getByRole("button", { name: "Done" }).click();
await page.getByRole("link", { name: hookURL }).waitFor();

console.log("Create an API key");
await page.getByRole("link", { name: "API keys", exact: true }).click();
await page.getByRole("button", { name: "Create key" }).click();
await dialog.getByLabel("Name").fill("Production");
await dialog.getByRole("button", { name: "Create", exact: true }).click();
await dialog.getByRole("heading", { name: "Copy the key" }).waitFor();
assert.match((await dialog.locator("code").textContent()) ?? "", /^dnb_/);
await shot(page, "04-api-key-secret");
await dialog.getByRole("button", { name: "Done" }).click();
await page.getByRole("cell", { name: "Production" }).waitFor();
await shot(page, "05-api-keys");

console.log(`Upload ${clips.length} clips through the page`);
await page.getByRole("link", { name: "Videos", exact: true }).click();
await page.getByRole("button", { name: "Upload videos" }).waitFor();
await page.locator('input[type="file"]').setInputFiles(clips);
await page.getByRole("region", { name: "Uploads" }).waitFor();
await shot(page, "06-uploading");
await until(async () => {
  const list = await videos();
  return list.length === clips.length && list.every((video) => video.status !== "waiting" && video.status !== "queued" && video.status !== "processing");
}, "the encoding");
await shot(page, "07-videos-encoded");
await until(async () => (await videos()).every((video) => video.captions.status !== "queued" && video.captions.status !== "processing"), "the captions");
const list = await videos();
for (const video of list) {
  assert.equal(video.status, "ready", `${video.title} is ${video.status}: ${video.error ?? ""}`);
  assert.equal(video.captions.status, video.title === "Silent loop" ? "none" : "ready", `the captions of ${video.title}`);
}
const welcome = list.find((video) => video.title === "Welcome to deneb");
assert.ok(welcome, "no video is titled Welcome to deneb");
assert.equal(welcome.captions.language, "en");
const captions = await (await page.request.get(welcome.playback.captions_url)).text();
assert.match(captions, /upload|video|stream/i, `the captions do not have the spoken words:\n${captions}`);
await page.reload();
await page.locator("tbody tr").nth(clips.length - 1).waitFor();
await imagesLoaded(page);
await shot(page, "08-videos");

console.log("Play a video with its captions");
await page.getByRole("link", { name: "Welcome to deneb" }).click();
await page.getByText("Ready (English)").waitFor();
await page.locator("video").waitFor({ state: "attached" });
await page.waitForTimeout(1000);
await shot(page, "09-video", { fullPage: true });
await play(page);
await shot(page, "10-video-playing");

console.log("Open the embed page");
const embed = await desktop.newPage();
watch(embed);
await embed.setViewportSize({ width: 960, height: 540 });
await embed.goto(welcome.playback.embed_url);
await embed.locator("video").waitFor({ state: "attached" });
await embed.waitForTimeout(1000);
await shot(embed, "11-embed");
await play(embed);
await shot(embed, "12-embed-playing");
await embed.goto(welcome.playback.embed_url.replace(/token=[^&]+/, "token=1.forged"));
await embed.getByText("This video is not available.").waitFor();
await embed.close();
// The forged token above is refused on purpose; it is not a problem of the page.
for (let index = problems.length - 1; index >= 0; index--) {
  if (problems[index].includes("1.forged")) problems.splice(index, 1);
}

console.log("Look at the pages on a phone");
const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, isMobile: true, hasTouch: true, storageState: await desktop.storageState() });
const mobile = await phone.newPage();
watch(mobile);
await mobile.goto(base);
await mobile.locator("tbody tr").nth(clips.length - 1).waitFor();
await imagesLoaded(mobile);
await shot(mobile, "13-phone-videos");
await mobile.goto(`${base}/videos/${encodeURIComponent(welcome.id)}`);
await mobile.getByText("Ready (English)").waitFor();
await mobile.waitForTimeout(1000);
await shot(mobile, "14-phone-video", { fullPage: true });
await phone.close();

console.log("Check the deliveries of the webhook");
await page.getByRole("link", { name: "Webhooks", exact: true }).click();
await page.getByRole("link", { name: hookURL }).click();
const expected = list.length + list.filter((video) => video.captions.status === "ready").length;
await until(async () => {
  await page.getByRole("button", { name: "Refresh" }).click();
  await page.waitForTimeout(500);
  return (await page.getByRole("cell", { name: "Delivered" }).count()) === expected;
}, `${expected} delivered webhooks`, 60_000);
await shot(page, "15-webhook");

console.log("Rename and delete a video");
await page.getByRole("link", { name: "Videos", exact: true }).click();
await page.getByRole("link", { name: "Product tour" }).click();
await page.getByRole("button", { name: "Rename" }).click();
await dialog.getByLabel("Title").fill("Product tour, final cut");
await shot(page, "16-rename");
await dialog.getByRole("button", { name: "Save" }).click();
await page.getByRole("heading", { name: "Product tour, final cut" }).waitFor();
await page.getByRole("button", { name: "Delete" }).click();
await shot(page, "17-delete");
await dialog.getByRole("button", { name: "Delete" }).click();
await page.getByRole("button", { name: "Upload", exact: true }).waitFor();
await until(async () => (await page.locator("tbody tr").count()) === clips.length - 1, "the deleted video to leave the list", 10_000);

await browser.close();
if (problems.length > 0) {
  console.error(`FAIL: ${problems.length} problems\n${problems.join("\n")}`);
  process.exit(1);
}
console.log("PASS");
