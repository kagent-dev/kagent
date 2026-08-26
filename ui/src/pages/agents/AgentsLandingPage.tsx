import { Button, Space, Tabs } from "antd";
import { useTheme } from "@emotion/react";
import { Plus } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { VendorSlot } from "@/vendorExtensions";
import { paths } from "@/router/routes";
import { PageFrame } from "@/components/Structure/PageFrame";
import { AgentsTab } from "@/pages/AgentsPage";
import { AgentTemplatesTab } from "@/pages/AgentTemplatesPage";
import { AgentConcepts } from "./AgentConcepts";
import { HarnessesTab } from "./HarnessesTab";

const TABS = ["agents", "templates", "harnesses"] as const;

type TabKey = (typeof TABS)[number];

/**
 * Agents, templates and harnesses on one surface.
 *
 * They were three separate destinations — agents and templates each had their own page
 * and their own sidebar entry, and harnesses had nowhere at all — which spread one idea
 * across the navigation and left the relationship between the parts implicit. A reader
 * looking at a list of templates had no way to see which harness would run them, and a
 * reader looking for "New agent" was looking for something that does not exist.
 *
 * The tab is in the URL rather than in state, so a tab can be linked to, survives a
 * reload, and goes back the way it came.
 */
export function AgentsLandingPage() {
  const theme = useTheme();
  const [params, setParams] = useSearchParams();

  /*
   * No page-level refresh, deliberately.
   *
   * There was one, and moving it into each tab's filter row is what removed the need:
   * a control sitting beside the filters that narrow *this* list should re-read this
   * list, and one that quietly refreshed two other tabs would be doing more than it
   * appears to. Each tab owns its own, and says which one it refreshed.
   */
  const requested = params.get("tab");
  // A tab this build does not have falls back rather than rendering nothing: an old
  // link, or a typed address, should land somewhere useful.
  const active: TabKey = TABS.includes(requested as TabKey)
    ? (requested as TabKey)
    : "agents";

  return (
    <PageFrame
      title="Agents"
      /* No blurb: the cards below say the same thing and say it better, and a
         description repeating them costs a line above a table people came to read. */
      actions={
        /*
         * One set of controls for the page, not one per tab.
         *
         * Each tab had its own refresh and the templates tab its own create, which put
         * three buttons in three places doing two things — and a reader on the agents
         * tab had a refresh that pointedly did not refresh the tab they were about to
         * click. These act on the surface, which is what the reader thinks they are
         * looking at.
         */
        <Space size={8}>
          {/* The point the agents list has always offered, kept where the controls
              now are rather than left behind in the tab they moved out of. */}
          <VendorSlot id="app_agents_agentsList_pageHeader_actions" />
          <Link to={paths.agentTemplateNew}>
            <Button type="primary" icon={<Plus size={14} />} data-testid="agents-new-template">
              New template
            </Button>
          </Link>
          <Link to={paths.harnessNew}>
            <Button type="primary" icon={<Plus size={14} />} data-testid="agents-new-harness">
              New harness
            </Button>
          </Link>
        </Space>
      }
    >
      {/* Closer to the heading than the page's default gap: the overview belongs to
          the title above it, and the full gap left it floating between the two. */}
      <div css={{ marginTop: `-${theme.space(3)}` }}>
        <AgentConcepts />
      </div>


      <Tabs
        activeKey={active}
        data-testid="agents-tabs"
        onChange={(key) => {
          // Replace rather than push: flipping between tabs is looking around one
          // surface, and filling the back stack with it means Back stops being the way
          // out of the page.
          setParams((current) => {
            const next = new URLSearchParams(current);
            next.set("tab", key);
            return next;
          }, { replace: true });
        }}
        items={[
          {
            key: "agents",
            label: "Agents",
            children: <AgentsTab />,
          },
          {
            key: "templates",
            label: "Templates",
            children: <AgentTemplatesTab />,
          },
          {
            key: "harnesses",
            label: "Harnesses",
            children: <HarnessesTab />,
          },
        ]}
      />
    </PageFrame>
  );
}
