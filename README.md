# dramaclaw-gateway

[简体中文](./README.zh_CN.md) | English

`dramaclaw-gateway` is the open-source model gateway for
[DramaClaw](https://github.com/dramaclaw/dramaclaw). It accepts the stable
DC-Media request contract used by DramaClaw, normalizes media semantics, and
converts requests into provider-specific image and video APIs.

```text
DramaClaw
    -> DC-Media request
    -> dramaclaw-gateway normalization
    -> provider adapter
    -> provider API
```

The project is based on [New API](https://github.com/QuantumNous/new-api) and
continues to reuse its gateway infrastructure and adapters. Media endpoints in
this repository follow DC-Media semantics and do not promise compatibility with
historical New API media task request or response shapes.

## Project Scope

The gateway is responsible for:

- accepting the public image and asynchronous video contracts used by
  DramaClaw;
- preserving first frame, last frame, reference image, reference video, and
  reference audio roles;
- validating unsupported combinations before contacting a provider;
- translating normalized requests into provider-specific payloads;
- mapping provider task IDs, statuses, errors, and result URLs back to the
  public contract;
- exposing registered channel metadata through `GET /api/channel/types`.

DramaClaw owns the user-facing model catalog and workflow controls. Provider
adapters own provider authentication, endpoints, payloads, limits, and response
mapping. The gateway must not infer DramaClaw workflow modes from provider model
names.

Commercial RelayClaw billing, audit, archival, and operations features are not
goals of this repository.

## DC-Media Contract

The canonical specification is [`dc-media-protocol.md`](./dc-media-protocol.md).
English contributors can use the standalone
[implementation reference](./docs/dc-media/en/protocol.md). Start with the
documentation index before implementing an adapter:

- [DC-Media documentation](./docs/dc-media/en/README.md)
- [Adapter development guide](./docs/dc-media/en/adapter-development.md)
- [Example provider adapter](./docs/dc-media/en/example-adapter.md)
- [Model onboarding checklist](./docs/dc-media/en/model-onboarding-checklist.md)
- [Channel support matrix](./docs/providers/en/README.md)

## Local Development

Prerequisites:

- Go version declared by [`go.mod`](./go.mod)
- Bun `1.3.14` for the web frontend
- Docker with Compose for the development database and Redis

Clone and start the API dependencies:

```bash
git clone https://github.com/dramaclaw/dramaclaw-gateway.git
cd dramaclaw-gateway
make dev-api
```

Start the frontend in a second terminal:

```bash
make dev-web
```

Open `http://localhost:5173`. The frontend development server proxies API calls
to the gateway on port `3000`.

To rebuild the API container after Go changes:

```bash
make dev-api-rebuild
```

The root `docker-compose.yml` may track upstream deployment defaults. Community
development should use `docker-compose.dev.yml` through the Make targets above
so the running API is built from this checkout.

## Contributing a Channel

Before writing code:

1. Open a **Provider adapter request** issue and attach the official provider
   documentation, authentication method, supported models, limits, and
   sanitized request/response examples.
2. Confirm whether an existing New API adapter can be extended without breaking
   DC-Media semantics.
3. Read [`CONTRIBUTING.md`](./CONTRIBUTING.md) and claim the issue.
4. Use the DC-Media request as the input contract. Provider fields belong only
   inside the provider adapter.

Create a compile-safe adapter scaffold with:

```bash
make new-adapter PROVIDER=example TYPE=64 MODE=task CAPABILITIES=video
```

Use `MODE=sync` for synchronous image, audio, or protocol adapters. The command
creates adapter, test, and bilingual provider-document stubs, but deliberately does not
edit shared channel registries.

An adapter is not complete merely because one request succeeds. It must preserve
media roles, reject unsupported combinations, map asynchronous task state and
errors, declare channel capabilities, and include conversion tests.

Useful validation commands:

```bash
go test ./relay/common ./relay/channel/task/<provider>
go test ./... -run '^$'
cd relaykit && GOWORK=off go build ./...
cd ../web && bun install --frozen-lockfile
bun run typecheck
bun run build
```

See the full [contribution guide](./CONTRIBUTING.md) for registration points,
testing requirements, and the adapter definition of done.

## Repository Layout

```text
relay/common/                 DC-Media normalization and validation
relay/channel/task/<provider> asynchronous media provider adapters
relay/channel/<provider>      synchronous provider adapters
relay/relay_adaptor.go        adapter factory registration
constant/channel.go           stable channel type IDs and default URLs
relay/channel_types.go        discoverable channel metadata
docs/dc-media/                protocol implementation guides
docs/providers/               provider support and limitations
web/                          channel administration frontend
```

## Security

Do not commit API keys, exported local workflows containing private paths,
generated media, databases, or raw provider responses containing credentials.
Report vulnerabilities through the repository's GitHub Security Advisory flow;
do not open a public issue for an undisclosed vulnerability.

## License and Attribution

`dramaclaw-gateway` is distributed under the
[GNU Affero General Public License v3.0](./LICENSE). Modified distributions and
network deployments must comply with AGPLv3 source availability, notice, and
attribution obligations.

This repository is a modified distribution based on
[QuantumNous/new-api](https://github.com/QuantumNous/new-api), is independently
maintained by the DramaClaw community, and is not an official New API release.
The original license, [`NOTICE`](./NOTICE), copyright notices, and third-party
notices are preserved.

Frontend design and development by New API contributors.

The user interface must retain a visible link to the original New API project as
required by the applicable AGPLv3 Section 7 additional terms in `NOTICE`.
