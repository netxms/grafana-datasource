---
worth: yes
where: pkg/plugin/datasource.go:242
added: 2026-10-21
---
# alarm Severity mapping duplicates the object status colour table

The alarm `Severity` value mapping spells out the same nine names and colours, in the same order, as `objectStatusColors` (`pkg/plugin/datasource.go:939`). Changing one colour means editing both, and nothing catches the two drifting apart: `TestObjectStatusColor` and the alarm test's `assertValueMapping` each pin their own copy. Fix: hold name/colour pairs in one table, build the Severity `ValueMapper` from it and index it in `objectStatusColor`. Surfaced by the revmux review of #27 (round issue-27/01-initial); outside that issue's scope.
