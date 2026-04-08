import createClient from "openapi-fetch";
// Types will be generated from OpenAPI spec:
//   bunx openapi-typescript http://localhost:8080/openapi.json -o src/lib/api/types.ts
// For now, use a simple typed wrapper.

const baseUrl = typeof window !== "undefined" ? window.location.origin : "";

/** Raw fetch client -- use this for all API calls. */
export const api = createClient({ baseUrl, credentials: "include" });

/** Helper to handle API errors consistently. */
export async function apiCall<T>(
  fn: () => Promise<{ data?: T; error?: any; response: Response }>
): Promise<T> {
  const { data, error, response } = await fn();

  if (response.status === 401) {
    // Redirect to login on auth failure
    if (typeof window !== "undefined") {
      window.location.href = "/login";
    }
    throw new Error("Unauthorized");
  }

  if (error) {
    throw new Error(error.detail || error.title || `API error ${response.status}`);
  }

  return data as T;
}
