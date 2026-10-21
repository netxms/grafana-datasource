---
worth: yes
where: pkg/plugin/datasource.go:668
added: 2026-10-21
---
# DCI value field passes the unit as a label instead of Config.Unit

`dciFrame` builds the numeric value field with `data.NewField("value", map[string]string{"unit": dciData.UnitName}, …)`. The second argument is the field's labels, so the series shows up as `value{unit="%"}` in legends and tooltips, and Grafana never applies unit formatting. Setting `Config: &data.FieldConfig{Unit: …}` would fix that. The catch: NetXMS unit names (`%`, `ms`, `B`, free text) aren't Grafana unit IDs, so we need to decide between passing them through as custom units and mapping the common ones. Found while working on #24 and left out of that change.
