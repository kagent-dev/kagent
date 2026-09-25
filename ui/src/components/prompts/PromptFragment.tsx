import { Card, Typography } from "antd";
import { useTheme } from "@emotion/react";

const { Text, Paragraph } = Typography;

interface PromptFragmentProps {
  /** The fragment's key within its library. */
  fragmentKey: string;
  /** The library's name, needed to build the include tag. */
  library: string;
  text: string;
}

/**
 * One named fragment, with the tag that pulls it into an agent's instructions.
 *
 * The include tag is the reason a fragment exists, and it is derivable but
 * fiddly to type — so it is shown rather than described, and made copyable so
 * nobody has to retype it into a prompt.
 */
export function PromptFragment({ fragmentKey, library, text }: PromptFragmentProps) {
  const theme = useTheme();
  const include = `{{include "${library}/${fragmentKey}"}}`;

  return (
    <Card
      size="small"
      data-testid="prompt-fragment"
      title={
        <span css={{ fontFamily: theme.font.mono }} data-testid="prompt-fragment-key">
          {fragmentKey}
        </span>
      }
      extra={
        <Text
          copyable={{ text: include, tooltips: ["Copy include tag", "Copied"] }}
          data-testid="prompt-fragment-include"
          css={{ fontFamily: theme.font.mono, color: theme.color.textMuted }}
        >
          {include}
        </Text>
      }
    >
      {/* `pre-wrap` because a fragment is prompt text: its line breaks and
          indentation are part of what gets sent to the model. */}
      <Paragraph
        data-testid="prompt-fragment-text"
        css={{
          margin: 0,
          whiteSpace: "pre-wrap",
          fontFamily: theme.font.mono,
          color: theme.color.text,
        }}
      >
        {text}
      </Paragraph>
    </Card>
  );
}
