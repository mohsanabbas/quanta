# Naming Conventions

- **Packages**: `engine`, `pipeline`, `source/<driver>`, `sink/<driver>`, `processor/<name>`.
- **Files**: use suffices `_adapter.go`, `_config.go`, `_registry.go` to denote role. Driver-specific files follow `<driver>_*.go`.
- **Types**: exported interfaces named `Adapter`, `Registration`, `Config`. Concrete types use `<Driver>` suffix.
- **Config Keys**: YAML/JSON use `lower_snake_case`. Struct fields use UpperCamelCase with `json` tags mapping to snake case.
- **Docs**: live under `docs/specs` with one concept per file.

