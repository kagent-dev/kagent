import { Card, Typography } from "antd";
import { useTheme, type Theme } from "@emotion/react";
import { useSearchParams } from "react-router-dom";

const { Text } = Typography;


export function AgentConcepts() {
  const theme = useTheme();
  const [, setParams] = useSearchParams();

  const open = (tab: string) =>
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.set("tab", tab);
        return next;
      },
      { replace: true },
    );

  return (
    <Card
      size="small"
      title="Overview"
      data-testid="agent-concepts"
      css={{ marginBottom: theme.space(4) }}
    >
      {/* One sentence per line: each is a separate fact. */}
      <Line theme={theme} testId="concepts-pairing">
        An agent is one template plus one harness.
      </Line>
      <Line theme={theme}>
        The template is what it does; the harness is where and how it runs.
      </Line>
      <Line theme={theme} testId="concepts-sources">
        Point to a shared template or harness, or write one inline.
      </Line>
      <Line theme={theme}>Each chat you start belongs to that agent.</Line>

      {/*
        Where these names come from, for a reader who has met the other half.

        The runtime words on this page — worker, actor, scheduled — are Agent
        Substrate's, not invented here, and somebody who has read either project's docs
        is served by knowing they are the same words. Said once, at the foot, because it
        is provenance rather than something needed to use the page.
      */}
      <Line theme={theme} testId="concepts-substrate">
        These map to{" "}
        <a
          href="https://github.com/agent-substrate/substrate"
          target="_blank"
          rel="noreferrer"
          css={{ color: theme.color.primaryText }}
        >
          Agent Substrate
        </a>{" "}
        concepts. A harness draws on a pool of <b>workers</b>, and each AgentInstance
        runs as an <b>actor</b> scheduled onto one of them.
      </Line>


      {/*
        Full width and reflowing, rather than four boxes at a fixed size.

        The chain is the explanation, so it should read as one line wherever there is
        room for one — and wrap into two rather than shrinking to a column of labels
        nobody can tell apart.
      */}
      <div
        css={{
          display: "flex",
          alignItems: "stretch",
          gap: theme.space(3),
          // Clear of the sentences above: the prose says what the thing is and the
          // diagram is a separate statement of the same, not a continuation of it.
          marginTop: theme.space(5),
          flexWrap: "wrap",
        }}
      >
        {/* The two halves, stacked, because they combine rather than follow one
            another. They share a column so the arrow out of them is one arrow. */}
        <div css={{ display: "grid", gap: theme.space(2), flex: "1 1 260px", minWidth: 0 }}>
          <Box
            kind="AgentTemplate"
            detail="The model, prompt and tools."
            onOpen={() => open("templates")}
          />
          <Box
            kind="Harness"
            detail="The runtime, image and worker pool."
            onOpen={() => open("harnesses")}
          />
        </div>

        <MergeArrow theme={theme} />
        <Box
          kind="Agent"
          detail="One template and one harness, each shared or inline."
          onOpen={() => open("agents")}
        />
        <FlowArrow theme={theme} />
        <Box kind="AgentInstance" detail="One chat with an agent, run as a Substrate Actor." />
      </div>

    </Card>
  );
}

/**
 * One concept, and — where it has a tab — the way to it.
 *
 * A card with an `onOpen` is a button in every sense that matters, so it is one: a
 * `div` with a click handler is unreachable by keyboard and announces nothing, and this
 * is navigation rather than decoration.
 */
function Box({
  kind,
  detail,
  onOpen,
}: {
  kind: string;
  detail: string;
  onOpen?: () => void;
}) {
  const theme = useTheme();
  const interactive = Boolean(onOpen);

  return (
    <button
      type="button"
      data-testid={`concept-${kind}`}
      disabled={!interactive}
      onClick={onOpen}
      css={{
        flex: "1 1 220px",
        minWidth: 0,
        textAlign: "left",
        // Centred down the box, because a card that stretches to match the two stacked
        // beside it would otherwise hold its text at the top of an empty rectangle.
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
        gap: 2,
        font: "inherit",
        padding: `${theme.space(3)} ${theme.space(3)}`,
        borderRadius: theme.radius.md,
        border: `1px solid ${theme.color.border}`,
        background: theme.color.bg,
        transition: "background 140ms ease, border-color 140ms ease, transform 140ms ease",
        cursor: interactive ? "pointer" : "default",
        ...(interactive
          ? {
              "&:hover": {
                borderColor: `${theme.color.primary}A6`,
                background: theme.color.bgElevated,
              },
              // Pressed, and distinct from hovered: a press that looks like a hover
              // reads as a click that did not register.
              "&:active": {
                background: `${theme.color.primary}33`,
                transform: "translateY(1px)",
              },
              "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}` },
            }
          : {}),
      }}
    >
      <div
        css={{
          fontFamily: theme.font.mono,
          fontSize: 13,
          // The derived one is marked, because it is the one that is not a resource and
          // the confusion is people looking for where to create it.
          color: theme.color.text,
        }}
      >
        {kind}
      </div>
      <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{detail}</Text>
    </button>
  );
}

/** One sentence, on its own line. */
function Line({
  theme,
  testId,
  children,
}: {
  theme: Theme;
  testId?: string;
  children: React.ReactNode;
}) {
  return (
    <Text
      data-testid={testId}
      css={{
        display: "block",
        color: theme.color.textMuted,
        fontSize: 12,
        // Loose enough that three lines read as three separate facts rather than as a
        // block of text to be got through.
        lineHeight: 1.9,
      }}
    >
      {children}
    </Text>
  );
}

/*
 * The connectors, drawn rather than set in an icon font.
 *
 * A lucide arrow between the boxes was legible and said nothing: the two on the left do
 * not *follow* each other into the Agent, they *combine* into it, and one glyph repeated
 * twice drew the same relationship for both steps. These are two different shapes for
 * two different relationships — a merge and a flow — which is the whole reason the
 * diagram is a diagram.
 *
 * Both are hidden from assistive technology: the prose above says what they say, and an
 * announced "arrow" between two named boxes is noise.
 */

/** Two lines converging into one: the pair becoming an agent. */
function MergeArrow({ theme }: { theme: Theme }) {
  const id = "concepts-merge";
  return (
    <div
      aria-hidden
      css={{ display: "flex", alignItems: "stretch", flexShrink: 0, width: 44 }}
    >
      <svg width="44" height="100%" viewBox="0 0 44 96" preserveAspectRatio="none" fill="none">
        <defs>
          <linearGradient id={id} x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor={theme.color.primaryText} stopOpacity="0.4" />
            <stop offset="100%" stopColor={theme.color.primaryText} />
          </linearGradient>
        </defs>
        {/* From the middle of each box on the left, curving into a single line. */}
        <path
          d="M0 24 C 16 24, 16 48, 28 48"
          stroke={`url(#${id})`}
          strokeWidth="1.5"
          vectorEffect="non-scaling-stroke"
        />
        <path
          d="M0 72 C 16 72, 16 48, 28 48"
          stroke={`url(#${id})`}
          strokeWidth="1.5"
          vectorEffect="non-scaling-stroke"
        />
        <path
          d="M28 48 L 38 48"
          stroke={theme.color.primaryText}
          strokeWidth="1.5"
          vectorEffect="non-scaling-stroke"
        />
        <path
          d="M34 44 L 39 48 L 34 52 Z"
          fill={theme.color.primaryText}
          vectorEffect="non-scaling-stroke"
        />
      </svg>
    </div>
  );
}

/** One line into the next box: the agent becoming a conversation. */
function FlowArrow({ theme }: { theme: Theme }) {
  const id = "concepts-flow";
  return (
    <div aria-hidden css={{ display: "flex", alignItems: "center", flexShrink: 0 }}>
      {/* Wide enough to read as a connector and opaque enough to be seen: the first
          version faded in from a quarter opacity on a near-black page, which drew a
          lone arrowhead floating between two boxes. */}
      <svg width="44" height="12" viewBox="0 0 44 12" fill="none">
        <defs>
          {/* From a visible purple rather than from the border grey: ramping out of a
              near-invisible colour meant only the last quarter of the line could be
              seen, which read as a lone arrowhead floating between two boxes. */}
          {/*
            User space, not the object's bounding box.

            A gradient defaults to `objectBoundingBox`, and this path is a horizontal
            line — a box with zero height, which degenerates and paints nothing. The
            line was there the whole time and invisible, leaving an arrowhead floating
            between two boxes. The merge arrow above renders because its curves have
            height; this one has none to give.
          */}
          <linearGradient id={id} gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="34" y2="0">
            <stop offset="0%" stopColor={theme.color.primaryText} stopOpacity="0.45" />
            <stop offset="100%" stopColor={theme.color.primaryText} />
          </linearGradient>
        </defs>
        <path d="M0 6 L 34 6" stroke={`url(#${id})`} strokeWidth="1.5" />
        <path d="M30 2 L 36 6 L 30 10 Z" fill={theme.color.primaryText} />
      </svg>
    </div>
  );
}
