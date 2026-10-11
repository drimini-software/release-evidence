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
