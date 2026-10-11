import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const project = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const cache = join(project, ".cache");
let directory: string;
let report: string;
let comparison: string;
const hostileText = '<script>window.reportInjected=true</script><img src="https://attacker.invalid/pixel" onerror="window.reportInjected=true">';

test.beforeAll(() => {
  mkdirSync(cache, { recursive: true });
  directory = mkdtempSync(join(cache, "report-browser-"));
  const command = join(directory, process.platform === "win32" ? "release-evidence.exe" : "release-evidence");
  execFileSync("go", ["build", "-o", command, "./cmd/release-evidence"], { cwd: project });
  const previous = join(directory, "previous.json");
  const current = join(directory, "current.json");
  execFileSync(command, ["scan", "--output", previous, join(project, "fixtures/session")], { cwd: project });
  const packet = JSON.parse(readFileSync(previous, "utf8"));
  packet.assessment = {
    status: "in_progress",
    context: hostileText,
    items: [{ finding_id: packet.findings[0].id, disposition: "assigned", owner: "Local test reviewer", rationale: hostileText }],
  };
  packet.decision = { status: "needs_work", rationale: "Synthetic browser test decision", owner: "Local test reviewer" };
  writeFileSync(current, JSON.stringify(packet));
  report = join(directory, "report.html");
  comparison = join(directory, "comparison.html");
  execFileSync(command, ["export", "--output", report, current], { cwd: project });
  execFileSync(command, ["compare", "--output", comparison, previous, current], { cwd: project });
});

test.afterAll(() => {
  if (!directory) return;
  if (!resolve(directory).startsWith(`${resolve(cache)}${sep}`)) throw new Error("Test cleanup escaped its cache directory");
  rmSync(directory, { recursive: true, force: true });
});

test("portable report works offline and renders hostile notes as text", async ({ page }) => {
  const externalRequests: string[] = [];
  page.on("request", request => { if (/^https?:/i.test(request.url())) externalRequests.push(request.url()); });
  await page.goto(pathToFileURL(report).href);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("session");
  await expect(page.getByRole("heading", { name: "Recorded evidence" })).toBeVisible();
  await expect(page.locator("#overview")).toContainText(hostileText);
  await expect(page.locator("script, img, iframe, svg, form")).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as Record<string, unknown>).reportInjected)).toBeUndefined();
  expect(externalRequests).toEqual([]);
  await page.getByRole("navigation", { name: "Report sections" }).getByRole("link", { name: "Open review items" }).click();
  await expect(page.getByRole("heading", { name: "Open review items" })).toBeInViewport();
});

test("comparison preserves human changes separately from unchanged evidence", async ({ page }) => {
  await page.goto(pathToFileURL(comparison).href);
  const changes = page.locator("#changes");
  await expect(changes).toContainText("0 added, removed, or changed rules; 9 unchanged rules.");
  await expect(changes).toContainText("Synthetic browser test decision");
  await expect(changes).toContainText("Local test reviewer");
  await expect(changes).toContainText(hostileText);
  await expect(changes.getByRole("heading", { name: "Human decision changed" })).toBeVisible();
});

test("report and comparison have no automatically detectable accessibility violations", async ({ page }) => {
  for (const path of [report, comparison]) {
    await page.goto(pathToFileURL(path).href);
    const results = await new AxeBuilder({ page }).analyze();
    expect(results.violations).toEqual([]);
  }
});

test("portable reports fit a narrow viewport and expose keyboard navigation", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  for (const path of [report, comparison]) {
    await page.goto(pathToFileURL(path).href);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
    await page.keyboard.press("Tab");
    await expect(page.getByRole("link", { name: "Skip to report" })).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.locator("#overview")).toBeInViewport();
  }
});
