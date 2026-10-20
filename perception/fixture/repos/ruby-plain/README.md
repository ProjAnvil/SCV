# ruby-plain

A deliberately unsupported-language fixture: plain Ruby sources, no build
markers, nothing the v3 structure tier recognizes. Perception must skip it
cleanly in every config mode (status not-framework, exit 0, no
structure.json / structure.vg) — the negative half of the detect-gate
fixture matrix (spec 2026-10-20 §7 / §11.6).
