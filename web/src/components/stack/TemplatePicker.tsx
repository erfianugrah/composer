import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface Template {
  id: string;
  name: string;
  description: string;
  category: string;
  icon: string;
}

interface Props {
  onSelect: (name: string, compose: string) => void;
}

export function TemplatePicker({ onSelect }: Props) {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [stackName, setStackName] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    apiFetch<{ templates: Template[] }>("/api/v1/templates").then(({ data }) => {
      if (data) setTemplates(data.templates || []);
    });
  }, []);

  async function handleCreate() {
    if (!selected || !stackName) return;
    setLoading(true);
    const { data } = await apiFetch<{ compose: string }>(`/api/v1/templates/${selected}`);
    if (data) onSelect(stackName, data.compose);
    setLoading(false);
  }

  // Group by category
  const categories = [...new Set(templates.map((t) => t.category))];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Create from Template</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <Input
          value={stackName} onChange={(e) => setStackName(e.target.value)}
          placeholder="Stack name (e.g. my-nginx)" required
          data-testid="template-stack-name"
        />

        <div className="space-y-3">
          {categories.map((cat) => (
            <div key={cat}>
              <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1">{cat}</p>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                {templates.filter((t) => t.category === cat).map((t) => (
                  <button
                    key={t.id}
                    onClick={() => setSelected(t.id)}
                    className={`flex items-center gap-2 rounded-lg border p-2 text-left text-sm transition-colors ${
                      selected === t.id
                        ? "border-cp-purple bg-cp-purple/10"
                        : "border-border hover:bg-accent/50"
                    }`}
                    data-testid={`template-${t.id}`}
                  >
                    <span className="text-lg">{t.icon}</span>
                    <div>
                      <div className="font-medium">{t.name}</div>
                      <div className="text-xs text-muted-foreground">{t.description}</div>
                    </div>
                  </button>
                ))}
              </div>
            </div>
          ))}
        </div>

        <Button
          onClick={handleCreate}
          disabled={!selected || !stackName || loading}
          className="w-full"
          data-testid="template-create-btn"
        >
          {loading ? "Creating..." : `Create ${stackName || "stack"} from ${selected || "template"}`}
        </Button>
      </CardContent>
    </Card>
  );
}
