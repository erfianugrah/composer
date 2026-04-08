import { test, expect } from "@playwright/test";

test.describe("Smoke tests", () => {
  test("login page renders with Composer branding", async ({ page }) => {
    await page.goto("/login");

    // Verify the page loaded
    await expect(page).toHaveTitle(/Composer/);

    // Check branding
    const brand = page.locator("text=Composer").first();
    await expect(brand).toBeVisible();

    // Check login form exists
    const form = page.getByTestId("login-form");
    await expect(form).toBeVisible();

    // Check form inputs
    const emailInput = page.getByTestId("login-email");
    await expect(emailInput).toBeVisible();
    await expect(emailInput).toHaveAttribute("type", "email");

    const passwordInput = page.getByTestId("login-password");
    await expect(passwordInput).toBeVisible();
    await expect(passwordInput).toHaveAttribute("type", "password");

    // Check submit button
    const submitBtn = page.getByTestId("login-submit");
    await expect(submitBtn).toBeVisible();
    await expect(submitBtn).toHaveText("Sign in");
  });

  test("login form shows error on invalid credentials", async ({ page }) => {
    await page.goto("/login");

    // Fill in bad credentials
    await page.getByTestId("login-email").fill("bad@example.com");
    await page.getByTestId("login-password").fill("wrongpassword");
    await page.getByTestId("login-submit").click();

    // Should show an error (network error since no backend in static preview)
    const error = page.getByTestId("login-error");
    await expect(error).toBeVisible({ timeout: 5000 });
  });

  test("dashboard page renders with sidebar and header", async ({ page }) => {
    await page.goto("/");

    // Should have the page title in header
    const title = page.getByTestId("page-title");
    await expect(title).toHaveText("Dashboard");

    // Sidebar nav items should exist
    await expect(page.getByTestId("nav-dashboard")).toBeVisible();
    await expect(page.getByTestId("nav-stacks")).toBeVisible();
    await expect(page.getByTestId("nav-settings")).toBeVisible();

    // Dashboard active in sidebar
    const dashboardNav = page.getByTestId("nav-dashboard");
    await expect(dashboardNav).toHaveClass(/text-cp-purple/);
  });

  test("stacks page renders", async ({ page }) => {
    await page.goto("/stacks");

    const title = page.getByTestId("page-title");
    await expect(title).toHaveText("Stacks");
  });

  test("Lovelace theme is applied (dark background)", async ({ page }) => {
    await page.goto("/login");

    // Verify the body has the dark background
    const bgColor = await page.evaluate(() => {
      return getComputedStyle(document.body).backgroundColor;
    });

    // The bg should be dark (cp-950: #15161e -> rgb(21, 22, 30))
    expect(bgColor).toMatch(/rgb\(21,\s*22,\s*30\)/);
  });

  test("sidebar navigation links work", async ({ page }) => {
    await page.goto("/");

    // Click "Stacks" in sidebar
    await page.getByTestId("nav-stacks").click();
    await expect(page).toHaveURL(/\/stacks/);

    // Click "Dashboard" in sidebar
    await page.getByTestId("nav-dashboard").click();
    await expect(page).toHaveURL(/\/$/);
  });
});
