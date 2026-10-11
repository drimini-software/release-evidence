import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1,
  timeout: 30_000,
  use: {
    baseURL: "http://127.0.0.1:43117",
    trace: "retain-on-failure",
  },
  webServer: {
    command: "go run ../cmd/release-evidence review --listen 127.0.0.1:43117 --idle-timeout 5s --state ../.cache/e2e-session-packet.json ../fixtures/session",
    url: "http://127.0.0.1:43117",
    reuseExistingServer: true,
    timeout: 30_000,
  },
  projects: [
    {
      name: "chromium",
      testMatch: "review.spec.ts",
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "chromium-reports",
      testMatch: "report.spec.ts",
      // Complete server-dependent checks before the standalone file reports.
      // The short idle timeout also cleans up Go's child process on Windows.
      dependencies: ["chromium"],
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
