import { useMemo } from "react";
import { Button, Form, Input, Select, Space, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Plus, Trash } from "lucide-react";
import { runtimeConfig } from "@/api/runtimeConfig";
import {
  builtinSkillDraft,
  emptySkillDraft,
  matchBuiltinSkill,
  type SkillDraft,
  type SkillSourceKind,
} from "./agentTemplateDraft";

const { Text } = Typography;

/**
 * The template's `spec.skills`: kagent's built-in skills, offered from the catalog
 * the chart passes, and the reader's own skills by OCI, Git or S3 source.
 */
export function SkillsField({
  skills,
  onChange,
  readOnly,
}: {
  skills: SkillDraft[];
  onChange: (next: SkillDraft[]) => void;
  readOnly: boolean;
}) {
  const theme = useTheme();
  const catalog = useMemo(() => runtimeConfig().builtinSkills, []);

  const setSkill = (index: number, patch: Partial<SkillDraft>) => {
    const next = [...skills];
    next[index] = { ...next[index], ...patch };
    onChange(next);
  };
  const remove = (index: number) => onChange(skills.filter((_, at) => at !== index));

  const placeholder = (example: string) => (readOnly ? "Not set" : example);
  const readOnlyInput = readOnly ? ({ readOnly: true, variant: "borderless" } as const) : {};
  const readOnlySelect = readOnly
    ? ({ open: false, variant: "borderless", suffixIcon: null } as const)
    : {};
  const muted = { color: theme.color.textMuted, fontSize: 12 };

  return (
    <Form.Item
      label="Skills"
      extra="A skill is a set of instructions the agent loads when a task matches it. Its source must not change: an OCI image pinned by digest, a full Git commit, or an S3 object version. The path selects the skill's directory inside the source."
    >
      <Space orientation="vertical" size={8} css={{ display: "flex" }}>
        {readOnly && skills.length === 0 ? <Text css={muted}>No skills.</Text> : null}

        {skills.map((skill, index) => {
          const removeButton = readOnly ? null : (
            <Button
              type="text"
              aria-label={`Remove skill ${index + 1}`}
              data-testid={`template-form-skill-remove-${index}`}
              icon={<Trash size={14} />}
              onClick={() => remove(index)}
            />
          );

          const builtin = matchBuiltinSkill(skill, catalog);
          if (builtin && catalog) {
            return (
              <Space key={index} size={8} align="start" wrap data-testid={`template-form-skill-${index}`}>
                <Tag>kagent</Tag>
                <Text strong>{skill.name}</Text>
                {builtin.current ? null : (
                  <Tag
                    color="warning"
                    data-testid={`template-form-skill-outdated-${index}`}
                    title="Saved from an earlier kagent-tools image than this install ships."
                  >
                    Older version
                  </Tag>
                )}
                {builtin.current || readOnly ? null : (
                  <Button
                    size="small"
                    data-testid={`template-form-skill-update-${index}`}
                    onClick={() => setSkill(index, { oci: catalog.image })}
                  >
                    Update
                  </Button>
                )}
                <Text css={muted}>{builtin.skill.description}</Text>
                {removeButton}
              </Space>
            );
          }

          const input = (field: Exclude<keyof SkillDraft, "sourceKind">, testId: string, example: string, width: number) =>
            readOnly && skill[field] === "" ? null : (
              <Input
                // Reading has nothing to type into, so the box fits the whole value
                // instead of cutting it at the editing width.
                css={{
                  width: readOnly ? `calc(${skill[field].length}ch + 24px)` : width,
                  maxWidth: "100%",
                }}
                data-testid={`template-form-skill-${testId}-${index}`}
                value={skill[field]}
                placeholder={placeholder(example)}
                onChange={(event) => setSkill(index, { [field]: event.target.value })}
                {...readOnlyInput}
              />
            );
          return (
            <Space key={index} size={8} align="start" wrap data-testid={`template-form-skill-${index}`}>
              {input("name", "name", "Skill name", 180)}
              <div data-testid={`template-form-skill-kind-${index}`}>
                <Select
                  css={{ width: 120 }}
                  value={skill.sourceKind}
                  onChange={(value: SkillSourceKind) => setSkill(index, { sourceKind: value })}
                  options={[
                    { value: "oci", title: "OCI image", label: "OCI image" },
                    { value: "git", title: "Git", label: "Git" },
                    { value: "s3", title: "S3", label: "S3" },
                  ]}
                  {...readOnlySelect}
                />
              </div>
              {skill.sourceKind === "oci" ? input("oci", "oci", "ghcr.io/org/skills@sha256:…", 360) : null}
              {skill.sourceKind === "git" ? (
                <>
                  {input("gitUrl", "git-url", "https://github.com/org/skills.git", 280)}
                  {input("gitCommit", "git-commit", "Full commit ID", 340)}
                </>
              ) : null}
              {skill.sourceKind === "s3" ? (
                <>
                  {input("s3Endpoint", "s3-endpoint", "https://s3.amazonaws.com", 220)}
                  {input("s3Bucket", "s3-bucket", "Bucket", 140)}
                  {input("s3Key", "s3-key", "Key", 180)}
                  {input("s3VersionId", "s3-version", "Version ID", 160)}
                  {input("s3Region", "s3-region", "Region (optional)", 140)}
                </>
              ) : null}
              {input("path", "path", "Path (optional)", 180)}
              {removeButton}
            </Space>
          );
        })}

        {readOnly ? null : (
          <Space size={8} wrap>
            {catalog && catalog.skills.length > 0 ? (
              <div data-testid="template-form-add-builtin-skill">
                <Select
                  size="small"
                  css={{ minWidth: 220 }}
                  value={null}
                  placeholder="Add a kagent skill"
                  popupMatchSelectWidth={false}
                  onChange={(name: string) => {
                    const skill = catalog.skills.find((entry) => entry.name === name);
                    if (skill) onChange([...skills, builtinSkillDraft(catalog, skill)]);
                  }}
                  options={catalog.skills.map((skill) => ({
                    value: skill.name,
                    title: skill.name,
                    disabled: skills.some((row) => row.name.trim() === skill.name),
                    label: (
                      <Space orientation="vertical" size={0}>
                        <span>{skill.name}</span>
                        <Text css={{ ...muted, whiteSpace: "normal" }}>{skill.description}</Text>
                      </Space>
                    ),
                  }))}
                />
              </div>
            ) : null}
            <Button
              size="small"
              icon={<Plus size={13} />}
              data-testid="template-form-add-skill"
              onClick={() => onChange([...skills, emptySkillDraft()])}
            >
              Add your own skill
            </Button>
          </Space>
        )}
      </Space>
    </Form.Item>
  );
}
