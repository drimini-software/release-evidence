import { expect, test, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const earlierInput = "#comparison-packet";

async function currentPacket(page: Page) {
  return page.evaluate(async () => {
    const token = document.querySelector<HTMLMetaElement>('meta[name="review-token"]')!.content;
    return (await fetch("/api/packet", { headers: { "X-Release-Evidence-Token": token } })).json();
  });
}

test("downloads comparisons with separate human changes and inert earlier notes", async ({ page, context }, testInfo) => {
  const external: string[] = [];
  context.on("request", (request) => {
    if (/^https?:/.test(request.url()) && !request.url().startsWith("http://127.0.0.1:43117/")) external.push(request.url());
  });
  await page.goto("/");
  await expect(page.locator(earlierInput)).toBeEnabled();
  await expect(page.locator("#compare-html")).toBeDisabled();
  await page.getByLabel("Context").fill("Current comparison review context");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.locator("#save-status")).toContainText("Saved locally");
  const current = await currentPacket(page);
  const previous = JSON.parse(JSON.stringify(current));
  const hostile = '<script>window.comparisonInjected=true</script><img src="https://attacker.invalid/pixel">';
  previous.assessment = { status: "complete", context: hostile };
  previous.decision = { status: "blocked", rationale: "Earlier browser decision" };
  await page.locator(earlierInput).setInputFiles({ name: "earlier.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(previous)) });
  await expect(page.locator("#comparison-status")).toContainText("Earlier packet selected: earlier.json");
  let htmlPath: string | null = null;
  for (const [format, extension] of [["html", "html"], ["markdown", "md"], ["json", "json"]]) {
    const ready = page.waitForEvent("download");
    await page.locator(`#compare-${format}`).click();
    const download = await ready;
    expect(download.suggestedFilename()).toBe(`release-evidence-comparison.${extension}`);
    expect(await download.failure()).toBeNull();
    const path = await download.path();
    const content = await readFile(path!, "utf8");
    for (const text of ["Current comparison review context", "Earlier browser decision", current.integrity.digest]) expect(content).toContain(text);
    if (format === "html") {
      htmlPath = testInfo.outputPath("comparison.html");
      await download.saveAs(htmlPath);
    }
    if (format === "json") {
      const result = JSON.parse(content);
      expect(result.changes).toEqual([]);
      expect(result.unchanged).toBe(9);
      expect(result.context_changed).toBe(true);
      expect(result.decision_changed).toBe(true);
      expect(result.assessment_before).toEqual(previous.assessment);
      expect(result.assessment_after).toEqual(current.assessment);
    }
    await expect(page.locator("#comparison-status")).toContainText("download started");
  }
  const report = await context.newPage();
  await report.goto(pathToFileURL(htmlPath!).href);
  await expect(report.locator("#changes")).toContainText(hostile);
  await expect(report.locator("#changes")).toContainText("0 added, removed, or changed rules; 9 unchanged rules.");
  await expect(report.locator("script, img, iframe, svg, form")).toHaveCount(0);
  expect(await report.evaluate(() => (window as unknown as Record<string, unknown>).comparisonInjected)).toBeUndefined();
  expect(external).toEqual([]);
  await report.close();
  expect(await currentPacket(page)).toEqual(current);
});

test("requires saving edits before comparing and keeps the earlier selection", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator(earlierInput)).toBeEnabled();
  const earlier = await currentPacket(page);
  await page.locator(earlierInput).setInputFiles({ name: "before-edit.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(earlier)) });
  await expect(page.locator("#compare-json")).toBeEnabled();
  await page.getByLabel("Context").fill("Saved before comparing");
  for (const format of ["html", "markdown", "json"]) await expect(page.locator(`#compare-${format}`)).toBeDisabled();
  await expect(page.locator("#comparison-status")).toContainText("Save changes before comparing");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.locator("#save-status")).toContainText("Saved locally");
  await expect(page.locator("#comparison-status")).toContainText("before-edit.json");
  const ready = page.waitForEvent("download");
  await page.locator("#compare-json").click();
  const result = JSON.parse(await readFile((await (await ready).path())!, "utf8"));
  expect(result.assessment_after.context).toBe("Saved before comparing");
  expect(result.assessment_before).toEqual(earlier.assessment);
});

test("rejects invalid packets and allows a keyboard retry after a pending failure", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator(earlierInput)).toBeEnabled();
  const current = await currentPacket(page);
  const downloads: string[] = [];
  page.on("download", (download) => downloads.push(download.suggestedFilename()));
  const badDigest = JSON.parse(JSON.stringify(current));
  badDigest.integrity.digest = "0".repeat(64);
  for (const content of ["{", JSON.stringify(badDigest)]) {
    await page.locator(earlierInput).setInputFiles({ name: "invalid.json", mimeType: "application/json", buffer: Buffer.from(content) });
    await page.locator("#compare-html").click();
    await expect(page.locator("#comparison-status")).toContainText("The earlier packet is invalid, unsupported, or has a mismatched evidence digest");
    await expect(page.locator("#compare-html")).toBeEnabled();
  }
  expect(downloads).toEqual([]);
  await page.locator(earlierInput).setInputFiles({ name: "valid.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(current)) });
  let release!: () => void;
  const pending = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/compare?format=*", async (route) => {
    await pending;
    await route.fulfill({ status: 503, contentType: "application/json", body: '{"error":"Comparison temporarily unavailable."}' });
  });
  await page.locator("#compare-html").focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#comparison-controls")).toHaveAttribute("aria-busy", "true");
  await expect(page.locator(earlierInput)).toBeDisabled();
  await expect(page.getByLabel("Context")).toBeDisabled();
  await expect(page.locator("#export-json")).toBeDisabled();
  await expect(page.locator("#compare-markdown")).toBeDisabled();
  release();
  await expect(page.locator("#comparison-status")).toHaveText("Download failed. Comparison temporarily unavailable.");
  expect(downloads).toEqual([]);
  await expect(page.locator("#compare-html")).toBeFocused();
  await expect(page.locator(earlierInput)).toBeEnabled();
  await page.unroute("**/api/compare?format=*");
  const ready = page.waitForEvent("download");
  await page.keyboard.press("Enter");
  expect((await ready).suggestedFilename()).toBe("release-evidence-comparison.html");
  await expect(page.locator("#comparison-status")).toContainText("download started");
  expect(await currentPacket(page)).toEqual(current);
});

test("rejects empty and oversized files locally and renders filenames as text", async ({ page }) => {
  let comparisons = 0;
  page.on("request", (request) => { if (request.url().includes("/api/compare?")) comparisons++; });
  await page.goto("/");
  await expect(page.locator(earlierInput)).toBeEnabled();
  await page.locator(earlierInput).setInputFiles({ name: "empty.json", mimeType: "application/json", buffer: Buffer.alloc(0) });
  await expect(page.locator("#comparison-status")).toContainText("earlier packet is empty");
  await expect(page.locator("#compare-html")).toBeDisabled();
  await page.locator(earlierInput).setInputFiles({ name: "too-large.json", mimeType: "application/json", buffer: Buffer.alloc(4 * 1024 * 1024 + 1, " ") });
  await expect(page.locator("#comparison-status")).toContainText("exceeds the 4 MiB limit");
  await expect(page.locator("#compare-html")).toBeDisabled();
  const filename = '<img src=x onerror=alert(1)>.json';
  const current = await currentPacket(page);
  await page.locator(earlierInput).setInputFiles({ name: filename, mimeType: "application/json", buffer: Buffer.from(JSON.stringify(current)) });
  await expect(page.locator("#comparison-status")).toContainText(filename);
  await expect(page.locator("#comparison-status img")).toHaveCount(0);
  await expect(page.locator("#compare-html")).toBeEnabled();
  expect(comparisons).toBe(0);
});
