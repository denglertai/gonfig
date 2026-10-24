# Azure Key Vault plugin

`gonfig-plugin-azure-keyvault` provides the filter `azure_keyvault`, which reads secrets from [Azure Key Vault](https://learn.microsoft.com/azure/key-vault/).
See [PLUGINS.md](../../PLUGINS.md) for how plugins work.

- [Usage](#usage)
- [References](#references)
- [Authentication](#authentication)
- [Errors](#errors)
- [Installation](#installation)

## Usage

The variable of a placeholder holds the reference to the secret, and the filter returns its value:

`app.yaml`:
```yaml
database:
  host: db.internal
  password: ${DB_PASSWORD | azure_keyvault}
```

```sh
az keyvault secret set --vault-name myvault --name db-password --value s3cret
DB_PASSWORD=myvault/db-password gonfig config process -f app.yaml
```

gonfig writes `s3cret` as the password.

Key Vault secrets are single values. Secrets containing a JSON object provide fields, selected with `#`, and every secret is read once per run, also if several of its fields are used:

`db.yaml`:
```yaml
user: ${DB_USER | azure_keyvault}
password: ${DB_PASSWORD | azure_keyvault}
```

```sh
az keyvault secret set --vault-name myvault --name db --value '{"user": "app", "password": "s3cret"}'
DB_USER=myvault/db#user DB_PASSWORD=myvault/db#password gonfig config process -f db.yaml
```

A [placeholder template](../../FILTERS.md#templates) keeps the reference out of the config files, with a default that a variable may override:

```yaml
# .gonfig.yaml
templates:
  placeholders:
    db_password:
      env: DB_PASSWORD_REF
      default: "myvault/db-password"
      filters: "azure_keyvault"
```

## References

A reference names the vault, the secret and optionally its version, in one of these forms:

| Form | Example |
| --- | --- |
| `<vault>/<secret>[/<version>]` | `myvault/db-password`, `myvault/db-password/4b5c...` |
| `<vault host>/<secret>[/<version>]` | `myvault.vault.azure.cn/db-password` |
| The URL of the secret | `https://myvault.vault.azure.net/secrets/db-password/4b5c...` |

Without a version, the current version is read. The filter accepts no params.

Vault names are completed with `vault.azure.net`. `GONFIG_AZURE_KEYVAULT_SUFFIX` sets another DNS suffix for sovereign clouds, e.g. `vault.azure.cn` for Azure China or `vault.usgovcloudapi.net` for Azure Government. Hosts and URLs are used as they are.

`GONFIG_SECRET_TIMEOUT` sets the time a request may take including authentication, e.g. `10s`. It defaults to `20s` and has to be shorter than `30s`.

## Authentication

The plugin authenticates with [`DefaultAzureCredential`](https://learn.microsoft.com/azure/developer/go/sdk/authentication/credential-chains#defaultazurecredential-overview), which tries these credentials in order:

1. Environment variables: `AZURE_TENANT_ID` and `AZURE_CLIENT_ID` with `AZURE_CLIENT_SECRET` or `AZURE_CLIENT_CERTIFICATE_PATH`
1. Workload identity, e.g. in AKS with the variables set by the workload identity webhook
1. Managed identity of the Azure resource, e.g. a VM or a container app; `AZURE_CLIENT_ID` selects a user-assigned identity
1. The Azure CLI (`az login`), the Azure Developer CLI and Azure PowerShell, for local development

`AZURE_TOKEN_CREDENTIALS` restricts the chain, e.g. to `prod` credentials or a single credential like `ManagedIdentityCredential`.
The identity needs the permission to get secrets, e.g. the role *Key Vault Secrets User*.

The plugin authenticates on the first placeholder using it, so gonfig runs without Azure credentials as long as no placeholder uses the filter.
If no credential is available, every placeholder of the run fails with the same error, without trying the credentials again.

## Errors

Errors name the reference, and the status, error code and message of Key Vault or why the credentials failed, but never the secret:

```console
$ DB=myvault gonfig value '${DB | azure_keyvault}'
Error: filter azure_keyvault: failed to read the secret "myvault": invalid reference, use <vault>/<secret>[/<version>]
$ gonfig value '${DB | azure_keyvault}'
Error: filter azure_keyvault: the reference is empty, e.g. as its variable is not set
$ DB=myvault/db-password gonfig value '${DB | azure_keyvault}'
Error: filter azure_keyvault: failed to read the secret "myvault/db-password": DefaultAzureCredential: failed to acquire a token. Attempted credentials: EnvironmentCredential: missing environment variable AZURE_TENANT_ID; WorkloadIdentityCredential: no client ID specified. Check pod configuration or set ClientID in the options; ManagedIdentityCredential: managed identity timed out. See https://aka.ms/azsdk/go/identity/troubleshoot#dac for more information; AzureCLICredential: executable not found on path; AzureDeveloperCLICredential: executable not found on path; AzurePowerShellCredential: executable not found on path
```

Errors of Key Vault look like `status 404 SecretNotFound: ...` or `status 403 Forbidden: ...`, followed by the message of Key Vault, e.g. which permission is missing.

`gonfig config check` runs the filter as well, so it reads the secrets and reports these errors, while the [diff](../../README.md#showing-the-changes) masks the secrets.

## Installation

The plugin is released with gonfig, see [installation](../../README.md#plugins-for-secret-managers):

- the release archives `gonfig-plugin-azure-keyvault_<OS>_<arch>`, e.g. `gonfig-plugin-azure-keyvault_Linux_x86_64.tar.gz`
- the apk `gonfig-plugin-azure-keyvault`, which installs gonfig as well
- the container image `ghcr.io/denglertai/gonfig`, which contains the plugin

The packages and the image install the plugin into `/usr/libexec/gonfig/plugins`, which gonfig searches by default on Linux and macOS. Put binaries from the archives there as well. On Windows, gonfig searches `.\plugins` in the working directory by default instead. Another directory can be set with `--plugin-path` on every system.

To build the plugin from source, run within this directory:

```console
$ go build -o gonfig-plugin-azure-keyvault .
```

The unit tests use the fake server of the Azure SDK. A live test reads a secret of a real vault if `TEST_AZURE_KEYVAULT_REF` and `TEST_AZURE_KEYVAULT_VALUE` are set, with the Azure CLI credential after `az login`, or with `TEST_AZURE_TENANT_ID`, `TEST_AZURE_CLIENT_ID` and `TEST_AZURE_CLIENT_SECRET` of a service principal:

```console
$ az keyvault secret set --vault-name myvault --name gonfig-test --value hello
$ TEST_AZURE_KEYVAULT_REF=myvault/gonfig-test TEST_AZURE_KEYVAULT_VALUE=hello go test ./...
```
