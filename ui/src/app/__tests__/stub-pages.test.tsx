import { render, screen } from "@testing-library/react";

import FeedPage from "../feed/page";
import CronJobsPage from "../cronjobs/page";
import OrganizationPage from "../admin/org/page";
import GatewaysPage from "../admin/gateways/page";

const STUB_PAGES = [
  { name: "FeedPage", Component: FeedPage, title: "Live Feed" },
  { name: "CronJobsPage", Component: CronJobsPage, title: "Cron Jobs" },
  { name: "OrganizationPage", Component: OrganizationPage, title: "Organization" },
  { name: "GatewaysPage", Component: GatewaysPage, title: "Gateways" },
];

describe("Placeholder stub pages (Step 8)", () => {
  it.each(STUB_PAGES)("$name renders title '$title'", ({ Component, title }) => {
    render(<Component />);
    expect(screen.getByText(title)).toBeInTheDocument();
  });

  it.each(STUB_PAGES)("$name renders 'Coming soon' text", ({ Component }) => {
    render(<Component />);
    expect(screen.getByText("Coming soon")).toBeInTheDocument();
  });

  it.each(STUB_PAGES)("$name has centered layout with min-height", ({ Component }) => {
    const { container } = render(<Component />);
    const wrapper = container.firstElementChild as HTMLElement;
    expect(wrapper.className).toContain("flex");
    expect(wrapper.className).toContain("items-center");
    expect(wrapper.className).toContain("justify-center");
    expect(wrapper.className).toContain("min-h-[400px]");
  });

  it.each(STUB_PAGES)("$name renders an icon (svg element)", ({ Component }) => {
    const { container } = render(<Component />);
    const svg = container.querySelector("svg");
    expect(svg).toBeInTheDocument();
    expect(svg?.classList.contains("h-12")).toBe(true);
    expect(svg?.classList.contains("w-12")).toBe(true);
  });
});
