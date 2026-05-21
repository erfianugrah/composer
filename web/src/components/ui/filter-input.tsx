import { cn } from "@/lib/utils";

export interface FilterInputProps {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  testId?: string;
  /** Tailwind width class. Defaults to `w-48`. */
  width?: string;
  className?: string;
}

/**
 * Standard list-page filter input.
 *
 * Replaces the 11 nearly-identical `<input type="search">` blocks scattered
 * across list components. Centralises the McMaster-style filter affordance:
 * narrow, monospace, ring-on-focus, `ml-auto` so it floats right by default.
 */
export function FilterInput({
  value,
  onChange,
  placeholder = "Filter…",
  testId,
  width = "w-48",
  className,
}: FilterInputProps) {
  return (
    <input
      type="search"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className={cn(
        "ml-auto h-7 rounded border border-input bg-transparent px-2 text-xs font-data placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
        width,
        className,
      )}
      data-testid={testId}
    />
  );
}
