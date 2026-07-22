# Repository guidance

- Keep credentials and other secrets out of source control, logs, examples, and tests.
- Keep platform bootstrap changes independent from business-domain behavior.
- Use feature branches and pull requests targeting `develop`; promote `develop` to `main` separately.
- Do not run local container builds in constrained shared environments. Prefer focused formatting, unit tests, and static analysis.
- Document new runtime configuration and deployment assumptions before relying on them.
