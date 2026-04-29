<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# Filters

## Purpose
Column-header filter components for the tables. The facet filter shows a virtualized, searchable popover with multi-select checkbox-style entries; the text filter is a free-form input that pushes its value into the table column on every keystroke.

## Key Files
| File | Description |
|------|-------------|
| `FacetFilter.tsx` | Popover-driven multi-select. Reads unique values via `column.getFacetedUniqueValues()`, virtualizes them, and exposes two `Entry` renderers — `ServerFacetEntry` (with online indicator) and `StandardFacetEntry`. |
| `TextFilter.tsx` | Single-line input wired to the column filter. Escape clears the value via `getHotkeyHandler`. |

## For AI Agents

### Working In This Directory
- `FacetFilter` accepts the entry component as an `Entry` prop so the same popover is reused across columns with different row decoration (server status vs. plain text). Add new entry types by writing another component matching `FacetEntryProps`.
- Filter values for facets are arrays of strings; the table-level `stringInArray` filter (in `BookTable.tsx`) checks `filterValue.includes(row.getValue(...))`. Keep value shape in sync when adding new facet columns.
- `TextFilter` runs `column.setFilterValue(filterValue)` on every change inside a `useEffect` — debouncing happens at the React-Table level, not here.

## Dependencies

### Internal
- `../../../state/api` (server-online lookup for `ServerFacetEntry`).

### External
- `@tanstack/react-table`, `@tanstack/react-virtual`, `@mantine/core`, `@mantine/hooks`, `phosphor-react`.

<!-- MANUAL: -->
