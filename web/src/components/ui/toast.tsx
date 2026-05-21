import { useEffect, useState } from "react";

export type ToastVariant = "success" | "error" | "info";

interface Toast {
  id: number;
  variant: ToastVariant;
  message: string;
  /** Optional secondary line under the main message. */
  detail?: string;
  /** Auto-dismiss timeout in ms. 0 = persist until dismissed manually. */
  duration: number;
}

let nextId = 1;
const listeners = new Set<(toasts: Toast[]) => void>();
let toasts: Toast[] = [];

function emit() {
  for (const fn of listeners) fn(toasts);
}

function add(variant: ToastVariant, message: string, opts: { detail?: string; duration?: number } = {}) {
  const id = nextId++;
  const toast: Toast = {
    id,
    variant,
    message,
    detail: opts.detail,
    duration: opts.duration ?? (variant === "error" ? 8000 : 4000),
  };
  toasts = [...toasts, toast];
  emit();
  if (toast.duration > 0) {
    setTimeout(() => dismiss(id), toast.duration);
  }
  return id;
}

function dismiss(id: number) {
  toasts = toasts.filter((t) => t.id !== id);
  emit();
}

/**
 * Global toast API. Import and call from anywhere — no context provider needed.
 *
 * Mount a single <Toaster /> in the root layout to render the queue.
 */
export const toast = {
  success: (message: string, opts?: { detail?: string; duration?: number }) =>
    add("success", message, opts),
  error: (message: string, opts?: { detail?: string; duration?: number }) =>
    add("error", message, opts),
  info: (message: string, opts?: { detail?: string; duration?: number }) =>
    add("info", message, opts),
  dismiss,
};

const variantStyles: Record<ToastVariant, string> = {
  success: "border-cp-green/40 bg-cp-green/10 text-cp-green",
  error: "border-cp-red/40 bg-cp-red/10 text-cp-red",
  info: "border-cp-blue/40 bg-cp-blue/10 text-cp-blue",
};

const variantIcon: Record<ToastVariant, string> = {
  success: "✓",
  error: "✕",
  info: "ℹ",
};

/**
 * Toast queue renderer. Mount once in the root layout.
 *
 * Subscribes to the module-level toast queue and renders each toast as a
 * dismissible card in a bottom-right stack.
 */
export function Toaster() {
  const [items, setItems] = useState<Toast[]>([]);

  useEffect(() => {
    const fn = (t: Toast[]) => setItems(t);
    listeners.add(fn);
    fn(toasts); // seed
    return () => { listeners.delete(fn); };
  }, []);

  if (items.length === 0) return null;

  return (
    <div
      className="fixed bottom-6 left-6 z-50 flex flex-col gap-2 max-w-sm pointer-events-none"
      role="region"
      aria-label="Notifications"
      data-testid="toaster"
    >
      {items.map((t) => (
        <div
          key={t.id}
          className={`pointer-events-auto rounded-lg border px-3 py-2 shadow-lg text-xs animate-fade-in-up ${variantStyles[t.variant]}`}
          role={t.variant === "error" ? "alert" : "status"}
          data-testid={`toast-${t.variant}`}
        >
          <div className="flex items-start gap-2">
            <span aria-hidden="true" className="font-bold mt-0.5">{variantIcon[t.variant]}</span>
            <div className="flex-1 min-w-0">
              <p className="font-medium break-words">{t.message}</p>
              {t.detail && (
                <p className="mt-1 text-[10px] opacity-80 font-data break-words">{t.detail}</p>
              )}
            </div>
            <button
              type="button"
              onClick={() => dismiss(t.id)}
              aria-label="Dismiss notification"
              className="opacity-60 hover:opacity-100 text-xs leading-none ml-1"
            >
              ✕
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}
