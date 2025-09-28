import { Tag, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

type ProviderFilterProps = {
  providers: Set<string>;
  selectedProviders: Set<string>;
  onToggleProvider: (provider: string) => void;
  onSelectAll: () => void;
  onSelectNone: () => void;
};

const ProviderFilter = ({ providers, selectedProviders, onToggleProvider, onSelectAll, onSelectNone }: ProviderFilterProps) => (
  <div className="bg-secondary/50 p-4 rounded-lg border">
    <div className="mb-2 flex items-center justify-between">
      <div className="flex items-center gap-2">
        <Tag className="h-4 w-4" />
        <span className="font-medium">Filter by Provider</span>
      </div>
      <div className="flex gap-2">
        <Button variant="ghost" size="sm" onClick={onSelectAll}>
          All
        </Button>
        <Button variant="ghost" size="sm" onClick={onSelectNone}>
          None
        </Button>
      </div>
    </div>
    <div className="flex flex-wrap gap-2 mt-2">
      {Array.from(providers).map((provider) => (
        <Badge key={provider} variant={selectedProviders.has(provider) ? "default" : "outline"} className="cursor-pointer" onClick={() => onToggleProvider(provider)}>
          {provider}
          {selectedProviders.has(provider) && <X className="ml-1 h-3 w-3" />}
        </Badge>
      ))}
    </div>
  </div>
);

export default ProviderFilter;
