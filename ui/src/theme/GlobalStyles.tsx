import { Global, css, useTheme } from "@emotion/react";
import { pillBorder } from "./theme";

export function GlobalStyles() {
  const theme = useTheme();
  const c = theme.color;
  // Status tags wear the theme's pill triples: antd's filled variant has no border and inks with the mid foreground.
  const statusTag = (status: string, bg: string, border: string, text: string) => `
    .ant-tag.ant-tag.ant-tag-${status}:not(.ant-tag-disabled):not(.ant-tag-solid) {
      background-color: ${bg};
      border: 1px solid ${pillBorder(border, bg)};
      color: ${text};
    }`;
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

        ${statusTag("success", c.successBg, c.successBorder, c.successText)}
        ${statusTag("processing", c.infoBg, c.infoBorder, c.infoText)}
        ${statusTag("warning", c.warningBg, c.warningBorder, c.warningText)}
        ${statusTag("error", c.dangerBg, c.dangerBorder, c.dangerText)}
        ${statusTag("default", c.bg, c.borderStrong, c.textMuted)}
        /* Pills are labels, not text to copy: a drag or double-click shouldn't highlight them. */
        .ant-tag {
          user-select: none;
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

        /* Pressed rows, menu items and options: the row tint at twice the hover's strength. */
        .ant-table-wrapper
          .ant-table-tbody
          .ant-table-row.clickable-table-row:active
          > .ant-table-cell,
        .ant-dropdown-menu-item:not(.ant-dropdown-menu-item-disabled):not(.ant-dropdown-menu-item-danger):active,
        .ant-select-item-option:not(.ant-select-item-option-disabled):not(.ant-select-item-option-selected):active {
          background: ${c.rowPressBg};
        }
        .ant-table-wrapper .ant-table-tbody .ant-table-row.clickable-table-row > .ant-table-cell,
        .ant-dropdown-menu-item,
        .ant-select-item-option {
          transition: none;
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

        /* Focus shows on the field's frame (antd's border and glow), never as a browser ring on the bare input inside it. */
        .ant-input-affix-wrapper input:focus,
        .ant-input-number input:focus,
        .ant-input-number-affix-wrapper input:focus,
        .ant-input-textarea-affix-wrapper textarea:focus,
        .ant-select input:focus,
        .ant-picker input:focus,
        .ant-input-outlined:focus,
        .ant-input-filled:focus {
          outline: none;
        }

        /* Tabs and segmented options: instant hover and press fills, drawn behind the label so nothing moves. */
        .ant-tabs:not(.ant-tabs-card) > .ant-tabs-nav .ant-tabs-tab {
          isolation: isolate;
        }
        .ant-tabs:not(.ant-tabs-card) > .ant-tabs-nav .ant-tabs-tab::before {
          content: "";
          position: absolute;
          inset: 6px 0;
          z-index: -1;
          border-radius: ${theme.radius.sm}px;
          pointer-events: none;
        }
        .ant-tabs:not(.ant-tabs-card) > .ant-tabs-nav .ant-tabs-tab:not(.ant-tabs-tab-disabled):hover::before {
          background: ${c.navHoverBg};
        }
        .ant-tabs:not(.ant-tabs-card) > .ant-tabs-nav .ant-tabs-tab:not(.ant-tabs-tab-disabled):active::before {
          background: ${c.navPressBg};
        }
        .ant-tabs > .ant-tabs-nav .ant-tabs-tab,
        .ant-tabs > .ant-tabs-nav .ant-tabs-tab-btn,
        .ant-segmented .ant-segmented-item,
        .ant-segmented .ant-segmented-item::after {
          transition: none;
        }

        /* Modals fade in place: antd's zoom grows them from the click point, so they slid across the page. */
        @keyframes kagentModalFadeIn {
          from {
            opacity: 0;
          }
          to {
            opacity: 1;
          }
        }
        @keyframes kagentModalFadeOut {
          from {
            opacity: 1;
          }
          to {
            opacity: 0;
          }
        }
        .ant-modal-root .ant-modal.ant-zoom-enter,
        .ant-modal-root .ant-modal.ant-zoom-appear,
        .ant-modal-root .ant-modal.ant-zoom-leave,
        .ant-modal-root .ant-fade-enter,
        .ant-modal-root .ant-fade-appear,
        .ant-modal-root .ant-fade-leave {
          animation-duration: 120ms;
          animation-timing-function: ease-out;
        }
        .ant-modal-root .ant-modal.ant-zoom-enter.ant-zoom-enter-active,
        .ant-modal-root .ant-modal.ant-zoom-appear.ant-zoom-appear-active {
          animation-name: kagentModalFadeIn;
        }
        .ant-modal-root .ant-modal.ant-zoom-leave.ant-zoom-leave-active {
          animation-name: kagentModalFadeOut;
        }
      `}
    />
  );
}
