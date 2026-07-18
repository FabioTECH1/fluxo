# Fluxo Dashboard UI

Vue 3, TypeScript, Vite, and Tailwind CSS v4 frontend embedded into the Fluxo Go binary.

## Development

```sh
npm install
npm run dev
```

Verify frontend changes with:

```sh
npx vue-tsc -b --noEmit
npm run build
```

## Shared components

Reusable controls live in `src/components/`. Prefer them over page-level copies so accessibility, spacing, interaction states, and dark mode remain consistent.

Common components include:

- `BaseModal` for dialogs.
- `Card` for page sections.
- `AppButton` for primary, secondary, and destructive actions.
- `DataTable` for responsive, horizontally scrollable tabular resources and sticky action slots.
- `TableActionMenu` for three-dot row actions that must escape table overflow.
- `StatusBadge` for compact state labels.
- `ToggleSwitch` for boolean settings and feature states.

### Boolean controls

Use `ToggleSwitch` whenever a value represents an enabled/disabled state:

```vue
<ToggleSwitch
  v-model="enabled"
  label="Push to deploy"
  description="Deploy when changes are pushed to the configured branch."
  label-position="left"
/>
```

For API-backed switches, pass the current state and handle the emitted value:

```vue
<ToggleSwitch
  :model-value="enabled"
  label="Scheduler"
  :disabled="updating"
  @update:model-value="updateScheduler"
/>
```

Use native checkboxes only when the user is acknowledging something or selecting multiple items from a set. Do not implement switch visuals directly in a view.

## Dark mode

Dark-mode behavior belongs in shared components. Views should consume the component rather than duplicate its light/dark classes.
