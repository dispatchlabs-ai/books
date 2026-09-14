import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  use: { baseURL: "http://127.0.0.1:8789" },
  webServer: {
    command: "BOOKS_WEB_PORT=8789 node server.mjs",
    url: "http://127.0.0.1:8789",
    reuseExistingServer: false,
    env: { BOOKS_API_URL: "", BOOKS_API_TOKEN_FILE: "", BOOKS_AGENT_URL: "" },
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"] } },
    {
      name: "mobile",
      use: { ...devices["iPhone 13"], defaultBrowserType: "chromium" },
    },
  ],
});
