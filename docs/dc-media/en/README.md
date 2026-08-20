# DC-Media Development Documentation

This directory is for contributors building provider and model adapters for
`dramaclaw-gateway`. The canonical public request and response contract is
[`dc-media-protocol.md`](../../../dc-media-protocol.md). These documents explain
how to implement and test that contract.

## Reading Order

1. [`protocol.md`](./protocol.md): standalone English implementation reference.
   The canonical specification remains [`dc-media-protocol.md`](../../../dc-media-protocol.md).
2. [`adapter-development.md`](./adapter-development.md): adapter types,
   registration, and conversion rules.
3. [`example-adapter.md`](./example-adapter.md): minimal asynchronous video
   adapter example.
4. [`model-onboarding-checklist.md`](./model-onboarding-checklist.md): model
   implementation and verification checklist.
5. [`../../providers/en/README.md`](../../providers/en/README.md): current
   registration status and contribution opportunities.
6. [`upstream-sync-release.md`](./upstream-sync-release.md): New API upstream
   synchronization and release requirements.

## Contribution Entry Points

- Use the **Provider adapter request** issue template before adding a provider.
- Follow [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) for implementation and
  testing requirements.
- If the public protocol lacks a required capability, open a protocol issue
  before adding a provider-specific public field.
- To add a model to an existing provider, provide official model documentation,
  limits, and sanitized request examples when claiming the work.

## Ownership Boundaries

- The DramaClaw model catalog controls user-visible modes and limits.
- The DC-Media common layer owns stable fields, normalization, and media roles.
- Provider adapters own provider protocol, authentication, limits, and response
  mapping.
- `/api/channel/types` is the runtime source of truth for registered
  channel-level capabilities.
- `docs/providers/` records human verification and known gaps; it does not
  replace runtime metadata.

中文文档：[`../README.md`](../README.md)。
