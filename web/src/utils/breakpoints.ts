// Script-side copies of the CSS responsive bands (see the comment above `*` in
// style.css). Media-query conditions cannot read CSS custom properties, so the
// stylesheets keep literal values: change a band here and in its @media rules together.

// Phone drilldown is (max-width: 640px); above it the dialer opens without a modal backdrop.
export const NON_MODAL_DIALER_QUERY = '(min-width: 641px)'

// Compact workspace, matching @media (max-width: 1100px): swipe actions on list rows.
export const COMPACT_WORKSPACE_QUERY = '(max-width: 1100px)'

// The other side of the compact-workspace boundary: Settings shows list and detail together.
export const SIDE_BY_SIDE_SETTINGS_QUERY = '(min-width: 1101px)'

// Persistent dialer, matching @media (min-width: 1480px).
export const PERSISTENT_DIALER_QUERY = '(min-width: 1480px)'
