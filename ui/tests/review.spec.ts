import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("renders the repository evidence and human review controls", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { level: 1 })).toContainText("Facts first");
  await expect(page.locator("#summary")).toContainText("session");
  await expect(page.locator("#findings > article")).toHaveCount(9);
  await expect(page.getByText("No repository data was sent to an external service.")).toBeVisible();
  await expect(page.getByText("This page edits one review packet for this repository.")).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Review path" }).getByRole("link")).toHaveCount(3);
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();
  await expect(page.getByRole("status")).toContainText("No unsaved changes");
});

test("saves a human assessment through the protected local API", async ({ page }) => {
  await page.goto("/");

  await page.getByLabel("Assessment status").selectOption("in_progress");
  await page.getByLabel("Context").fill("Automated local review check");
  await expect(page.getByRole("status")).toHaveText("Unsaved changes in this packet.");
  await expect(page.getByRole("button", { name: "Save changes" })).toBeEnabled();
  await page.locator("#decision-status").selectOption("needs_work");
  await page.getByLabel("Decision owner").fill("Local reviewer");
  await page.getByLabel("Decision rationale").fill("CI and recovery evidence remain open.");

  const firstFinding = page.locator("#findings > article").first();
  await firstFinding.getByLabel("Disposition").selectOption("accepted");
  await firstFinding.getByLabel("Owner", { exact: true }).fill("Local reviewer");
  await page.getByRole("button", { name: "Save changes" }).click();

  await expect(page.getByRole("status")).toHaveText(/Saved locally at \d{2}:\d{2}:\d{2} UTC\./);
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();

  await page.getByLabel("Decision rationale").fill("A second local change.");
  await expect(page.getByRole("status")).toHaveText("Unsaved changes in this packet.");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("status")).toHaveText(/Saved locally at \d{2}:\d{2}:\d{2} UTC\./);

  await page.reload();
  await expect(page.getByLabel("Decision rationale")).toHaveValue("A second local change.");
  await expect(page.getByRole("status")).toContainText("No unsaved changes. Last saved");
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
