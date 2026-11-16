---
worth: yes
where: src/components/QueryEditor.tsx
added: 2026-11-16
---
# query editor uses the deprecated @grafana/ui Select

Every dropdown in the query editor is a `Select` from `@grafana/ui`, which Grafana has deprecated in
favour of `Combobox`. `yarn lint` reports each one as a `@typescript-eslint/no-deprecated` warning; they
appeared with the create-plugin 9.0.3 update, which brought `@grafana/eslint-config` 10. Nothing is
broken for users today, but `Select` will be removed from `@grafana/ui` at some point.

`Combobox` is not a drop-in replacement: its option and `onChange` shapes differ, and it needs a newer
minimum Grafana than `Select` does, so check the `grafanaDependency` in `src/plugin.json` before
migrating.
