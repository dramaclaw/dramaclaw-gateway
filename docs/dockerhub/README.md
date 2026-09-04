# dramaclaw-gateway

The OpenAI-compatible model gateway bundled with every [DramaClaw](https://github.com/dramaclaw/dramaclaw) CE install. It accepts the **DC-Media** contract DramaClaw uses for image, video and audio generation and converts each request into the provider's native API. A maintained fork of [New API](https://github.com/QuantumNous/new-api).

Source and docs: <https://github.com/dramaclaw/dramaclaw-gateway>

## With DramaClaw (recommended)

DramaClaw's `docker-compose.yml` already starts this image as the `newapi` service:

```bash
git clone https://github.com/dramaclaw/dramaclaw.git && cd dramaclaw
cp .env.example .env
docker compose up -d
```

Open http://localhost:8080 → Settings → Model Config → **Custom** (or **Local + Official Hybrid**) to initialize the gateway and add your provider channels. Pin the version with `DRAMACLAW_GATEWAY_VERSION` in `.env`.

## Standalone

```bash
docker run -d --name dramaclaw-gateway \
  -p 3000:3000 \
  -v dramaclaw-gateway-data:/data \
  -e TZ=Asia/Shanghai \
  claymorelab/dramaclaw-gateway:v1.0.0-rc.24-dramaclaw.1
```

Open http://localhost:3000 and complete the setup wizard.

## Tags

- `v<upstream>-dramaclaw.N`, e.g. `v1.0.0-rc.24-dramaclaw.1`. Multi-arch (amd64 / arm64), cosign-signed, with SBOM and provenance.
- **No `latest` tag.** Pin the exact tag DramaClaw CE was released with.

## Image facts

- Port `3000`, working directory `/data`, SQLite at `/data/one-api.db` by default.
- Set `SQL_DSN` for PostgreSQL / MySQL; every other variable matches upstream New API (`.env.example` in the repo).

## Providers

ComfyUI · MiniMax / Hailuo · VolcEngine Doubao / Seedance · fal.ai · Ali · Kling · Jimeng · Vertex AI · Gemini · OpenAI / Sora · Suno. Verification status: [channel support matrix](https://github.com/dramaclaw/dramaclaw-gateway/blob/main/docs/providers/en/README.md).
