import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

interface CategoryFilterProps {
  categories: Set<string>;
  selectedCategories: Set<string>;
  onToggleCategory: (category: string) => void;
  onSelectAll: () => void;
  onClearAll: () => void;
}

const CategoryFilter: React.FC<CategoryFilterProps> = ({
  categories,
  selectedCategories,
  onToggleCategory,
  onSelectAll,
  onClearAll,
}) => {
  return (
    <div className="mb-6 p-4 border rounded-md bg-secondary/10">
      <h3 className="text-sm font-medium mb-3">Filter by Category</h3>
      <div className="flex flex-wrap gap-2 mb-3">
        {Array.from(categories)
          .sort()
          .map((category) => (
            <Badge
              key={category}
              variant={selectedCategories.has(category) ? "default" : "outline"}
              className="cursor-pointer capitalize"
              onClick={() => onToggleCategory(category)}
            >
              {category}
            </Badge>
          ))}
      </div>
      <div className="flex justify-end gap-2 mt-3">
        <Button variant="ghost" size="sm" onClick={onClearAll}>
          Clear All
        </Button>
        <Button variant="ghost" size="sm" onClick={onSelectAll}>
          Select All
        </Button>
      </div>
    </div>
  );
};

export default CategoryFilter; 