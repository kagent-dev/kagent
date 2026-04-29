"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { PlusCircle, Trash2, GitBranch, AlertCircle } from "lucide-react";
import { FieldHint, FieldLabel, FormSection } from "./form-primitives";
import {
  MAX_SKILLS_PER_SOURCE,
  applyGitSkillUrlPathChange,
  gitSkillRowUrlIssues,
  isDuplicateGitSkillFormRow,
  isDuplicateOciSkillRef,
  isValidSkillContainerImage,
  type GitSkillFormRow,
} from "@/lib/agentSkillsForm";
import type { GitRepo } from "@/types";

type AgentSkillsFormSectionProps = {
  skillRefs: string[];
  skillGitRepos: GitSkillFormRow[];
  skillsGitAuthSecretName: string;
  skillsError?: string;
  disabled: boolean;
  resolvedGitSkillRepos: GitRepo[];
  onSkillRefChange: (index: number, value: string) => void;
  onAddSkillRef: () => void;
  onRemoveSkillRef: (index: number) => void;
  onGitRowChange: (index: number, next: GitSkillFormRow) => void;
  onAddGitRow: () => void;
  onRemoveGitRow: (index: number) => void;
  onGitAuthSecretChange: (value: string) => void;
  onClearSkillsError: () => void;
};

export function AgentSkillsFormSection({
  skillRefs,
  skillGitRepos,
  skillsGitAuthSecretName,
  skillsError,
  disabled,
  resolvedGitSkillRepos,
  onSkillRefChange,
  onAddSkillRef,
  onRemoveSkillRef,
  onGitRowChange,
  onAddGitRow,
  onRemoveGitRow,
  onGitAuthSecretChange,
  onClearSkillsError,
}: AgentSkillsFormSectionProps) {
  return (
    <FormSection
      id="section-skills"
      title="Skills"
      description="Mount skill bundles from OCI images or Git. Files appear under /skills in the agent runtime."
    >
      <div className="space-y-6">
        <div>
          <FieldLabel className="text-sm">OCI images</FieldLabel>
          <FieldHint>One fully qualified image per line (e.g. GHCR). Each image is mounted as a directory under /skills.</FieldHint>
          <div className="mt-2 space-y-2">
            {skillRefs.map((ref, idx) => {
              const isDuplicate = isDuplicateOciSkillRef(ref, skillRefs);
              const isInvalid = ref.trim() !== "" && !isValidSkillContainerImage(ref);
              const hasError = isDuplicate || isInvalid;

              return (
                <div key={idx} className="space-y-1">
                  <div className="flex items-center gap-2">
                    <div className="min-w-0 flex-1">
                      <Input
                        id={idx === 0 ? "agent-oci-skill-0" : undefined}
                        name={`ociSkillRef-${idx}`}
                        placeholder="ghcr.io/example/python-skill:v1.0.0"
                        value={ref}
                        onChange={(e) => onSkillRefChange(idx, e.target.value)}
                        disabled={disabled}
                        className={hasError ? "border-destructive" : ""}
                        aria-invalid={hasError}
                        autoComplete="off"
                        spellCheck={false}
                        translate="no"
                      />
                      {isDuplicate && (
                        <p className="mt-1 flex items-center gap-1 text-xs text-destructive">
                          <AlertCircle className="h-3.5 w-3.5 shrink-0" aria-hidden />
                          Duplicate ref
                        </p>
                      )}
                      {isInvalid && (
                        <p className="mt-1 flex items-center gap-1 text-xs text-destructive">
                          <AlertCircle className="h-3.5 w-3.5 shrink-0" aria-hidden />
                          Expected registry/repository:tag
                        </p>
                      )}
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="icon"
                      onClick={onAddSkillRef}
                      disabled={skillRefs.length >= MAX_SKILLS_PER_SOURCE || disabled}
                      aria-label="Add OCI skill image"
                    >
                      <PlusCircle className="h-4 w-4" aria-hidden />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      onClick={() => onRemoveSkillRef(idx)}
                      disabled={skillRefs.length <= 1}
                      aria-label={`Remove OCI skill row ${idx + 1}`}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" aria-hidden />
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        <div className="border-t border-border/60 pt-6">
          <FieldLabel className="flex items-center gap-2 text-sm">
            <GitBranch className="h-4 w-4 text-muted-foreground" aria-hidden />
            Git repositories
          </FieldLabel>
          <FieldHint>
            Optional ref (branch, tag, or SHA) and path. For private remotes, reference a Secret in the agent namespace (HTTPS token
            or SSH key).
          </FieldHint>
          <div className="mt-3 space-y-4">
            {skillGitRepos.map((row, idx) => {
              const { hasExtraWithoutUrl, urlInvalid } = gitSkillRowUrlIssues(row);
              const dupGit = isDuplicateGitSkillFormRow(row, resolvedGitSkillRepos);

              return (
                <div key={idx} className="space-y-2 rounded-md border border-border/70 bg-muted/25 p-3">
                  <div>
                    <Label className="text-xs text-muted-foreground">Repository URL</Label>
                    <Input
                      className="mt-1"
                      placeholder="https://github.com/org/repo.git"
                      value={row.url}
                      onChange={(e) => {
                        onClearSkillsError();
                        onGitRowChange(
                          idx,
                          applyGitSkillUrlPathChange(row, { url: e.target.value }),
                        );
                      }}
                      disabled={disabled}
                      aria-invalid={hasExtraWithoutUrl || urlInvalid || dupGit}
                    />
                    {hasExtraWithoutUrl && (
                      <p className="mt-1 text-xs text-destructive">Set a URL when using ref, path, or name.</p>
                    )}
                    {urlInvalid && (
                      <p className="mt-1 text-xs text-destructive">Use https://, http://, git@, or ssh://</p>
                    )}
                    {dupGit && <p className="mt-1 text-xs text-destructive">Duplicate URL + ref + path</p>}
                  </div>
                  <div className="grid gap-2 sm:grid-cols-3">
                    <div>
                      <Label className="text-xs text-muted-foreground">Ref</Label>
                      <Input
                        className="mt-1"
                        placeholder="main"
                        value={row.ref}
                        onChange={(e) => {
                          onClearSkillsError();
                          onGitRowChange(idx, { ...row, ref: e.target.value });
                        }}
                        disabled={disabled}
                      />
                    </div>
                    <div>
                      <Label className="text-xs text-muted-foreground">Path in repo</Label>
                      <Input
                        className="mt-1"
                        placeholder="skills/myskill"
                        value={row.path}
                        onChange={(e) => {
                          onClearSkillsError();
                          onGitRowChange(
                            idx,
                            applyGitSkillUrlPathChange(row, { path: e.target.value }),
                          );
                        }}
                        disabled={disabled}
                      />
                    </div>
                    <div>
                      <Label className="text-xs text-muted-foreground">Name under /skills</Label>
                      <Input
                        className="mt-1"
                        placeholder="autodetected from path or URL"
                        value={row.name}
                        onChange={(e) => {
                          onClearSkillsError();
                          onGitRowChange(idx, { ...row, name: e.target.value });
                        }}
                        disabled={disabled}
                      />
                    </div>
                  </div>
                  <div className="flex justify-end gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      size="icon"
                      onClick={onAddGitRow}
                      disabled={skillGitRepos.length >= MAX_SKILLS_PER_SOURCE || disabled}
                      aria-label="Add Git skill repository"
                    >
                      <PlusCircle className="h-4 w-4" aria-hidden />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      onClick={() => onRemoveGitRow(idx)}
                      disabled={disabled}
                      aria-label={`Remove Git skill block ${idx + 1}`}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" aria-hidden />
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
          <div className="mt-4">
            <Label className="text-xs text-muted-foreground" htmlFor="git-skills-auth-secret">
              Git credentials secret (optional)
            </Label>
            <Input
              id="git-skills-auth-secret"
              className="mt-1"
              placeholder="e.g. git-auth (same namespace as agent)"
              value={skillsGitAuthSecretName}
              onChange={(e) => {
                onClearSkillsError();
                onGitAuthSecretChange(e.target.value);
              }}
              disabled={disabled}
            />
            <FieldHint className="mt-1">
              HTTPS: a <code className="text-xs">token</code> key. SSH: <code className="text-xs">ssh-privatekey</code>.
            </FieldHint>
          </div>
          {skillsError ? (
            <p className="mt-3 flex items-start gap-2 text-sm text-destructive" role="alert">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
              <span>{skillsError}</span>
            </p>
          ) : null}
        </div>
      </div>
    </FormSection>
  );
}
