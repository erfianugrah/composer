/**
 * Extracts a user-friendly error message from a fetch Response.
 *
 * Priority:
 * 1. API response body (RFC 9457 detail/title, or generic message field)
 * 2. Status-based fallback (only when body has no useful message)
 */
export async function extractError(res: Response): Promise<string> {
  // Try to extract message from response body first
  try {
    const contentType = res.headers.get("content-type") || "";
    if (contentType.includes("json")) {
      const data = await res.json();
      // Huma RFC 9457: { detail, title, status }
      if (data.detail) return data.detail;
      if (data.title) return data.title;
      // Generic API patterns
      if (data.message) return data.message;
      if (data.error) return typeof data.error === "string" ? data.error : JSON.stringify(data.error);
    } else {
      // Non-JSON body (e.g. WAF HTML error page, plain text)
      const text = await res.text();
      const trimmed = text.trim();
      // Skip HTML responses (WAF error pages, etc.) -- use status fallback instead
      if (trimmed.length > 0 && trimmed.length < 500 && !trimmed.startsWith("<")) {
        return trimmed;
      }
    }
  } catch {
    // Body read failed -- fall through to status-based fallback
  }

  // Status-based fallback only when the body gave us nothing
  switch (res.status) {
    case 400: return "Bad request.";
    case 401: return "Invalid credentials.";
    case 403: return "Access denied. If unexpected, check firewall/WAF rules.";
    case 404: return "Not found.";
    case 409: return "Conflict -- resource already exists.";
    case 422: return "Validation error -- check your input.";
    case 429: return "Rate limited -- try again shortly.";
    case 500: return "Server error.";
    case 502: return "Bad gateway -- is the backend running?";
    case 503: return "Service unavailable.";
    case 504: return "Gateway timeout.";
    default:  return `Error (HTTP ${res.status}).`;
  }
}

/**
 * Extracts a user-friendly message from a caught fetch exception.
 * These are network-level failures (no HTTP response at all).
 */
export function networkError(err: unknown): string {
  if (err instanceof TypeError) {
    const msg = err.message.toLowerCase();
    if (msg.includes("failed to fetch") || msg.includes("networkerror")) {
      return "Cannot reach the server. Check that the container is running and accessible.";
    }
    if (msg.includes("cors") || msg.includes("opaque")) {
      return "Cross-origin request blocked. Check reverse proxy CORS configuration.";
    }
  }
  if (err instanceof DOMException && err.name === "AbortError") {
    return "Request timed out.";
  }
  return "Network error -- cannot connect to the server.";
}

/**
 * Wraps fetch with credentials, CSRF header, and structured error handling.
 * Returns { data, error } -- never throws.
 */
// P11: In-flight request dedup for GET requests
const inflight = new Map<string, Promise<{ data: unknown; error: null } | { data: null; error: string }>>();

export async function apiFetch<T = unknown>(
  url: string,
  init?: RequestInit,
): Promise<{ data: T; error: null } | { data: null; error: string }> {
  // Dedup identical GET requests that are still in flight
  const method = init?.method?.toUpperCase() || "GET";
  const dedup = method === "GET";
  if (dedup && inflight.has(url)) {
    return inflight.get(url)! as Promise<{ data: T; error: null } | { data: null; error: string }>;
  }

  const promise = (async () => {
  try {
    const res = await fetch(url, {
      credentials: "include",
      ...init,
      headers: {
        "X-Requested-With": "XMLHttpRequest",
        ...init?.headers,
      },
    });
    if (!res.ok) {
      return { data: null, error: await extractError(res) };
    }
    const contentType = res.headers.get("content-type") || "";
    if (res.status === 204 || !contentType.includes("json")) {
      return { data: null as unknown as T, error: null };
    }
    return { data: (await res.json()) as T, error: null };
  } catch (err) {
    return { data: null, error: networkError(err) };
  }
  })();

  if (dedup) {
    inflight.set(url, promise);
    promise.finally(() => inflight.delete(url));
  }

  return promise as Promise<{ data: T; error: null } | { data: null; error: string }>;
}
