import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { ThemeProvider } from "next-themes";
import { ThemeToggle } from "./ThemeToggle";

const meta: Meta<typeof ThemeToggle> = {
  title: "Layout/ThemeToggle",
  component: ThemeToggle,
  decorators: [
    (Story) => (
      <ThemeProvider
        attribute="class"
        defaultTheme="system"
        enableSystem
        disableTransitionOnChange
        storageKey="storybook-theme"
      >
        <div className="flex items-center gap-4 p-8">
          <Story />
          <span className="text-sm text-muted-foreground">
            One click advances system → light → dark → system.
          </span>
        </div>
      </ThemeProvider>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof ThemeToggle>;

export const Default: Story = {};

/**
 * Walks the full cycle and returns to "system", which the previous
 * light/dark-only control could not reach.
 */
export const CyclesBackToSystem: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);

    const button = await canvas.findByRole("button", { name: "Theme: system" });
    await userEvent.click(button);
    await expect(
      canvas.getByRole("button", { name: "Theme: light" })
    ).toBeInTheDocument();

    await userEvent.click(canvas.getByRole("button", { name: "Theme: light" }));
    await expect(
      canvas.getByRole("button", { name: "Theme: dark" })
    ).toBeInTheDocument();

    await userEvent.click(canvas.getByRole("button", { name: "Theme: dark" }));
    await expect(
      canvas.getByRole("button", { name: "Theme: system" })
    ).toBeInTheDocument();
  },
};
