import type { ReactNode } from "react";
import { Select, Tag } from "antd";
import { useTheme } from "@emotion/react";
import { X } from "lucide-react";
import { SearchInput } from "./SearchInput";
import type { ListView } from "./useListView";

/**
 * The controls above a list, and the row of pills that says what they are doing.
 *
 * Built from filter *definitions* rather than from a namespace list, because the
 * namespace filter is only the first one. The same bar carries a template filter, a
 * state filter and a creator filter on other surfaces, and a component that knew what
 * a namespace was would be reusable on the pages that filter by namespace and useless
 * on the one that does not.
 *
 * Three behaviours are the point of it, and each replaces something the pages were
 * doing worse:
 *
 * - **Selecting nothing means everything.** A separate "all namespaces" toggle beside
 *   a single-select was two controls answering one question, and they could disagree —
 *   a namespace chosen and the toggle on left the reader unsure which won. A
 *   multi-select with an empty value has one state and it reads plainly.
 * - **Every active filter is a pill, on its own row.** A `Select` showing "2 selected"
 *   hides *which* two behind a click. The pills are the answer to "why am I not seeing
 *   the thing I am looking for", which is the question a filtered list most often
 *   provokes.
 * - **Clicking a pill removes exactly that one.** Undoing one choice out of several
 *   otherwise means reopening the control and hunting for the row to untick.
 *
 * The search term is a pill too. It is a filter like any other — and it is the one
 * most often forgotten about, left in the box from ten minutes ago, quietly hiding
 * rows. Making it visible in the same row is also what lets "clear filters" mean it.
 */

/** One choice in a filter. */
export interface FilterOption {
  value: string;
  /** What the reader sees, where that is not the value itself. */
  label?: string;
}

export interface FilterDefinition {
  /**
   * Stable id. It is also the URL parameter this filter is remembered in, so it
   * should read well in an address: `ns`, `state`, `creator`.
   */
  id: string;
  /** The filter's name, used on its pills: "Namespace: kagent". */
  label: string;
  /** What selecting nothing means, shown in the control itself: "All namespaces". */
  allLabel: string;
  options: readonly FilterOption[];
  /** Widened where the values are long. */
  minWidth?: number;
}

export interface FilterBarProps {
  filters: readonly FilterDefinition[];
  view: ListView;
  /** Omitted where a list has nothing worth searching. */
  search?: {
    placeholder: string;
    /** For screen readers, since the placeholder disappears once typing starts. */
    label: string;
  };
  /** The end of the control row — the "3 of 12" summary, and where it comes from. */
  trailing?: ReactNode;
  /** Prefix for this bar's test ids, so two bars on one page stay distinguishable. */
  testId: string;
}

/** A filter value as the reader chose it, rather than as the URL carries it. */
function labelFor(filter: FilterDefinition, value: string): string {
  return filter.options.find((option) => option.value === value)?.label ?? value;
}

export function FilterBar({
  filters,
  view,
  search,
  trailing,
  testId,
}: FilterBarProps) {
  const theme = useTheme();

  const pills = filters.flatMap((filter) =>
    view.selected(filter.id).map((value) => ({
      key: `${filter.id}:${value}`,
      testId: `${testId}-pill-${filter.id}-${value}`,
      text: `${filter.label}: ${labelFor(filter, value)}`,
      remove: () => view.deselect(filter.id, value),
    })),
  );

  if (search && view.query !== "") {
    pills.unshift({
      key: "query",
      testId: `${testId}-pill-search`,
      text: `Search: ${view.query}`,
      remove: () => view.setQuery(""),
    });
  }

  return (
    <div
      data-testid={testId}
      css={{ display: "flex", flexDirection: "column", gap: theme.space(2) }}
    >
      <div
        css={{
          display: "flex",
          alignItems: "center",
          gap: theme.space(4),
          flexWrap: "wrap",
        }}
      >
        {search ? (
          <SearchInput
            testId={`${testId}-search`}
            label={search.label}
            placeholder={search.placeholder}
            value={view.query}
            onChange={view.setQuery}
          />
        ) : null}

        {filters.map((filter) => (
          // The id goes on a wrapper this app owns rather than on the `Select`:
          // antd spreads unknown props down to its own inner elements, so an id
          // handed to the component does not reliably land on a box a test can
          // point at.
          <div key={filter.id} data-testid={`${testId}-filter-${filter.id}`}>
            <Select
              mode="multiple"
              value={[...view.selected(filter.id)]}
              onChange={(next: string[]) => view.setSelected(filter.id, next)}
              options={filter.options.map((option) => ({
                value: option.value,
                label: option.label ?? option.value,
              }))}
              placeholder={filter.allLabel}
              aria-label={`Filter by ${filter.label.toLocaleLowerCase()}`}
              // The chosen values are on the pills below, so repeating them inside
              // the trigger would say the same thing twice and grow the control
              // until it pushed the rest of the row onto another line.
              maxTagCount={0}
              maxTagPlaceholder={(hidden) => `${hidden.length} selected`}
              // An option can be longer than the trigger; antd sizes the popup to
              // the trigger unless told otherwise, and clips the rest.
              popupMatchSelectWidth={false}
              optionFilterProp="label"
              // Nothing to choose between: the list is empty, or the read failed.
              disabled={filter.options.length === 0}
              css={{ minWidth: filter.minWidth ?? 220 }}
            />
          </div>
        ))}

        {trailing}
      </div>

      {pills.length > 0 ? (
        <div
          data-testid={`${testId}-pills`}
          css={{
            display: "flex",
            alignItems: "center",
            gap: theme.space(2),
            flexWrap: "wrap",
          }}
        >
          {pills.map((pill) => (
            <Tag
              key={pill.key}
              data-testid={pill.testId}
              closable
              onClose={(event) => {
                // antd's own close would also unmount the tag, which fights the
                // re-render this removal causes.
                event.preventDefault();
                pill.remove();
              }}
              // The whole pill removes, not only the cross. The cross is a small
              // target and the pill is plainly one thing; removal is idempotent, so
              // a click that lands on both does no harm.
              onClick={pill.remove}
              css={{
                cursor: "pointer",
                marginInlineEnd: 0,
                background: theme.color.infoBg,
                borderColor: theme.color.infoBorder,
                color: theme.color.infoText,
              }}
            >
              {pill.text}
            </Tag>
          ))}

          <Tag
            data-testid={`${testId}-pill-clear`}
            onClick={view.clear}
            icon={<X size={11} aria-hidden css={{ verticalAlign: "-1px" }} />}
            css={{
              cursor: "pointer",
              marginInlineEnd: 0,
              // Deliberately not one of the filter pills' colours: it is the row's
              // one control that does something to all of them, and a reader
              // scanning for "how do I get back" should not have to read every pill
              // to find it.
              background: "transparent",
              borderStyle: "dashed",
              // `borderStrong` rather than `border`: this is a control's edge, which
              // wants 3:1, and the surface-separating token measures 1.35:1 against
              // the page — a dashed outline in it reads as a smudge.
              borderColor: theme.color.borderStrong,
              color: theme.color.textMuted,
            }}
          >
            Clear filters
          </Tag>
        </div>
      ) : null}
    </div>
  );
}
