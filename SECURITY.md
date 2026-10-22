# Security policy

gonfig writes secrets from environment variables and files into config files, so a bug can expose them. Please report vulnerabilities privately, so they can be fixed before they are known publicly.

## Supported versions

Security fixes are released as a new version of the latest release only. Update to the [latest release](https://github.com/denglertai/gonfig/releases/latest) before reporting, if you can.

## Reporting a vulnerability

Report vulnerabilities with [GitHub's private vulnerability reporting](https://github.com/denglertai/gonfig/security/advisories/new). Please don't open a public issue or pull request for them.

Please include:

- The version of gonfig (`gonfig --version`) and how you installed it, e.g. the container image, the apk or a release archive
- The command, the file and the environment variables needed to reproduce the problem, with made-up secrets
- What happens and what you expected, e.g. which value is exposed and where

gonfig is maintained in spare time. You get an answer as soon as possible, and you are kept informed while the vulnerability is fixed. Once a fix is released, the advisory is published, crediting you unless you prefer otherwise.

## Scope

Examples of vulnerabilities:

- Values of environment variables or files appearing where they shouldn't, e.g. in logs, error messages or the diff of `config check --diff` without `--log-values`, or in output files other than the processed one
- Secrets written in plain text although a filter should have hashed or encrypted them
- Output files created with weaker permissions than documented, or written outside the intended path
- Problems with the signatures, SBOM attestations or build provenance of releases, or the release workflows producing them

Not vulnerabilities:

- Values shown after enabling `--log-values` (or `GONFIG_LOG_VALUES`), which is meant to show them for debugging
- Values passed on unchanged after enabling `--allow-unknown-filters`, as documented
- Anything requiring control of the environment variables, the templates or the `.gonfig.yaml` that gonfig is run with, since these define what is written
- Harm done by plugins you installed in the plugin directory, since they run with the permissions of gonfig. Plugins loaded although nobody installed them are in scope, though.
- Vulnerabilities in dependencies without an effect on gonfig. They are updated by Renovate, and alerts are tracked with Dependabot. govulncheck checks every change and, daily, the latest release for vulnerable code that gonfig calls, in dependencies and the Go standard library.

## Verifying releases

Container images are signed with cosign. Since v0.2.0, the container images also have signed SBOM attestations, and the images, the release archives and the apk have SLSA build provenance. See the [installation instructions](./README.md#installation) for how to verify them.
