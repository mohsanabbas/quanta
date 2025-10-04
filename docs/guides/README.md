# Quanta User Guides
This directory contains practical guides for using and tuning Quanta.
## Available Guides
### [TUNING_GUIDE.md](TUNING_GUIDE.md)
Comprehensive guide to performance tuning and optimization:
- Understanding tuning parameters
- Pre-configured scenarios (auto, e2e, safe mode)
- Memory calculations and formulas
- Troubleshooting common issues
- Monitoring and metrics
### [BUGFIXES.md](BUGFIXES.md)
Documentation of recent bug fixes and improvements:
- Critical bugs fixed (semaphore panic, double release, stuck partitions)
- Before/after code examples
- Testing checklist
- Verification steps
### [TUNING_LOADING_FLOW.md](TUNING_LOADING_FLOW.md)
Deep dive into configuration loading:
- Complete flow from engine startup to driver configuration
- Where and how tuning files are loaded
- Path derivation logic
- Environment variable overrides
- Visual flow diagrams
## Quick Links
- [Configuration Reference](../../CONFIGS.md) - Complete YAML schema reference
- [Configuration Spec](../specs/configuration.md) - Configuration specification
- [Source Spec](../specs/source.md) - Source driver specification
