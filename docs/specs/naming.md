# Naming Conventions

- **Packages** follow the pattern `engine`, `pipeline`, `source/<driver>`, `sink/<driver>`, `transform/<name>`, `processor/<name>`.
- **Files** use `<component>_adapter.go`, `<component>_config.go`, `<component>_registry.go`, and `<driver_library>.go` where relevant.
- **Types** export interfaces named `Adapter`, `Registration`, `Config`. Concrete implementations use `<Driver>` suffix.
- **Configuration keys** exposed to YAML/JSON use `lower_snake_case`. Go struct fields use `UpperCamelCase` with struct tags mapping to snake case.
- **Docs** live under `docs/specs` and mirror this structure.

