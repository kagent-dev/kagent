import { Button, Tabs } from "antd";
import { useTheme } from "@emotion/react";
import { Plus } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { egressCopy } from "@/components/egress-policy-form/egressPolicyCopy";
import { paths } from "@/router/routes";
import { EgressPoliciesTab } from "./EgressPoliciesTab";
import { PolicyConcepts } from "./PolicyConcepts";

// region Helpers

const TABS = ["egress"] as const;

type TabKey = (typeof TABS)[number];

// region Component

/** Every policy kind on one page, one tab each. The tab lives in the URL, as on the agents page. */
export function PoliciesPage() {
  const theme = useTheme();
  const [params, setParams] = useSearchParams();
  const requested = params.get("tab");
  const active: TabKey = TABS.includes(requested as TabKey) ? (requested as TabKey) : "egress";

  return (
    <PageFrame
      title={egressCopy.page.title}
      actions={
        <Link to={paths.egressPolicyNew}>
          <Button type="primary" icon={<Plus size={14} />} data-testid="policies-new-egress">
            {egressCopy.page.createButton}
          </Button>
        </Link>
      }
    >
      <div css={{ marginTop: `-${theme.space(3)}` }}>
        <PolicyConcepts />
      </div>

      <Tabs
        activeKey={active}
        data-testid="policies-tabs"
        onChange={(key) =>
          setParams(
            (current) => {
              const next = new URLSearchParams(current);
              next.set("tab", key);
              return next;
            },
            { replace: true },
          )
        }
        items={[{ key: "egress", label: egressCopy.page.tab, children: <EgressPoliciesTab /> }]}
      />
    </PageFrame>
  );
}
