"use client";
import React, { useState } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { addGitRepo } from "@/app/actions/gitrepos";
import { toast } from "sonner";

interface ValidationErrors {
  name?: string;
  url?: string;
  branch?: string;
}

export default function AddGitRepoPage() {
  const router = useRouter();

  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [branch, setBranch] = useState("main");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errors, setErrors] = useState<ValidationErrors>({});

  const validateForm = (): boolean => {
    const newErrors: ValidationErrors = {};

    if (!name.trim()) {
      newErrors.name = "Name is required";
    } else if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(name.trim())) {
      newErrors.name = "Name must contain only lowercase letters, numbers, and hyphens";
    }

    if (!url.trim()) {
      newErrors.url = "Repository URL is required";
    } else {
      try {
        new URL(url.trim());
      } catch {
        newErrors.url = "Enter a valid URL";
      }
    }

    if (branch.trim() && !/^[a-zA-Z0-9._/-]+$/.test(branch.trim())) {
      newErrors.branch = "Branch name contains invalid characters";
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validateForm()) {
      toast.error("Please fix the validation errors.");
      return;
    }

    setIsSubmitting(true);

    try {
      const response = await addGitRepo({
        name: name.trim(),
        url: url.trim(),
        branch: branch.trim() || "main",
      });

      if (response.error) {
        throw new Error(response.error);
      }

      toast.success(`Repo "${name.trim()}" added successfully! Cloning in background...`);
      router.push("/git");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to add repo");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen p-8">
      <div className="max-w-3xl mx-auto">
        <h1 className="text-2xl font-bold mb-8">Add Git Repo</h1>

        <div className="space-y-6">
          <div>
            <Label className="text-base mb-2 block font-bold">Name</Label>
            <p className="text-xs mb-2 block text-muted-foreground">
              A unique name to identify this repository. Lowercase letters, numbers, and hyphens only.
            </p>
            <Input
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (errors.name) setErrors((prev) => ({ ...prev, name: undefined }));
              }}
              placeholder="e.g. kagent"
              disabled={isSubmitting}
              className={errors.name ? "border-red-500" : ""}
            />
            {errors.name && <p className="text-red-500 text-sm mt-1">{errors.name}</p>}
          </div>

          <div>
            <Label className="text-base mb-2 block font-bold">Repository URL</Label>
            <p className="text-xs mb-2 block text-muted-foreground">
              The HTTPS URL of the git repository to clone.
            </p>
            <Input
              value={url}
              onChange={(e) => {
                setUrl(e.target.value);
                if (errors.url) setErrors((prev) => ({ ...prev, url: undefined }));
              }}
              placeholder="https://github.com/kagent-dev/kagent.git"
              disabled={isSubmitting}
              className={errors.url ? "border-red-500" : ""}
            />
            {errors.url && <p className="text-red-500 text-sm mt-1">{errors.url}</p>}
          </div>

          <div>
            <Label className="text-base mb-2 block font-bold">Branch</Label>
            <p className="text-xs mb-2 block text-muted-foreground">
              The branch to clone. Defaults to &quot;main&quot; if left empty.
            </p>
            <Input
              value={branch}
              onChange={(e) => {
                setBranch(e.target.value);
                if (errors.branch) setErrors((prev) => ({ ...prev, branch: undefined }));
              }}
              placeholder="main"
              disabled={isSubmitting}
              className={errors.branch ? "border-red-500" : ""}
            />
            {errors.branch && <p className="text-red-500 text-sm mt-1">{errors.branch}</p>}
          </div>
        </div>

        <div className="flex justify-between pt-6">
          <Button
            variant="outline"
            onClick={() => router.push("/git")}
            disabled={isSubmitting}
          >
            Cancel
          </Button>
          <Button
            variant="default"
            onClick={handleSubmit}
            disabled={isSubmitting}
          >
            {isSubmitting ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Adding...
              </>
            ) : (
              "Add Repo"
            )}
          </Button>
        </div>
      </div>
    </div>
  );
}
