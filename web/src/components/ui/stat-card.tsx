import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export interface StatCardProps {
  label: string;
  value: string | number;
  color?: string;
  testId?: string;
}

/**
 * Compact stat card — McMaster-density (p-3, text-xl) instead of the
 * older p-6/text-2xl hero block. Use across list pages.
 */
export function StatCard({ label, value, color, testId }: StatCardProps) {
  return (
    <Card>
      <CardContent className="p-3" data-testid={testId}>
        <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
        <p className={cn("text-xl font-bold tabular-nums font-data", color)}>{value}</p>
      </CardContent>
    </Card>
  );
}
