import React from 'react';
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface VertexAIConfigSectionProps {
  projectId: string;
  location: string;
  onProjectIdChange: (value: string) => void;
  onLocationChange: (value: string) => void;
  errors?: {
    projectId?: string;
    location?: string;
  };
  isSubmitting: boolean;
  isLoading: boolean;
  isEditMode: boolean;
}

export const VertexAIConfigSection: React.FC<VertexAIConfigSectionProps> = ({
  projectId,
  location,
  onProjectIdChange,
  onLocationChange,
  errors = {},
  isSubmitting,
  isLoading,
  isEditMode,
}) => {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Vertex AI Configuration</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div>
          <label htmlFor="projectId" className="text-sm mb-2 block">Project ID</label>
          {isEditMode ? (
            <div className="flex-1 py-2 px-3 border rounded-md bg-muted">
              {projectId}
            </div>
          ) : (
            <Input
              id="projectId"
              value={projectId}
              onChange={(e) => onProjectIdChange(e.target.value)}
              placeholder="Enter your Google Cloud Project ID"
              disabled={isSubmitting || isLoading}
              className={errors.projectId ? "border-destructive" : ""}
            />
          )}
          {errors.projectId && <p className="text-destructive text-sm mt-1">{errors.projectId}</p>}
        </div>

        <div>
          <label htmlFor="location" className="text-sm mb-2 block">Location</label>
          {isEditMode ? (
            <div className="flex-1 py-2 px-3 border rounded-md bg-muted">
              {location}
            </div>
          ) : (
            <Input
              id="location"
              value={location}
              onChange={(e) => onLocationChange(e.target.value)}
              placeholder="Enter your Google Cloud Project Location (e.g., us-central1)"
              disabled={isSubmitting || isLoading}
              className={errors.location ? "border-destructive" : ""}
            />
          )}
          {errors.location && <p className="text-destructive text-sm mt-1">{errors.location}</p>}
          {!isEditMode && (
            <p className="text-[0.8rem] text-muted-foreground mt-1">
              The Google Cloud region where your Vertex AI resources are located.
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}; 