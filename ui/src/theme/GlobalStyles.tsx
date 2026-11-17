import { Global, css, useTheme } from "@emotion/react";
import { dataPillLook } from "@/components/form/dataPillLook";

// antd's own "down" icon, as a mask so it takes the theme colour.
const CHEVRON_MASK = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='64 64 896 896'%3E%3Cpath d='M884 256h-75c-5.1 0-9.9 2.5-12.9 6.6L512 654.2 227.9 262.6c-3-4.1-7.8-6.6-12.9-6.6h-75c-6.5 0-10.3 7.4-6.5 12.7l352.6 486.1c12.8 17.6 39 17.6 51.7 0l352.6-486.1c3.9-5.3.1-12.7-6.4-12.7z'/%3E%3C/svg%3E") center / contain no-repeat`;

export function GlobalStyles() {
  const theme = useTheme();
  const pill = dataPillLook(theme);
  return (
    <Global
      styles={css`
        *,
        *::before,
        *::after {
          box-sizing: border-box;
        }
        html,
        body,
        #root {
          height: 100%;
          margin: 0;
        }
        body {
          background: ${theme.color.bg};
          color: ${theme.color.text};
          font-family: ${theme.font.body};
          -webkit-font-smoothing: antialiased;
        }
        a {
          color: inherit;
          text-decoration: none;
        }
        /*
         * antd's 1.57 line-height seats a label's glyphs below the icon's centre.
         * A button is a fixed-height flex row, so a one-em line box centres both.
         */
        .ant-btn {
          line-height: 1;
        }
        /* AutoComplete ships with no suffix, so it gets the same chevron as Select. */
        .ant-select-auto-complete::after {
          content: "";
          flex: none;
          align-self: center;
          width: 12px;
          height: 12px;
          margin-inline-start: 4px;
          background-color: ${theme.color.textMuted};
          mask: ${CHEVRON_MASK};
          -webkit-mask: ${CHEVRON_MASK};
          pointer-events: none;
        }
        /* A closed searchable select still reads as a dropdown; the text cursor appears once it opens. */
        .ant-select-single.ant-select-show-search:not(.ant-select-auto-complete):not(.ant-select-open):not(.ant-select-disabled),
        .ant-select-single.ant-select-show-search:not(.ant-select-auto-complete):not(.ant-select-open):not(.ant-select-disabled) input {
          cursor: pointer;
        }
        /* Tag and multiple-mode values render as data pills. */
        .ant-select-multiple .ant-select-selection-item {
          border-radius: ${pill.borderRadius}px;
          font-family: ${pill.fontFamily};
          font-size: ${pill.fontSize}px;
        }
        .ant-select-multiple .ant-select-selection-item-remove {
          border-radius: 2px;
          color: ${theme.color.textMuted};
        }
        .ant-select-multiple .ant-select-selection-item-remove:hover {
          background: ${theme.color.text}29;
          color: ${theme.color.text};
        }
        .ant-select-multiple .ant-select-selection-item-remove:active {
          background: ${theme.color.text}3d;
          color: ${theme.color.text};
        }
        /*
         * The surfaces, given a very shallow gradient.
         *
         * Here rather than on each component because these are the component
         * library's own elements — a card's background is drawn by antd, and antd
         * takes a colour rather than a gradient for it. Three or four steps of
         * lightness is the whole effect: enough that a panel reads as sitting on
         * the page rather than cut out of it, and not enough to notice as an
         * effect.
         */
        .ant-card,
        .ant-layout-header {
          background: ${theme.color.surfaceCard};
        }
        .ant-layout-header {
          background: ${theme.color.surfaceNav};
        }

        /*
         * Table rows are not assumed interactive. Hover treatment and pointer cursor
         * are opt-in via the clickable-table-row class so static data tables do not imply
         * a click target.
         *
         * Matched on the cell class antd paints the hover from rather than on a hovered
         * row's cells, which is what this rule used to say. A virtual table has no table
         * markup at all — its body is divs — so the old tr/td selector could not reach
         * one, and the substrate page's actors and workers went on lighting up under the
         * pointer while every other static table had stopped. The class is on the cells
         * of both bodies, so one rule covers both.
         *
         * Selected rows need no exception of their own. The one table in the app with
         * row selection is the conversations list, where a row is selectable exactly
         * when it is openable — an unopenable row gets a disabled checkbox — so every
         * row that can be selected already carries clickable-table-row, and this rule
         * has passed it by before selection is reached.
         */
        .ant-table-wrapper
          .ant-table-tbody
          .ant-table-row:not(.clickable-table-row)
          > .ant-table-cell-row-hover {
          background: inherit;
        }

        /* Written against the cell class for the same reason as the rule above: a
           virtual body has no table cell for a tr/td selector to find. */
        .ant-table-wrapper
          .ant-table-tbody
          .ant-table-row.clickable-table-row
          > .ant-table-cell {
          cursor: pointer;
        }

        /*
         * Pressed, and visibly different from hovered.
         *
         * This rule existed and did nothing: it set the border token, which is the same colour
         * antd already uses for the row hover — measured at rgb(50, 44, 61) for both, so
         * pressing a row looked exactly like pointing at it and a click on a slow route
         * still looked like it had not registered. The whole point of the rule was to
         * distinguish the two.
         *
         * Tinted with the brand colour rather than made a lighter grey: the row is
         * already grey when hovered, and another grey a few percent lighter is a
         * difference nobody notices under a moving finger.
         *
         * At 40% rather than the 30% it carried: the stronger row hover it now has to
         * beat left the two within 1.12:1 of each other on the light theme.
         */
        .ant-table-wrapper
          .ant-table-tbody
          .ant-table-row.clickable-table-row:active
          > .ant-table-cell {
          background: ${theme.color.primary}66;
        }

        /*
         * The expand control is part of its row, not a target of its own.
         *
         * Where the whole row expands, this button did the same thing as the row while
         * hovering and pressing differently from it — two overlapping affordances for one
         * action, and the smaller one won on the mouse. It keeps its shape and its
         * plus/minus, and takes the row's states.
         */
        /* Class-based for the reason the hover rules above are: a virtual body has no
           tr for this to match, so keying off the row class covers both bodies. */
        .ant-table-wrapper
          .ant-table-row.clickable-table-row
          .ant-table-row-expand-icon {
          cursor: pointer;
          transition: none;
          /* Transparent at rest, not only on hover: the control ships with a white fill,
             so suppressing the fill on hover alone swapped one difference for another —
             it read as a small white square that vanished under the mouse. Letting the
             row's own background show through is what makes it part of the row. */
          background: transparent;
          color: inherit;
          border-color: ${theme.color.border};
          box-shadow: none;
        }
        .ant-table-wrapper
          tr.clickable-table-row
          .ant-table-row-expand-icon:hover,
        .ant-table-wrapper
          tr.clickable-table-row
          .ant-table-row-expand-icon:active,
        .ant-table-wrapper
          tr.clickable-table-row
          .ant-table-row-expand-icon:focus,
        .ant-table-wrapper
          tr.clickable-table-row
          .ant-table-row-expand-icon:focus-visible {
          color: inherit;
          border-color: ${theme.color.border};
          background: transparent;
          box-shadow: none;
          outline: none;
        }
      `}
    />
  );
}
