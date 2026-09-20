# buf-plugin-aip

A [Buf](https://buf.build) lint plugin for [Google AIP (API Improvement Proposals)](https://aip.dev) powered by [`github.com/googleapis/api-linter/v2`](https://github.com/googleapis/api-linter).

## Features

- **Standard AIP Rules:** Directly wraps Google's official `api-linter` (v2), covering all 340+ core rules and client library guidelines.
- **High Performance:** Lints all non-imported protobuf ASTs in a single pass (`O(1)` per rule dispatch), avoiding the $O(N \times \text{rules})$ overhead seen in older ports.
- **Native Protoreflect:** Built on Go's official `google.golang.org/protobuf/reflect/protoreflect` and Buf's `bufplugin` SDK (no legacy `jhump/protoreflect` conversions).
- **Flexible Categorization:**
  - `AIP` – All AIP rules.
  - `AIP_CORE` – Core AIP rules (enabled by default).
  - `AIP_CLIENT_LIBRARIES` – Client library rules (AIP-42xx).
  - `AIP_<NNNN>` – Per-AIP rule groups (e.g. `AIP_0131` for AIP-131 rules).
- **Full Buf Integration:** Works seamlessly with `use`, `except`, `ignore`, and `// buf:lint:ignore` comments, as well as native `api-linter` configuration files.

---

## Installation

### From Source
```bash
go install github.com/hestiatechnology/buf-plugin-aip@latest
```
Or clone and build locally:
```bash
git clone https://github.com/hestiatechnology/buf-plugin-aip.git
cd buf-plugin-aip
go build -o buf-plugin-aip .
# Ensure the binary is on your $PATH
export PATH="$PWD:$PATH"
```

---

## Usage

In your `buf.yaml` (v2 format):

```yaml
version: v2
modules:
  - path: .
lint:
  use:
    - AIP_CORE
plugins:
  - plugin: buf-plugin-aip
```

Run Buf lint:

```bash
buf lint
```

---

## Configuration Examples

### 1. Enabling All AIP Rules
```yaml
version: v2
lint:
  use:
    - AIP
plugins:
  - plugin: buf-plugin-aip
```

### 2. Targeting a Specific AIP (e.g. AIP-131)
```yaml
version: v2
lint:
  use:
    - AIP_0131
plugins:
  - plugin: buf-plugin-aip
```

### 3. Ignoring Specific Rules
You can exclude rules globally in `buf.yaml`:
```yaml
version: v2
lint:
  use:
    - AIP_CORE
  except:
    - AIP_0191_JAVA_PACKAGE
    - AIP_0191_JAVA_MULTIPLE_FILES
plugins:
  - plugin: buf-plugin-aip
```

Or ignore in protobuf comments directly:
```protobuf
service LibraryService {
  // buf:lint:ignore AIP_0131_METHOD_SIGNATURE
  rpc GetBook(GetBookRequest) returns (Book);
}
```

### 4. Using an `api-linter.yaml` Config File
You can also supply an existing `api-linter` configuration file via plugin options:

```yaml
version: v2
lint:
  use:
    - AIP_CORE
plugins:
  - plugin: buf-plugin-aip
    options:
      config: ./api-linter.yaml
```

Where `api-linter.yaml`:
```yaml
- disabled_rules:
    - core::0131::method-signature
```

---

## Auto-Fix CLI (`buf-plugin-aip fix`)

`buf-plugin-aip` includes an integrated auto-fixer capable of automatically resolving machine-fixable AIP violations in `.proto` files (e.g. timestamp field suffixes `_at` $\rightarrow$ `_time`, RPC request message naming, enum formatting, forbidden types).

### Dry-run & Diff
Inspect suggested fixes without modifying files:
```bash
# Preview fixes with unified diff
buf-plugin-aip fix --dry-run --diff ./proto
```

### Apply Fixes
Apply all fixes directly to files:
```bash
buf-plugin-aip fix ./proto
```

### Granular Filtering
```bash
# Fix only timestamp naming (AIP-142)
buf-plugin-aip fix --category AIP_0142 ./proto

# Fix specific rule
buf-plugin-aip fix --rule AIP_0133_REQUEST_RESOURCE_FIELD ./proto

# Exclude specific rules from being auto-fixed
buf-plugin-aip fix --except AIP_0122_NAME_SUFFIX ./proto
```

---

## Rule ID Mapping

Google API Linter rule names follow `<group>::<aip>::<name>` and are converted to standard Buf rule IDs:

| Google API Linter Rule | Buf Plugin Rule ID | Category |
|------------------------|--------------------|----------|
| `core::0131::http-body` | `AIP_0131_HTTP_BODY` | `AIP`, `AIP_CORE`, `AIP_0131` |
| `core::0131::request-message-name` | `AIP_0131_REQUEST_MESSAGE_NAME` | `AIP`, `AIP_CORE`, `AIP_0131` |
| `core::0203::field-behavior-required` | `AIP_0203_FIELD_BEHAVIOR_REQUIRED` | `AIP`, `AIP_CORE`, `AIP_0203` |
| `client-libraries::4232::...` | `AIP_4232_...` | `AIP`, `AIP_CLIENT_LIBRARIES`, `AIP_4232` |

---

## License

Apache 2.0
