import { Global, css, useTheme } from "@emotion/react";

export function GlobalStyles() {
  const theme = useTheme();
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
         */
        .ant-table-wrapper
          .ant-table-tbody
          > tr.ant-table-row:not(.clickable-table-row):hover
          > td {
          background: inherit;
        }

        .ant-table-wrapper
          .ant-table-tbody
          > tr.ant-table-row.clickable-table-row
          > td {
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
         */
        .ant-table-wrapper
          .ant-table-tbody
          > tr.ant-table-row.clickable-table-row:active
          > td {
          background: ${theme.color.primary}4D;
        }

        /*
         * The expand control is part of its row, not a target of its own.
         *
         * Where the whole row expands, this button did the same thing as the row while
         * hovering and pressing differently from it — two overlapping affordances for one
         * action, and the smaller one won on the mouse. It keeps its shape and its
         * plus/minus, and takes the row's states.
         */
        .ant-table-wrapper
          tr.clickable-table-row
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
