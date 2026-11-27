# OpenRouter 图片接口

渠道类型：`20`（OpenRouter）。默认 Base URL 为 `https://openrouter.ai/api`，
适配入口位于 `relay/channel/openai/`。本文描述该渠道的同步图片协议转换；
其他渠道继续使用各自的协议。

## 官方依据与验证范围

核对日期：**2026-11-16**。

- [Image Generation 官方文档](https://openrouter.ai/docs/guides/overview/multimodal/image-generation)
- [GPT Image 2 端点能力](https://openrouter.ai/api/v1/images/models/openai/gpt-image-2/endpoints)
- [Gemini 3.1 Flash Image 端点能力](https://openrouter.ai/api/v1/images/models/google/gemini-3.1-flash-image/endpoints)

验证包括官方文档、上述无需密钥的只读模型能力查询，以及本地协议 fixture。
本次没有执行真实付费图片生成，也没有完成 DramaClaw CE 真实端到端验证。
因此，下面的模型示例表示可配置的上游 ID，不表示所有参数组合均已通过真实调用。

| CE 常用别名示例 | OpenRouter 上游模型 ID | 查询时的端点限制 |
|---|---|---|
| `LingShan-G2` | `openai/gpt-image-2` | OpenAI 端点：`n` 为 1–10，最多 16 张参考图，支持流式响应；具体比例、质量和输出格式参见端点能力 |
| `LingShan-NB-2` | `google/gemini-3.1-flash-image` | Google 端点：`n=1`，最多 14 张参考图，分辨率为 `512`、`1K`、`2K`、`4K`，未声明流式支持 |

在渠道模型映射中把本地别名映射到上游模型 ID。模型及供应商的能力可能变化，
使用前应重新查询所选模型的端点记录。不能把渠道级 `image` 能力当作任意模型、
任意 `n`、比例、分辨率或参考图数量的保证。

## 请求与响应

网关保留 OpenAI 兼容的入站接口：

| 网关入站请求 | OpenRouter 上游请求 |
|---|---|
| `POST /v1/images/generations` | `POST https://openrouter.ai/api/v1/images` |
| `POST /v1/images/edits` | `POST https://openrouter.ai/api/v1/images` |

两种请求均使用 OpenRouter 的 JSON Image API。参考图统一转换为：

```json
{
  "input_references": [
    {
      "type": "image_url",
      "image_url": {"url": "https://example.com/reference.png"}
    }
  ]
}
```

支持的参考图来源包括 JSON `image` URL / URL 数组、`images` 参考图对象，
以及 multipart 的 `image` / `image[]` 图片文件。上传文件转换为带 MIME 类型的
base64 data URL；JSON 中的 HTTP(S) URL 和 data URL 可直接传递给上游。
原生 OpenRouter `input_references` 也可用于 JSON 请求。

原生参数按允许列表传递，包括 `provider` 和 `seed`，同时保留显式的 `false` 或 `0`。
开启全局或渠道请求体透传时，OpenRouter 图片请求仍会执行协议转换。
OpenAI 的 `response_format` 不改变 OpenRouter 原生返回格式。

OpenRouter 图片适配使用同步 JSON 响应。`stream: true` 返回 HTTP 400。
响应中的 `data[].b64_json`、可选的 `media_type` 和 `usage` 沿用现有图片响应链路。

## DramaClaw 几何参数

例如 CE 提交：

```json
{
  "model": "LingShan-NB-2",
  "prompt": "参考输入图片生成一张新的场景图",
  "n": 1,
  "image": ["https://example.com/reference.png"],
  "width": 688,
  "height": 1024,
  "metadata": {"ratio": "2:3", "resolution": "1k"}
}
```

网关将 DC-Media `metadata.ratio` / `metadata.resolution` 转为 OpenRouter 的
`aspect_ratio` / `resolution`。这些语义参数优先于网关由 `width` / `height`
归一化得到的像素 `size`，避免把 CE 内部的尺寸计算结果和语义比例重复提交。
如果调用方仅提供标准 `size`，则保留该字段供上游解释。
OpenAI 的 `size: "auto"`（忽略大小写和首尾空白）在 OpenRouter 转换中视为未指定尺寸，
不会覆盖已提供的比例和分辨率；没有这些语义参数时省略 `size`，使用供应商默认值。

OpenRouter 对显式像素尺寸的支持随模型和供应商变化；其官方文档说明，
显式 `size` 与不匹配的比例 / 分辨率组合可能返回 400。Gemini 的上述端点记录
仅声明语义比例与分辨率，不应据此假定其支持任意像素尺寸。

## 限制

- 此适配不支持 OpenAI `mask` 编辑。OpenRouter 的 `markup` 与 OpenAI alpha mask
  语义不同，不能静默互换。
- 生图模型对 `n`、参考图数量、比例、分辨率和质量有各自的限制。
  网关通用请求边界并不替代供应商的模型级校验。
- 图片请求通过本次同步 HTTP 调用返回结果。此路径没有另设异步任务创建、
  查询或取消接口。
- 真实账号权限、账户额度、供应商可用性和端到端图片结果仍需部署后验证。
