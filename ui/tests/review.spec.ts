import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";

test("renders the repository evidence and human review controls", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { level: 1 })).toContainText("Facts first");
  await expect(page.locator("#summary")).toContainText("session");
  await expect(page.locator("#findings > article")).toHaveCount(9);
  await expect(page.getByText("No repository data was sent to an external service.")).toBeVisible();
  await expect(page.getByText("This page edits one review packet for this repository.")).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Review path" }).getByRole("link")).toHaveCount(3);
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();
  await expect(page.locator("#save-status")).toContainText("No unsaved changes");
});

test("saves a human assessment through the protected local API", async ({ page }) => {
  await page.goto("/");

  await page.getByLabel("Assessment status").selectOption("in_progress");
  await page.getByLabel("Context").fill("Automated local review check");
  await expect(page.locator("#save-status")).toHaveText("Unsaved changes in this packet.");
  await expect(page.getByRole("button", { name: "Save changes" })).toBeEnabled();
  await page.locator("#decision-status").selectOption("needs_work");
  await page.getByLabel("Decision owner").fill("Local reviewer");
  await page.getByLabel("Decision rationale").fill("CI and recovery evidence remain open.");

  const firstFinding = page.locator("#findings > article").first();
  await firstFinding.getByLabel("Disposition").selectOption("accepted");
  await firstFinding.getByLabel("Owner", { exact: true }).fill("Local reviewer");
  await page.getByRole("button", { name: "Save changes" }).click();

  await expect(page.locator("#save-status")).toHaveText(/Saved locally at \d{2}:\d{2}:\d{2} UTC\./);
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();

  await page.getByLabel("Decision rationale").fill("A second local change.");
  await expect(page.locator("#save-status")).toHaveText("Unsaved changes in this packet.");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.locator("#save-status")).toHaveText(/Saved locally at \d{2}:\d{2}:\d{2} UTC\./);

  await page.reload();
  await expect(page.getByLabel("Decision rationale")).toHaveValue("A second local change.");
  await expect(page.locator("#save-status")).toContainText("No unsaved changes. Last saved");
});

test("downloads all formats with saved judgments and no external requests", async ({ page }) => {
  const externalRequests: string[] = [];
  page.on("request", (request) => {
    if (/^https?:/.test(request.url()) && !request.url().startsWith("http://127.0.0.1:43117/")) externalRequests.push(request.url());
  });
  await page.goto("/");
  await expect(page.locator("#export-html")).toBeEnabled();
  await page.getByLabel("Context").fill("Portable saved review context");
  await page.getByLabel("Decision owner").fill("Download reviewer");
  await page.getByLabel("Decision rationale").fill("Portable decision rationale");
  await page.locator("#decision-status").selectOption("needs_work");
  await expect(page.locator("#export-status")).toContainText("Save changes before downloading");
  for (const id of ["html", "markdown", "json"]) await expect(page.locator(`#export-${id}`)).toBeDisabled();
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.locator("#save-status")).toContainText("Saved locally");
  const current = await page.evaluate(async () => {
    const token = document.querySelector<HTMLMetaElement>('meta[name="review-token"]')!.content;
    return (await fetch("/api/packet", { headers: { "X-Release-Evidence-Token": token } })).json();
  });

  for (const [format, filename] of [
    ["html", "release-evidence-report.html"],
    ["markdown", "release-evidence-report.md"],
    ["json", "release-evidence-packet.json"],
  ]) {
    const downloaded = page.waitForEvent("download");
    await page.locator(`#export-${format}`).click();
    const download = await downloaded;
    expect(download.suggestedFilename()).toBe(filename);
    expect(await download.failure()).toBeNull();
    const path = await download.path();
    expect(path).not.toBeNull();
    const content = await readFile(path!, "utf8");
    for (const text of ["Portable saved review context", "Download reviewer", "Portable decision rationale", current.integrity.digest]) expect(content).toContain(text);
    if (format === "json") expect(JSON.parse(content)).toEqual(current);
    await expect(page.locator("#export-status")).toContainText("download started");
  }
  expect(externalRequests).toEqual([]);
  await expect(page.getByLabel("Context")).toHaveValue("Portable saved review context");
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();
});

test("blocks downloads and editing during saving, and preserves unsaved edits on failure", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#export-html")).toBeEnabled();
  await page.getByLabel("Context").fill("Retain this unsaved context");
  let release!: () => void;
  const saving = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/review", async (route) => {
    await saving;
    await route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "The local packet could not be saved." }) });
  });
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByLabel("Context")).toBeDisabled();
  await expect(page.locator("#export-html")).toBeDisabled();
  release();
  await expect(page.locator("#save-status")).toContainText("could not be saved");
  await expect(page.getByLabel("Context")).toBeEnabled();
  await expect(page.getByLabel("Context")).toHaveValue("Retain this unsaved context");
  await expect(page.locator("#export-html")).toBeDisabled();
  await expect(page.locator("#export-status")).toContainText("Unsaved edits are excluded");
  await page.unroute("**/api/review");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.locator("#save-status")).toContainText("Saved locally");
  await expect(page.locator("#export-html")).toBeEnabled();
});

test("reports download failure, unlocks controls, and supports keyboard retry", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#export-html")).toBeEnabled();
  const downloads: string[] = [];
  page.on("download", (download) => downloads.push(download.suggestedFilename()));
  let release!: () => void;
  const exporting = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/export", async (route) => {
    await exporting;
    await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Export temporarily unavailable." }) });
  });
  await page.locator("#export-html").focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#export-controls")).toHaveAttribute("aria-busy", "true");
  await expect(page.getByLabel("Context")).toBeDisabled();
  for (const id of ["html", "markdown", "json"]) await expect(page.locator(`#export-${id}`)).toBeDisabled();
  release();
  await expect(page.locator("#export-status")).toHaveText("Download failed. Export temporarily unavailable.");
  expect(downloads).toEqual([]);
  await expect(page.getByLabel("Context")).toBeEnabled();
  await expect(page.locator("#export-html")).toBeFocused();
  await page.unroute("**/api/export");
  const downloaded = page.waitForEvent("download");
  await page.keyboard.press("Enter");
  expect((await downloaded).suggestedFilename()).toBe("release-evidence-report.html");
  await expect(page.locator("#export-status")).toContainText("download started");
  await expect(page.locator("#export-controls")).toHaveAttribute("aria-busy", "false");
});

test("keeps downloads unavailable when the initial packet cannot load", async ({ page }) => {
  await page.route("**/api/packet", (route) => route.fulfill({ status: 500, contentType: "application/json", body: '{"error":"Packet unavailable."}' }));
  await page.goto("/");
  await expect(page.locator("#save-status")).toHaveText("Packet unavailable.");
  await expect(page.locator("#export-status")).toContainText("packet could not be loaded");
  await expect(page.getByLabel("Context")).toBeDisabled();
  for (const id of ["html", "markdown", "json"]) await expect(page.locator(`#export-${id}`)).toBeDisabled();
});

test("rejects a stale tab save without losing edits and allows explicit recovery", async ({ page, context }, testInfo) => {
  await page.goto("/");
  await expect(page.locator("#export-html")).toBeEnabled();
  const other = await context.newPage();
  await other.goto("/");
  await expect(other.locator("#export-html")).toBeEnabled();
  await other.getByLabel("Context").fill("Keep the second tab's pending context");
  await other.getByLabel("Decision owner").fill("Second tab owner");
  await page.getByLabel("Context").fill("First tab's newer saved context");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.locator("#save-status")).toContainText("Saved locally");

  let attempts = 0;
  other.on("request", (request) => {
    if (request.url().endsWith("/api/review") && request.method() === "PUT") attempts++;
  });
  const rejected = other.waitForResponse((response) => response.url().endsWith("/api/review"));
  await other.getByRole("button", { name: "Save changes" }).click();
  expect((await rejected).status()).toBe(412);
  await expect(other.locator("#save-status")).toContainText("This review has newer saved changes");
  await expect(other.locator("#save-status")).toContainText("Copy them before reloading");
  await expect(other.locator("#save-status")).toBeFocused();
  await expect(other.getByLabel("Context")).toHaveValue("Keep the second tab's pending context");
  await expect(other.getByLabel("Decision owner")).toHaveValue("Second tab owner");
  await expect(other.getByLabel("Context")).toBeEnabled();
  await expect(other.getByRole("button", { name: "Save changes" })).toBeDisabled();
  for (const format of ["html", "markdown", "json"]) await expect(other.locator(`#export-${format}`)).toBeDisabled();
  await other.getByLabel("Context").fill("Copy this edited context before reload");
  await expect(other.locator("#save-status")).toContainText("newer saved changes");
  await other.getByLabel("Decision owner").press("Enter");
  expect(attempts).toBe(1);
  const saved = await other.evaluate(async () => {
    const token = document.querySelector<HTMLMetaElement>('meta[name="review-token"]')!.content;
    return (await fetch("/api/packet", { headers: { "X-Release-Evidence-Token": token } })).json();
  });
  expect(saved.assessment.context).toBe("First tab's newer saved context");
  const accessibility = await new AxeBuilder({ page: other }).analyze();
  expect(accessibility.violations).toEqual([]);
  await other.setViewportSize({ width: 360, height: 800 });
  const sizes = await other.evaluate(() => ({ width: document.documentElement.scrollWidth, viewport: window.innerWidth }));
  expect(sizes.width).toBeLessThanOrEqual(sizes.viewport);
  await other.locator(".save-dock").screenshot({ path: testInfo.outputPath("save-conflict.png") });

  const copiedContext = await other.getByLabel("Context").inputValue();
  const copiedOwner = await other.getByLabel("Decision owner").inputValue();
  other.once("dialog", (dialog) => { void dialog.accept(); });
  await other.reload();
  await expect(other.getByLabel("Context")).toHaveValue("First tab's newer saved context");
  await other.getByLabel("Context").fill(`${saved.assessment.context}; ${copiedContext}`);
  await other.getByLabel("Decision owner").fill(copiedOwner);
  await other.getByRole("button", { name: "Save changes" }).click();
  await expect(other.locator("#save-status")).toContainText("Saved locally");
  await expect(other.getByLabel("Context")).toHaveValue("First tab's newer saved context; Copy this edited context before reload");
  await expect(other.locator("#export-html")).toBeEnabled();
  await other.close();
});

test("fails closed when a loaded packet has no review version", async ({ page }) => {
  await page.route("**/api/packet", async (route) => {
    const response = await route.fetch();
    const headers = response.headers();
    delete headers.etag;
    await route.fulfill({ response, headers });
  });
  await page.goto("/");
  await expect(page.locator("#save-status")).toContainText("review version is unavailable");
  await expect(page.getByLabel("Context")).toBeDisabled();
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();
  await expect(page.locator("#export-json")).toBeDisabled();
});

test("preserves edits when a save commits but its successful response is lost", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#export-html")).toBeEnabled();
  await page.getByLabel("Context").fill("Saved on the server before the response was lost");
  await page.route("**/api/review", async (route) => {
    const committed = await route.fetch();
    expect(committed.status()).toBe(200);
    await route.fulfill({ status: 503, contentType: "application/json", body: '{"error":"The save response was lost."}' });
  });
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.locator("#save-status")).toContainText("save response was lost");
  await expect(page.getByRole("button", { name: "Save changes" })).toBeEnabled();
  await page.unroute("**/api/review");
  await page.getByLabel("Context").fill("Keep this additional edit after the lost response");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.locator("#save-status")).toContainText("newer saved changes");
  await expect(page.getByLabel("Context")).toHaveValue("Keep this additional edit after the lost response");
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();
  const savedContext = await page.evaluate(async () => {
    const token = document.querySelector<HTMLMetaElement>('meta[name="review-token"]')!.content;
    const current = await (await fetch("/api/packet", { headers: { "X-Release-Evidence-Token": token } })).json();
    return current.assessment.context;
  });
  expect(savedContext).toBe("Saved on the server before the response was lost");
});

test("explains finding outcomes and their search boundary", async ({ page }) => {
  await page.goto("/");

  const firstFinding = page.locator("#findings > article").first();
  await expect(firstFinding.getByText(/Scanner result:/)).toBeVisible();
  await firstFinding.getByText("Limits and search boundary").click();
  await expect(firstFinding.getByRole("heading", { name: "Search boundary" })).toBeVisible();
  await expect(firstFinding.getByText(/Open = undecided/)).toBeVisible();
});

test("keeps save controls visible during a long review", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("heading", { name: "What happens next?" }).scrollIntoViewIfNeeded();

  const dock = await page.locator(".save-dock").boundingBox();
  const viewport = page.viewportSize();
  expect(dock).not.toBeNull();
  expect(viewport).not.toBeNull();
  expect(dock!.y).toBeGreaterThanOrEqual(0);
  expect(dock!.y + dock!.height).toBeLessThanOrEqual(viewport!.height);
});

test("has no automatically detectable accessibility violations", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#findings > article")).toHaveCount(9);

  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
});

test("does not overflow a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto("/");
  await expect(page.locator("#findings > article")).toHaveCount(9);

  const sizes = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: document.documentElement.clientWidth,
  }));
  expect(sizes.documentWidth).toBeLessThanOrEqual(sizes.viewportWidth);
});
