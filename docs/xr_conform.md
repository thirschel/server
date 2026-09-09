# `xr conform`

`xr conform` runs the built-in xRegistry Core conformance checks against one or
more registry URLs. The current catalog targets the exact
`xregistry-core-1.0-rc4` profile and uses the normative
[`xregistry/spec`](https://github.com/xregistry/spec) content at commit
[`d2433a8c726ab096303bd943a4fc6691925f7910`](https://github.com/xregistry/spec/tree/d2433a8c726ab096303bd943a4fc6691925f7910).
The command does not fetch specification content at runtime.

## Catalog

The initial catalog preserves the existing six logical checks and their Go
display names:

| Stable ID | Display name | Direct dependencies | Mode |
|---|---|---|---|
| `core.registry-access` | `TestSniff` | none | `read-only` |
| `core.model` | `TestModel` | `core.registry-access` | `read-only` |
| `core.capabilities` | `TestCapabilities` | `core.registry-access` | `read-only` |
| `core.registry-root` | `TestRegistryRoot` | `core.model`, `core.capabilities` | `read-only` |
| `core.groups` | `TestGroups` | `core.registry-root` | `read-only` |
| `core.resources` | `TestResources` | `core.groups` | `read-only` |

Stable IDs are lowercase, dot-separated semantic names. Selection is exact and
case-sensitive; IDs are not prefixes. IDs are never repurposed. Any future
rename requires an alias and a deprecation period approved as a public contract
change.

Run the offline catalog listing to see descriptions, profiles, dependencies,
and immutable specification references:

```console
xr conform --list-tests
```

`--list-tests` does not contact a registry. It cannot be combined with target
URLs, `--server`, `--test`, `--allow-mutations`, hidden `--run`, or
execution-formatting flags.

## Selecting tests

Without `--test`, all catalog cases run in catalog order:

```console
xr conform https://registry.example.com
```

Use repeatable `--test` flags to select stable IDs:

```console
xr conform \
  --test core.model \
  --test core.capabilities \
  https://registry.example.com
```

Requested cases include their complete transitive dependency closure.
Duplicates are removed, dependencies execute once per target, and execution
always follows catalog order rather than command-line order. Unknown or
incorrectly cased IDs fail before any registry request and report the valid
IDs.

The hidden `--run` option remains available for TD framework self-tests. It is
mutually exclusive with `--list-tests`, `--test`, and `--allow-mutations`.

## Exact profile

The first registry request must return a JSON root document whose
`specversion` value is exactly the string `1.0-rc4`. A missing, null,
non-string, or different value fails `core.registry-access`. Dependent cases
then stop without sending their own requests.

The catalog does not claim conformance for other xRegistry versions, and the
current cases do not provide exhaustive certification coverage.

## Mutation safety

Every catalog case declares either `read-only` or `mutation` mode. Read-only
cases may send only `GET`, `HEAD`, and `OPTIONS`; the client blocks other
methods before they reach the HTTP transport.

Mutation cases require explicit permission:

```console
xr conform --allow-mutations --test <mutation-case-id> URL
```

Selecting a mutation case or mutation dependency without
`--allow-mutations` fails before any request. Permission does not allow a
read-only case to send an unsafe method. The initial six cases are all
read-only.

## Output and exit status

Conformance output is currently text-only. For a supported rc4 target, the
default output, tree shape, and legacy TD-entry counts remain compatible with
the original runner.

Command and usage errors exit with status `1`. Each failed target contributes
the existing TD failure status `2`; successful targets, skipped targets, and
warnings under the default warning policy contribute `0`. Multi-target runs
retain the existing behavior of summing the per-target statuses.
