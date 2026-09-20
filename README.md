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

### 1. Curated Presets (Recommended)
Instead of enabling all 340+ rules at once (which requires doc comments on every field and language-specific options), use a curated preset:

```yaml
version: v2
lint:
  use:
    - AIP_RECOMMENDED # Core API design without pedantic comments or language options
plugins:
  - plugin: buf-plugin-aip
```

Available presets:
- **`AIP_RECOMMENDED`**: High-value API design (CRUD signatures, resource naming, pagination, HTTP mappings) omitting comment and language-specific checks.
- **`AIP_CRUD`**: Standard resource methods only (AIP-131 through AIP-136: Get, List, Create, Update, Delete, Custom methods).
- **`AIP_NOLANG`**: Core rules without language-specific packaging options (Java, C#, PHP, Ruby).
- **`AIP_CORE`**: All core rules (default set).
- **`AIP`**: All 342 AIP rules.

### 2. Auto-Fix via `buf.yaml` (`options.auto_fix`)
You can enable automatic in-place fixing during `buf lint`. Any machine-fixable violation will be automatically corrected on disk and suppressed from the error list:

```yaml
version: v2
lint:
  use:
    - AIP_CORE
plugins:
  - plugin: buf-plugin-aip
    options:
      auto_fix: true
```

### 3. Targeting a Specific AIP (e.g. AIP-131)
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

## CLI Commands

In addition to running as a Buf plugin, `buf-plugin-aip` provides standalone CLI tools:

### 1. Standalone Linter (`buf-plugin-aip check`)
Lint any `.proto` files directly without requiring `buf`:
```bash
# Check all proto files in directory
buf-plugin-aip check ./proto

# Filter by category or rule
buf-plugin-aip check --category AIP_CORE ./proto
buf-plugin-aip check --rule AIP_0131_HTTP_BODY ./proto

# Only display violations that can be auto-fixed
buf-plugin-aip check --fixable-only ./proto

# Output violations as JSON (useful for CI scripts/tooling)
buf-plugin-aip check --json ./proto
```

### 2. Auto-Fix (`buf-plugin-aip fix`)
Automatically resolve machine-fixable violations in-place:
```bash
# Preview fixes with unified diffs
buf-plugin-aip fix --dry-run --diff ./proto

# Apply fixes directly
buf-plugin-aip fix ./proto

# Fix only specific rules or categories
buf-plugin-aip fix --category AIP_0142 ./proto
buf-plugin-aip fix --except AIP_0122_NAME_SUFFIX ./proto
```

### 3. Rule Inspector (`buf-plugin-aip explain`)
Inspect the requirements, rationale, spec link, and auto-fixability of any rule or AIP number:
```bash
# Inspect a specific rule
buf-plugin-aip explain AIP_0131_HTTP_BODY

# Inspect an entire AIP proposal
buf-plugin-aip explain 142
```

### 4. Rule Catalog (`buf-plugin-aip list-rules`)
Explore and search the 340+ rules:
```bash
# List all rules in a category
buf-plugin-aip list-rules --category AIP_0142

# List only rules that support auto-fixing
buf-plugin-aip list-rules --fixable

# Search rules by keyword
buf-plugin-aip list-rules --search timestamp

# Export rule catalog as JSON
buf-plugin-aip list-rules --json
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
