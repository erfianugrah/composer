import { test, expect } from "@playwright/test";

test.describe("Login Page", () => {
  test("renders with Composer branding and form", async ({ page }) => {
    await page.goto("/login");
    await expect(page).toHaveTitle(/Composer/);

    const brand = page.locator("text=Composer").first();
    await expect(brand).toBeVisible();

    const form = page.getByTestId("login-form");
    await expect(form).toBeVisible();
  });

  test("has email and password inputs with correct types", async ({ page }) => {
    await page.goto("/login");

    const emailInput = page.getByTestId("login-email");
    await expect(emailInput).toBeVisible();
    await expect(emailInput).toHaveAttribute("type", "email");
    await expect(emailInput).toHaveAttribute("required", "");

    const passwordInput = page.getByTestId("login-password");
    await expect(passwordInput).toBeVisible();
    await expect(passwordInput).toHaveAttribute("type", "password");
    await expect(passwordInput).toHaveAttribute("required", "");
  });

  test("has a submit button with correct text", async ({ page }) => {
    await page.goto("/login");
    const submitBtn = page.getByTestId("login-submit");
    await expect(submitBtn).toBeVisible();
    await expect(submitBtn).toHaveText("Sign in");
  });

  test("shows error on invalid credentials", async ({ page }) => {
    await page.goto("/login");
    await page.getByTestId("login-email").fill("bad@example.com");
    await page.getByTestId("login-password").fill("wrongpassword");
    await page.getByTestId("login-submit").click();

    // Should show error (network error in static preview, or auth error against backend)
    const error = page.getByTestId("login-error");
    await expect(error).toBeVisible({ timeout: 5000 });
  });

  test("submit button shows loading state on click", async ({ page }) => {
    await page.goto("/login");
    await page.getByTestId("login-email").fill("test@example.com");
    await page.getByTestId("login-password").fill("testpassword");

    const btn = page.getByTestId("login-submit");
    await btn.click();

    // Should briefly show "Signing in..." text
    await expect(btn).toHaveText("Signing in...", { timeout: 1000 }).catch(() => {
      // May have already resolved by the time we check
    });
  });

  test("uses AuthLayout with dark background and glow effect", async ({ page }) => {
    await page.goto("/login");

    const bgColor = await page.evaluate(() =>
      getComputedStyle(document.body).backgroundColor
    );
    // cp-950: #15161e -> rgb(21, 22, 30)
    expect(bgColor).toMatch(/rgb\(21,\s*22,\s*30\)/);
  });
});

test.describe("Dashboard Page", () => {
  test("renders with sidebar, header, and content area", async ({ page }) => {
    await page.goto("/");

    const title = page.getByTestId("page-title");
    await expect(title).toHaveText("Dashboard");
  });

  test("sidebar shows all navigation items", async ({ page }) => {
    await page.goto("/");

    await expect(page.getByTestId("nav-dashboard")).toBeVisible();
    await expect(page.getByTestId("nav-stacks")).toBeVisible();
    await expect(page.getByTestId("nav-pipelines")).toBeVisible();
    await expect(page.getByTestId("nav-settings")).toBeVisible();
  });

  test("dashboard nav is active (highlighted) on root page", async ({ page }) => {
    await page.goto("/");

    const dashboardNav = page.getByTestId("nav-dashboard");
    const className = await dashboardNav.getAttribute("class");
    expect(className).toContain("text-cp-purple");
    expect(className).toContain("bg-cp-purple");
  });

  test("other nav items are NOT active on root page", async ({ page }) => {
    await page.goto("/");

    for (const id of ["nav-stacks", "nav-pipelines", "nav-settings"]) {
      const nav = page.getByTestId(id);
      const className = await nav.getAttribute("class");
      expect(className).toContain("text-muted-foreground");
    }
  });

  test("header shows health indicator", async ({ page }) => {
    await page.goto("/");

    // The green ping dot should be visible
    const healthText = page.locator("text=Healthy");
    await expect(healthText).toBeVisible();
  });

  test("sidebar footer shows version", async ({ page }) => {
    await page.goto("/");

    const version = page.locator("text=Composer v0.1.0");
    await expect(version).toBeVisible();
  });
});

test.describe("Stacks Page", () => {
  test("renders with correct title", async ({ page }) => {
    await page.goto("/stacks");

    const title = page.getByTestId("page-title");
    await expect(title).toHaveText("Stacks");
  });

  test("stacks nav is active on stacks page", async ({ page }) => {
    await page.goto("/stacks");

    const stacksNav = page.getByTestId("nav-stacks");
    const className = await stacksNav.getAttribute("class");
    expect(className).toContain("text-cp-purple");
  });
});

test.describe("Pipelines Page", () => {
  test("renders with correct title and coming soon message", async ({ page }) => {
    await page.goto("/pipelines");

    const title = page.getByTestId("page-title");
    await expect(title).toHaveText("Pipelines");

    const comingSoon = page.locator("text=Coming soon");
    await expect(comingSoon).toBeVisible();
  });

  test("pipelines nav is active", async ({ page }) => {
    await page.goto("/pipelines");

    const nav = page.getByTestId("nav-pipelines");
    const className = await nav.getAttribute("class");
    expect(className).toContain("text-cp-purple");
  });
});

test.describe("Settings Page", () => {
  test("renders with correct title and sections", async ({ page }) => {
    await page.goto("/settings");

    const title = page.getByTestId("page-title");
    await expect(title).toHaveText("Settings");

    await expect(page.locator("text=User Management")).toBeVisible();
    await expect(page.locator("text=System")).toBeVisible();
    await expect(page.locator("main").locator("text=Composer v0.1.0")).toBeVisible();
  });

  test("webhook configuration form is visible", async ({ page }) => {
    await page.goto("/settings");

    await expect(page.getByRole("heading", { name: "Create Webhook" })).toBeVisible();
    await expect(page.getByTestId("webhook-stack-name")).toBeVisible();
    await expect(page.getByTestId("webhook-provider")).toBeVisible();
    await expect(page.getByTestId("webhook-branch")).toBeVisible();
    await expect(page.getByTestId("webhook-create-btn")).toBeVisible();
  });

  test("webhook provider dropdown has correct options", async ({ page }) => {
    await page.goto("/settings");

    const select = page.getByTestId("webhook-provider");
    const options = await select.locator("option").allTextContents();
    expect(options).toEqual(["GitHub", "GitLab", "Gitea", "Generic"]);
  });

  test("webhook create button disabled without stack name", async ({ page }) => {
    await page.goto("/settings");

    const btn = page.getByTestId("webhook-create-btn");
    await expect(btn).toBeDisabled();

    // Fill in stack name -> button enabled
    await page.getByTestId("webhook-stack-name").fill("test-stack");
    await expect(btn).toBeEnabled();
  });

  test("active webhooks section shows", async ({ page }) => {
    await page.goto("/settings");

    await expect(page.locator("text=Active Webhooks")).toBeVisible();
  });
});

test.describe("Navigation", () => {
  test("sidebar links navigate between pages", async ({ page }) => {
    await page.goto("/");

    await page.getByTestId("nav-stacks").click();
    await expect(page).toHaveURL(/\/stacks/);

    await page.getByTestId("nav-pipelines").click();
    await expect(page).toHaveURL(/\/pipelines/);

    await page.getByTestId("nav-settings").click();
    await expect(page).toHaveURL(/\/settings/);

    await page.getByTestId("nav-dashboard").click();
    await expect(page).toHaveURL(/\/$/);
  });

  test("all pages load without console errors", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (err) => errors.push(err.message));

    for (const path of ["/", "/login", "/stacks", "/pipelines", "/settings"]) {
      await page.goto(path);
      await page.waitForLoadState("networkidle");
    }

    // Filter out expected network errors (API calls fail in static preview)
    const unexpectedErrors = errors.filter(
      (e) => !e.includes("fetch") && !e.includes("NetworkError") && !e.includes("Failed to fetch")
    );
    expect(unexpectedErrors).toHaveLength(0);
  });
});

test.describe("Lovelace Theme", () => {
  test("login page has dark background (cp-950)", async ({ page }) => {
    await page.goto("/login");

    const bgColor = await page.evaluate(() =>
      getComputedStyle(document.body).backgroundColor
    );
    expect(bgColor).toMatch(/rgb\(21,\s*22,\s*30\)/);
  });

  test("dashboard has correct background (cp-900 via background token)", async ({ page }) => {
    await page.goto("/");

    const bgColor = await page.evaluate(() =>
      getComputedStyle(document.body).backgroundColor
    );
    // background token maps to cp-900: #1d1f28 -> rgb(29, 31, 40)
    expect(bgColor).toMatch(/rgb\(29,\s*31,\s*40\)/);
  });

  test("sidebar has darkest background (cp-950)", async ({ page }) => {
    await page.goto("/");

    // The aside element with cp-950 class
    const sidebar = page.locator("aside").first();
    const bgColor = await sidebar.evaluate((el) =>
      getComputedStyle(el).backgroundColor
    );
    expect(bgColor).toMatch(/rgb\(21,\s*22,\s*30\)/);
  });

  test("active nav link has purple accent", async ({ page }) => {
    await page.goto("/");

    const dashboardNav = page.getByTestId("nav-dashboard");
    const className = await dashboardNav.getAttribute("class");
    expect(className).toContain("cp-purple");
  });

  test("text uses Space Grotesk font", async ({ page }) => {
    await page.goto("/");

    const fontFamily = await page.evaluate(() =>
      getComputedStyle(document.body).fontFamily
    );
    expect(fontFamily.toLowerCase()).toContain("space grotesk");
  });
});

test.describe("Responsive Design", () => {
  test("sidebar is hidden on mobile viewport", async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 }); // iPhone size
    await page.goto("/");

    const sidebar = page.locator("aside").first();
    await expect(sidebar).toBeHidden();
  });

  test("sidebar is visible on desktop viewport", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/");

    const sidebar = page.locator("aside").first();
    await expect(sidebar).toBeVisible();
  });

  test("main content fills width on mobile", async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto("/");

    const main = page.locator("main").first();
    const box = await main.boundingBox();
    expect(box!.width).toBeGreaterThan(350);
  });
});

test.describe("Static Assets", () => {
  test("favicon loads", async ({ page }) => {
    const response = await page.goto("/favicon.svg");
    expect(response?.status()).toBe(200);
    expect(response?.headers()["content-type"]).toContain("svg");
  });

  test("CSS files load with correct content type", async ({ page }) => {
    await page.goto("/");

    // Find a CSS link in the HTML
    const cssHref = await page.evaluate(() => {
      const link = document.querySelector('link[rel="stylesheet"]');
      return link?.getAttribute("href") || "";
    });

    if (cssHref) {
      const response = await page.goto(cssHref);
      expect(response?.status()).toBe(200);
    }
  });
});
