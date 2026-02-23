# ADR-021: Quoted Timezone Values in Spec Strings

## Status
Accepted

## Date
2026-02-23

## Context

Unix/Linux shell users habitually quote environment variable values. When writing cron specs with timezone prefixes, they naturally write:

```
TZ="America/New_York" 0 9 * * *
TZ='UTC' * * * * *
CRON_TZ="Asia/Tokyo" 30 4 * * *
```

Prior to this change, these specs failed with `invalid character '"' at position 0 in timezone` because the `validateTimezone()` function (correctly) rejects quote characters as invalid IANA timezone name characters.

This was reported as [#335](https://github.com/netresearch/go-cron/issues/335).

## Decision

Strip matching quote pairs from the timezone value **before** validation and loading. The stripping happens in `parseTimezone()` after extracting the raw value between `=` and the first space, but before calling `validateTimezone()`.

### What IS Supported

| Input | Parsed timezone | Notes |
|-------|----------------|-------|
| `TZ="UTC" * * * * *` | `UTC` | Double quotes stripped |
| `TZ='UTC' * * * * *` | `UTC` | Single quotes stripped |
| `CRON_TZ="America/New_York" 0 9 * * *` | `America/New_York` | Works with both prefixes |
| `TZ=UTC * * * * *` | `UTC` | Unquoted still works (backwards-compatible) |

### What is NOT Supported

| Input | Result | Rationale |
|-------|--------|-----------|
| `TZ="America/New York" ...` | Error (parses `"America/New` due to space split) | IANA timezone names never contain spaces. Supporting spaces inside quotes would require quote-aware tokenization, a disproportionate complexity increase for a non-existent use case. |
| `TZ="UTC' ...` | Error (mismatched quotes not stripped, `"` fails validation) | Only matching pairs are stripped |
| `TZ="" ...` | Error (empty timezone after stripping) | Empty timezone is always invalid |
| `TZ="UTC ...` | Error (opening quote without closing, `"` fails validation) | Partial quoting is not supported |

### Implementation

The change is minimal — 5 lines added to `parseTimezone()` in `parser.go`:

```go
if len(tzName) >= 2 {
    if (tzName[0] == '"' && tzName[len(tzName)-1] == '"') ||
        (tzName[0] == '\'' && tzName[len(tzName)-1] == '\'') {
        tzName = tzName[1 : len(tzName)-1]
    }
}
```

Key design choices:
- **Strip before validation**: Quotes are removed before `validateTimezone()` runs, so the validation logic and character allowlist remain unchanged
- **No changes to `isValidTimezoneChar()`**: Quote characters are still invalid in timezone names — we just strip them before they reach validation
- **Space-based splitting unchanged**: The parser still splits on the first space character, so `TZ="America/New York"` does not work (and shouldn't, since no real IANA timezone contains a space)

## Consequences

### Positive

- **Shell user ergonomics**: Users who copy-paste from shell configs or write specs by muscle memory no longer get confusing errors
- **Backwards-compatible**: Unquoted values continue to work identically
- **Zero risk**: Stripping quotes before validation means security properties are preserved — path traversal, injection, and length checks still apply to the actual timezone name
- **Minimal code change**: 5 lines of production code, no new dependencies

### Negative

- **Slight parsing ambiguity**: A timezone value literally named `"UTC"` (with quotes as part of the name) would now be interpreted as `UTC`. This is not a real concern since IANA timezone names never contain quotes.

### Neutral

- **No behavioral change for valid specs**: This is purely additive — previously-invalid specs now parse, previously-valid specs are unchanged

## Alternatives Considered

### 1. Document That Quoting Is Not Allowed

Add documentation telling users not to quote timezone values.

**Rejected because:**
- Users make this mistake by reflex from years of shell scripting
- A one-time 5-line fix is better than perpetual user confusion
- "Don't do that" documentation is rarely read before the error occurs

### 2. Add Quotes to Valid Character Set

Allow `"` and `'` through `isValidTimezoneChar()` and let `time.LoadLocation()` handle the error.

**Rejected because:**
- `time.LoadLocation("\"UTC\"")` would fail with a confusing "unknown time zone" error
- Validation should catch the issue early with a clear message
- Quotes are genuinely not valid in IANA timezone names

### 3. Quote-Aware Tokenization

Rewrite the spec parser to handle quoted fields, supporting spaces inside quotes.

**Rejected because:**
- Disproportionate complexity for zero practical benefit (no IANA timezone contains spaces)
- Would change the fundamental parsing model
- Higher risk of regressions

## References

- [#335](https://github.com/netresearch/go-cron/issues/335): Feature request for quoted TZ/CRON_TZ values
- [IANA Time Zone Database](https://www.iana.org/time-zones): Authoritative timezone name list (no names contain quotes or spaces)
