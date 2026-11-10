import { useMemo, type ReactNode } from "react";
import { Button, Form, Input, Select, Space, Tag, Tooltip, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { ExternalLink, Plus, Trash } from "lucide-react";
import { runtimeConfig } from "@/api/runtimeConfig";
import {
  builtinSkillDraft,
  emptySkillDraft,
  matchBuiltinSkill,
  type SkillDraft,
  type SkillSourceKind,
} from "./agentTemplateDraft";

const { Text } = Typography;

const SOURCE_LABEL: Record<SkillSourceKind, string> = { oci: "OCI image", git: "Git", s3: "S3" };

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

  const muted = { color: theme.color.textMuted, fontSize: 12 };
  const mono = { fontFamily: theme.font.mono, fontSize: 12, wordBreak: "break-all" as const };

  // A line between skills, so each remove button reads as belonging to its own skill.
  const row = {
    display: "flex",
    flexDirection: "column" as const,
    gap: 6,
    "& + &": { paddingTop: 12, borderTop: `1px solid ${theme.color.border}` },
  };

  const header = (children: ReactNode, index: number, name: string) => (
    <div css={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", minHeight: 24 }}>
      {children}
      {readOnly ? null : (
        <Tooltip title={name.trim() ? `Remove ${name.trim()}` : "Remove this skill"}>
          <Button
            type="text"
            size="small"
            css={{ marginLeft: "auto" }}
            aria-label={name.trim() ? `Remove skill ${name.trim()}` : `Remove skill ${index + 1}`}
            data-testid={`template-form-skill-remove-${index}`}
            icon={<Trash size={14} />}
            onClick={() => remove(index)}
          />
        </Tooltip>
      )}
    </div>
  );

  const details = (entries: Array<[string, ReactNode]>) => (
    <dl
      css={{
        display: "grid",
        gridTemplateColumns: "max-content minmax(0, 1fr)",
        columnGap: 16,
        rowGap: 2,
        margin: 0,
        "& dt": { ...muted, lineHeight: "20px" },
        "& dd": { margin: 0, lineHeight: "20px", minWidth: 0 },
      }}
    >
      {entries.map(([label, value]) => (
        <div key={label} css={{ display: "contents" }}>
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );

  const link = (url: string, testId: string) =>
    /^https?:\/\//.test(url) ? (
      <a
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        data-testid={testId}
        css={{
          ...mono,
          color: theme.color.primaryText,
          textDecoration: "underline",
          textUnderlineOffset: 2,
          "&:hover": { textDecorationThickness: 2 },
          "&:active": { opacity: 0.8 },
          "&:focus-visible": { outline: `2px solid ${theme.color.textMuted}`, outlineOffset: 2, borderRadius: 2 },
        }}
      >
        {url}
        <ExternalLink size={12} aria-hidden css={{ marginLeft: 4, verticalAlign: "-1px" }} />
      </a>
    ) : (
      <span css={mono} data-testid={testId}>
        {url}
      </span>
    );

  const readOnlySource = (skill: SkillDraft, index: number): Array<[string, ReactNode]> => {
    const id = (field: string) => `template-form-skill-${field}-${index}`;
    const value = (text: string, field: string) => (
      <span css={mono} data-testid={id(field)}>
        {text}
      </span>
    );
    const entries: Array<[string, ReactNode]> = [];
    if (skill.sourceKind === "oci") entries.push(["Image", value(skill.oci, "oci")]);
    if (skill.sourceKind === "git") {
      entries.push(["Repository", link(skill.gitUrl, id("git-url"))]);
      entries.push(["Commit", value(skill.gitCommit, "git-commit")]);
    }
    if (skill.sourceKind === "s3") {
      entries.push(["Endpoint", link(skill.s3Endpoint, id("s3-endpoint"))]);
      entries.push(["Bucket", value(skill.s3Bucket, "s3-bucket")]);
      entries.push(["Key", value(skill.s3Key, "s3-key")]);
      entries.push(["Version", value(skill.s3VersionId, "s3-version")]);
      if (skill.s3Region) entries.push(["Region", value(skill.s3Region, "s3-region")]);
    }
    if (skill.path) entries.push(["Path", value(skill.path, "path")]);
    return entries;
  };

  return (
    <Form.Item
      label="Skills"
      extra="A skill is a set of instructions the agent loads when a task matches it. Its source must not change: an OCI image pinned by digest, a full Git commit, or an S3 object version. The path selects the skill's directory inside the source."
    >
      <div css={{ display: "flex", flexDirection: "column", gap: 12 }}>
        {readOnly && skills.length === 0 ? <Text css={muted}>No skills.</Text> : null}

        {skills.map((skill, index) => {
          const builtin = matchBuiltinSkill(skill, catalog);
          if (builtin && catalog) {
            return (
              <div key={index} css={row} data-testid={`template-form-skill-${index}`}>
                {header(
                  <>
                    <Tag css={{ marginInlineEnd: 0 }}>kagent</Tag>
                    <Text strong>{skill.name}</Text>
                    {builtin.current ? null : (
                      <Tag
                        color="warning"
                        css={{ marginInlineEnd: 0 }}
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
                  </>,
                  index,
                  skill.name,
                )}
                <Text css={muted}>{builtin.skill.description}</Text>
              </div>
            );
          }

          if (readOnly) {
            return (
              <div key={index} css={row} data-testid={`template-form-skill-${index}`}>
                {header(
                  <>
                    <Tag css={{ marginInlineEnd: 0 }} data-testid={`template-form-skill-kind-${index}`}>
                      {SOURCE_LABEL[skill.sourceKind]}
                    </Tag>
                    <Text strong data-testid={`template-form-skill-name-${index}`}>
                      {skill.name || "Not set"}
                    </Text>
                  </>,
                  index,
                  skill.name,
                )}
                {details(readOnlySource(skill, index))}
              </div>
            );
          }

          const input = (field: Exclude<keyof SkillDraft, "sourceKind">, testId: string, example: string, width: number) => (
            <Input
              css={{ width, maxWidth: "100%" }}
              data-testid={`template-form-skill-${testId}-${index}`}
              value={skill[field]}
              placeholder={example}
              onChange={(event) => setSkill(index, { [field]: event.target.value })}
            />
          );
          return (
            <div key={index} css={row} data-testid={`template-form-skill-${index}`}>
              {header(
                <>
                  {input("name", "name", "Skill name", 200)}
                  <div data-testid={`template-form-skill-kind-${index}`}>
                    <Select
                      css={{ width: 120 }}
                      value={skill.sourceKind}
                      onChange={(value: SkillSourceKind) => setSkill(index, { sourceKind: value })}
                      options={(Object.keys(SOURCE_LABEL) as SkillSourceKind[]).map((value) => ({
                        value,
                        title: SOURCE_LABEL[value],
                        label: SOURCE_LABEL[value],
                      }))}
                    />
                  </div>
                </>,
                index,
                skill.name,
              )}
              <Space size={8} wrap>
                {skill.sourceKind === "oci" ? input("oci", "oci", "ghcr.io/org/skills@sha256:…", 360) : null}
                {skill.sourceKind === "git" ? (
                  <>
                    {input("gitUrl", "git-url", "https://github.com/org/skills.git", 320)}
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
              </Space>
            </div>
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
      </div>
    </Form.Item>
  );
}
