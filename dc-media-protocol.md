# DC-Media-Protocol：媒体模型统一请求协议

简体中文 | [English](./dc-media-protocol.en.md)

> 状态：草案
>
> 适用范围：DramaClaw 图片与视频模型接入
>
> 协议版本：1.0-draft

本文档定义 DramaClaw 向统一模型网关发送图片、视频生成请求时使用的公共协议。新增媒体模型或供应商时，应优先适配本协议，不应在业务层新增供应商专属请求结构。

本文使用以下规范用语：

- **必须（MUST）**：协议强制要求。
- **应该（SHOULD）**：除非有明确理由，否则应遵守。
- **可以（MAY）**：可选能力。

## 1. 目标与边界

统一媒体协议用于隔离业务语义与供应商差异：

```text
模型目录声明能力
        ↓
DramaClaw 选择业务模式并构造统一请求
        ↓
统一模型网关校验公共协议
        ↓
供应商适配器转换为供应商请求
```

各层职责如下：

- **模型目录**：声明模型身份、支持模式、分辨率、比例、素材数量与时长限制，以及可选模型参数。
- **DramaClaw**：校验用户输入，解析模型目录，生成规范化请求，并保证报价参数与执行参数一致。
- **统一模型网关**：校验公共字段之间的一致性，将公共值转换为供应商所需格式，并处理供应商差异。
- **供应商适配器**：只负责字段映射和供应商约束，不得改变公共字段表达的素材角色，也不得静默丢弃输入素材。

本文档不定义：

- 用户积分价格或人民币换算关系；
- 供应商采购成本；
- 密钥、鉴权头、生产域名或内部部署方式；
- 单个供应商未公开的私有参数。

## 2. 设计原则

1. **一种语义只有一种公共表示。** 例如自动比例统一为 `auto`，不在业务层混用 `adaptive`。
2. **供应商差异停留在网关。** DramaClaw 不应根据供应商分别发送 `auto`、`adaptive`、`-1` 或省略字段。
3. **素材角色由字段表达，调用形态由素材组合确定。** 首帧、尾帧和参考图片必须使用各自字段；网关可以根据规范字段及素材数量选择对应的上游调用形态，但不得把参考图片自动改写为首帧。
4. **能力由模型目录声明。** 前端展示和后端校验必须读取同一模型目录；前端限制不能替代后端校验。
5. **报价与执行共享规范化结果。** 模型、分辨率、时长、素材数量和是否包含视频输入必须一致。
6. **新代码只产生规范字段。** 旧字段只允许在兼容边界读取，不得继续从新调用链发送。

## 3. 接口与基础结构

### 3.1 图片接口

#### 3.1.1 文生图接口

文生图使用：

```http
POST /images/generations
```

该接口用于只依赖提示词生成图片。请求中不得携带参考图片；固定比例时发送
`width`、`height` 和对应的 `metadata.ratio`，自动比例时只发送
`metadata.ratio = "auto"`。

基础请求结构：

```json
{
  "model": "example-image-model",
  "prompt": "一只猫在海边散步",
  "n": 1,
  "response_format": "b64_json",
  "width": 2048,
  "height": 1152,
  "metadata": {
    "ratio": "16:9",
    "resolution": "2k"
  }
}
```

#### 3.1.2 图片编辑与参考图生成接口

带一张或多张参考图的图片生成、重绘或编辑使用：

```http
POST /images/edits
```

参考图片统一放入顶层 `image` 数组。该接口不得改用文生图端点，也不得使用
`image_url`、`image_urls` 等供应商字段。

基础请求结构：

```json
{
  "model": "example-image-model",
  "prompt": "保持人物身份，将场景改为雨夜街道",
  "image": [
    "https://example.invalid/person.png",
    "https://example.invalid/style.png"
  ],
  "n": 1,
  "response_format": "b64_json",
  "metadata": {
    "ratio": "auto",
    "resolution": "2k"
  }
}
```

具体的固定比例、自动比例和参考图数量约束分别见第 5 节和第 6 节。

### 3.2 视频接口

- 视频生成：`POST /video/generations`

基础结构：

```json
{
  "model": "example-video-model",
  "prompt": "人物向镜头走来",
  "duration": 5,
  "width": 1280,
  "height": 720,
  "n": 1,
  "response_format": "url",
  "metadata": {
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": true,
    "human_review": false,
    "watermark": false
  }
}
```

### 3.3 公共顶层字段

| 字段 | 类型 | 图片 | 视频 | 说明 |
|---|---|---:|---:|---|
| `model` | string | 是 | 是 | 网关模型名称；来源于模型目录，不接受前端另行指定计费模型 |
| `prompt` | string | 是 | 是 | 用户提示词或由业务层构造的最终提示词 |
| `image` | string 或 string[] | 可选 | 可选 | 图片编辑时为参考图列表；视频时仅表示首帧 |
| `duration` | integer 或 `"auto"` | 否 | 可选 | 视频输出时长 |
| `width` | integer | 可选 | 可选 | 固定画幅的期望宽度；必须与 `height` 同时出现 |
| `height` | integer | 可选 | 可选 | 固定画幅的期望高度；必须与 `width` 同时出现 |
| `n` | integer | 是 | 是 | 单次请求生成数量；当前默认值为 `1` |
| `response_format` | string | 是 | 是 | 图片通常为 `b64_json`，视频为 `url` |
| `metadata` | object | 是 | 是 | 画幅语义、清晰度、参考素材和可选能力 |

## 4. 值规范化

DramaClaw 在向网关发送请求前，必须执行以下规范化：

```text
adaptive → auto
4K       → 4k
2K       → 2k
1K       → 1k
1080P    → 1080p
720P     → 720p
480P     → 480p
```

规则：

- 命名分辨率档位必须使用小写。
- 明确像素尺寸（例如 `1920x1080`）不得被改写成分辨率档位。
- 新请求不得发送 `adaptive`；网关可按供应商要求将 `auto` 转换为 `adaptive` 或省略比例。
- 新请求不得使用 `duration: -1` 表示自动时长。

## 5. 几何协议

### 5.1 固定比例

固定比例请求必须保留画幅语义，并在能够确定像素尺寸时同时发送宽高：

```json
{
  "width": 2048,
  "height": 1152,
  "metadata": {
    "ratio": "16:9",
    "resolution": "2k"
  }
}
```

约束：

- `metadata.ratio` 是画幅语义的主要来源。
- `metadata.resolution` 是模型清晰度和计费档位，不等同于宽高比。
- `width`、`height`、`ratio` 和 `resolution` 应由同一次尺寸映射生成，不得分别推导。
- `width` 和 `height` 必须同时出现，且必须为正整数。
- 即使当前调用无法计算 `width`、`height`，固定的 `metadata.ratio` 也不得被丢弃。
- 网关应允许编码尺寸取整造成的合理误差，不能要求像素宽高与比例数学上完全相等。

禁止同时发送已废弃的合并尺寸字段：

```json
{
  "size": "2048x1152",
  "aspect_ratio": "16:9"
}
```

### 5.2 自动比例

自动比例统一表示为：

```json
{
  "metadata": {
    "ratio": "auto",
    "resolution": "2k"
  }
}
```

自动比例请求：

- 必须发送 `metadata.ratio = "auto"`；
- 不得发送 `width`、`height` 或固定 `size`；
- 可以独立发送 `metadata.resolution`；
- 由网关根据输入素材和供应商协议决定最终比例参数。

## 6. 图片请求

### 6.1 文生图

固定比例示例：

```json
{
  "model": "example-image-model",
  "prompt": "电影感夜景街道",
  "n": 1,
  "response_format": "b64_json",
  "width": 1024,
  "height": 1024,
  "metadata": {
    "ratio": "1:1",
    "resolution": "1k"
  }
}
```

自动比例示例：

```json
{
  "model": "example-image-model",
  "prompt": "保持构图并改善光影",
  "n": 1,
  "response_format": "b64_json",
  "metadata": {
    "ratio": "auto",
    "resolution": "2k"
  }
}
```

### 6.2 图片编辑与多图参考

参考图片统一使用顶层 `image` 数组：

```json
{
  "model": "example-image-model",
  "prompt": "保持人物身份，将场景改为雨夜街道",
  "image": [
    "https://example.invalid/person.png",
    "https://example.invalid/style.png"
  ],
  "n": 1,
  "response_format": "b64_json",
  "metadata": {
    "ratio": "auto",
    "resolution": "2k"
  }
}
```

约束：

- 图片编辑使用 `/images/edits`。
- `image` 中的顺序可表达业务层的参考优先级，但不得改变字段语义。
- 参考图片数量必须同时满足模型目录和网关限制。
- 自动比例时不得根据第一张参考图预先生成 `width`、`height`。

### 6.3 图片可选参数

图片模型可以使用以下顶层可选字段，前提是模型目录已声明并允许用户配置：

| 字段 | 类型 | 示例 | 说明 |
|---|---|---|---|
| `quality` | string | `"low"` | 图片质量档位 |
| `output_format` | string | `"png"` | 输出格式 |
| `input_fidelity` | string | `"high"` | 参考图还原强度 |
| `watermark` | boolean | `false` | 是否生成水印；仅在公共图片端点支持时发送 |

未声明或用户未选择的可选参数应该省略，不应主动猜测供应商默认值。

## 7. 视频生成模式

模型目录使用以下规范模式名称：

| 模式 | 含义 |
|---|---|
| `text_to_video` | 纯文本生成视频 |
| `image_to_video` | 单张图片作为整体内容参考，不锁定首帧 |
| `first_frame` | 单张图片严格作为视频首帧 |
| `first_last_frame` | 首帧、尾帧或首尾帧组合 |
| `image_reference` | 一张或多张图片作为风格、角色或内容参考 |
| `all_reference` | 图片、视频、音频的多模态参考 |
| `video_edit` | 以源视频为基础进行编辑 |

模式是 DramaClaw 内部的业务语义，用于决定界面、模型目录校验、素材角色和最终公共字段。模式本身不属于当前线上请求协议；进入网关后，单图、多图和全能参考允许按素材组合归一化为供应商可支持的调用形态。

当前协议不使用顶层 `mode` 字段。DramaClaw 根据业务模式生成规范的素材字段组合，网关按照第 7.2 节的固定优先级识别调用形态并转换为供应商协议。客户端和单个供应商适配器不得私自增加 `mode`，也不得建立另一套模式推断规则。

### 7.1 模式与字段映射

| 模式 | 顶层 `image` | `last_frame_image` | `reference_images` | `reference_videos` | `reference_audios` | 比例 | 时长 |
|---|---|---|---|---|---|---|---|
| `text_to_video` | 不发送 | 不发送 | 不发送 | 不发送 | 不发送 | 固定或目录允许的值 | 固定 |
| `image_to_video` | 不发送 | 不发送 | 恰好 1 张 | 不发送 | 不发送 | 固定或目录允许的值 | 固定 |
| `first_frame` | 首帧 | 不发送 | 不发送 | 不发送 | 不发送 | `auto` | 固定 |
| `first_last_frame` | 有首帧时发送 | 有尾帧时发送 | 不发送 | 不发送 | 不发送 | `auto` | 固定 |
| `image_reference` | 不发送 | 不发送 | 1 张或多张 | 不发送 | 不发送 | 固定或目录允许的值 | 固定 |
| `all_reference` | 不发送 | 不发送 | 可选 | 可选 | 可选 | 固定或目录允许的值 | 固定 |
| `video_edit` | 不发送 | 不发送 | 可选 | 源视频及允许的参考视频 | 可选 | `auto` | `auto` |

说明：

- 表中的参考字段均位于 `metadata`。
- `image_to_video`、`image_reference` 和 `all_reference` 是不同的 DramaClaw 业务模式，但在线协议允许它们归一化为相同或相近的素材结构。
- 只有一张 `reference_images` 时，网关统一按图生视频调用形态处理；即使该请求源自图片参考或全能参考模式，也不要求网关保留模式名称。
- 有多张 `reference_images` 且没有视频或音频时，网关按图片参考调用形态处理；供应商只支持单图时必须返回明确的不支持错误，不得静默丢弃多余图片。
- 一旦存在 `reference_videos` 或 `reference_audios`，固定时长请求按全能参考调用形态处理。
- `first_last_frame` 至少需要一个关键帧；只有尾帧时不得把尾帧自动提升为首帧。
- 参考图片的第一张不得自动作为首帧。
- `video_edit` 的源视频仍通过 `metadata.reference_videos` 传递，业务模式决定它是编辑源而不是普通参考视频。

### 7.2 网关调用形态推断

网关必须先校验互斥字段，再按以下优先级识别调用形态：

| 优先级 | 请求特征 | 调用形态 |
|---:|---|---|
| 1 | `duration = "auto"`、`metadata.ratio = "auto"` 且存在 `reference_videos` | 视频编辑 |
| 2 | 存在 `last_frame_image`，可同时存在顶层 `image` | 首尾帧或尾帧视频 |
| 3 | 存在顶层 `image` | 首帧视频 |
| 4 | 存在 `reference_videos` 或 `reference_audios` | 全能参考 |
| 5 | `reference_images` 数量大于 1 | 图片参考 |
| 6 | `reference_images` 数量等于 1 | 图生视频 |
| 7 | 不存在任何输入素材 | 文生视频 |

推断规则：

- 推断结果只用于选择供应商端点、Workflow 或请求结构，不用于恢复 DramaClaw 原始界面模式。
- 素材数组中的空字符串不计入数量；重复素材是否允许由模型目录和供应商约束决定，不得通过静默去重绕过数量限制。
- 顶层 `image` 与 `reference_images` 不得同时出现。首帧和参考图片具有不同语义，网关不得通过取第一张图片解决冲突。
- `last_frame_image` 不得与 `reference_images`、`reference_videos` 或 `reference_audios` 混用。
- `duration = "auto"` 但没有参考视频时属于无效请求，不能据此推断视频编辑。
- 供应商不支持推断出的调用形态时，应返回稳定的不支持错误，不得降级后忽略素材。

### 7.3 首帧

```json
{
  "model": "example-video-model",
  "prompt": "人物转身看向镜头",
  "image": "https://example.invalid/first.png",
  "duration": 5,
  "metadata": {
    "ratio": "auto",
    "resolution": "720p"
  }
}
```

### 7.4 首尾帧

```json
{
  "model": "example-video-model",
  "prompt": "从白天自然过渡到夜晚",
  "image": "https://example.invalid/first.png",
  "duration": 5,
  "metadata": {
    "last_frame_image": "https://example.invalid/last.png",
    "ratio": "auto",
    "resolution": "720p"
  }
}
```

首尾帧模式的比例强制为 `auto`，不得发送 `width` 或 `height`。

### 7.5 图片参考与全能参考

```json
{
  "model": "example-video-model",
  "prompt": "参考角色与场景生成新的镜头",
  "duration": 8,
  "metadata": {
    "ratio": "16:9",
    "resolution": "720p",
    "reference_images": [
      "https://example.invalid/character.png",
      "https://example.invalid/scene.png"
    ],
    "reference_videos": [
      "https://example.invalid/motion.mp4"
    ],
    "reference_audios": [
      "https://example.invalid/voice.mp3"
    ]
  }
}
```

全能参考不是视频编辑。包含参考视频且使用固定 `duration` 时，网关必须按全能参考处理；只有同时满足 `duration = "auto"`、`ratio = "auto"` 和存在参考视频时，才能识别为视频编辑。

### 7.6 视频编辑

```json
{
  "model": "example-video-model",
  "prompt": "保持人物动作，替换背景为海边",
  "duration": "auto",
  "metadata": {
    "ratio": "auto",
    "resolution": "720p",
    "reference_videos": [
      "https://example.invalid/source.mp4"
    ],
    "reference_images": [
      "https://example.invalid/background.png"
    ],
    "reference_audios": [
      "https://example.invalid/music.mp3"
    ]
  }
}
```

视频编辑模式：

- 比例必须为 `auto`；
- 时长必须为 `"auto"`；
- 不得发送 `width`、`height`、`size` 或固定比例；
- 网关根据源视频和供应商协议计算最终输出参数。
- 网关通过 `duration = "auto"`、`metadata.ratio = "auto"` 和存在 `metadata.reference_videos` 的组合识别视频编辑，不需要额外的 `mode` 字段。

## 8. 视频时长

### 8.1 固定时长

```json
{
  "duration": 5
}
```

- 必须是正整数秒。
- 必须符合模型目录的 `minDuration` 与 `maxDuration`。
- 不得同时发送 `seconds`。

### 8.2 自动时长

```json
{
  "duration": "auto"
}
```

- 当前用于视频编辑模式。
- 不得使用 `-1`、`"-1"` 或空值表达自动时长。
- 网关可以按供应商要求转换为 `-1`、省略字段或其他供应商表示。

## 9. 视频可选参数

下列公共视频参数统一位于 `metadata`：

```json
{
  "metadata": {
    "generate_audio": true,
    "human_review": false,
    "watermark": false,
    "output_format": "mp4",
    "return_last_frame": false,
    "scene_optimize": "anime",
    "audio_setting": "auto"
  }
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `generate_audio` | boolean | 是否生成同步音频 |
| `human_review` | boolean | 是否启用真人素材审核或加白流程 |
| `watermark` | boolean | 是否在结果中添加水印 |
| `output_format` | string | 视频封装格式，例如 `mp4`、`mov` |
| `return_last_frame` | boolean | 是否在结果中同时返回尾帧图片 |
| `scene_optimize` | string | 模型声明的场景优化档位 |
| `audio_setting` | string | 视频编辑时的声音处理策略 |

规则：

- 模型目录决定字段是否展示和可用。
- DramaClaw 发送用户选择或已明确的产品默认值。
- 网关仅向支持该字段的供应商发送；不支持时应省略或返回明确的参数错误。
- 可选字段不得被重复放在顶层和 `metadata`。
- 图片请求的 `watermark` 当前是图片端点顶层字段；视频请求的 `watermark` 位于 `metadata`，两者不得混用。

## 10. 响应与任务状态

### 10.1 图片响应

图片接口沿用 OpenAI 兼容的 `data` 数组，并允许一次返回多个结果：

```json
{
  "created": 1786694400,
  "data": [
    {
      "url": "https://example.invalid/result.png"
    }
  ]
}
```

当 `response_format = "b64_json"` 时，结果项使用 `b64_json`，不得同时返回空的
`url`。供应商只返回 URL 或只返回二进制时，由网关转换为客户端请求的格式；无法
转换时返回明确错误。

### 10.2 视频任务提交响应

```json
{
  "id": "task_01H...",
  "task_id": "task_01H...",
  "status": "queued",
  "created_at": 1786694400,
  "model": "example-video-model"
}
```

- `id` 与 `task_id` 必须表示同一个网关公开任务 ID。
- 不得向客户端暴露仅供网关查询上游的供应商任务 ID。
- 创建成功但尚未获得上游进度时统一返回 `queued`。

### 10.3 视频任务查询响应

```json
{
  "id": "task_01H...",
  "task_id": "task_01H...",
  "status": "succeeded",
  "progress": 100,
  "model": "example-video-model",
  "results": [
    {
      "type": "video",
      "url": "https://example.invalid/result.mp4",
      "format": "mp4"
    }
  ]
}
```

`results` 必须是数组。供应商只返回一个结果时也不得改成单个对象。返回尾帧时可以
增加 `type = "image"` 的结果项。

### 10.4 任务状态

网关对外只返回以下状态：

| 状态 | 含义 |
|---|---|
| `queued` | 已创建，等待供应商处理 |
| `running` | 正在处理 |
| `succeeded` | 成功，结果已可用 |
| `failed` | 失败，不会继续处理 |
| `cancelled` | 已取消 |
| `expired` | 任务或临时结果已过期 |

供应商的 `processing`、`submitted`、`SUCCESS` 等状态必须在适配器内映射，不能直接
透传为新的公共状态。

### 10.5 错误响应

```json
{
  "error": {
    "code": "unsupported_media_combination",
    "message": "the selected channel does not support multiple reference images",
    "retryable": false,
    "upstream_request_id": "provider-request-id"
  }
}
```

- `code` 必须稳定，不能使用供应商英文错误文本代替。
- `message` 用于排障，可以包含经过处理的供应商信息。
- `retryable` 表示使用相同参数重试是否可能成功。
- `upstream_request_id` 在供应商提供时应该返回。
- 响应不得包含渠道密钥、鉴权头或完整的上游请求体。

### 10.6 取消任务

取消成功后返回最新任务对象，状态为 `cancelled`。供应商不支持取消时返回
`task_cancellation_unsupported`，不得仅在本地伪造取消成功。

## 11. 模型目录契约

每个媒体模型至少包含稳定身份、网关模型名和能力配置：

```json
{
  "catalog_id": "stable-catalog-id",
  "media_type": "video",
  "label": "Example Video Model",
  "gateway_model": "example-video-model",
  "enabled": true,
  "config": {
    "supportedModes": [
      "text_to_video",
      "first_frame",
      "first_last_frame"
    ],
    "resolutionOptions": ["480p", "720p"],
    "ratioOptions": ["16:9", "9:16", "1:1", "auto"],
    "minDuration": 4,
    "maxDuration": 15,
    "referenceImageMax": 2,
    "referenceVideoMax": 0,
    "referenceAudioMax": 0,
    "supportsGenerateAudio": true,
    "humanReview": true
  }
}
```

约束：

- `catalog_id` 是不可变关联身份；改显示名称或网关模型名时不得更换它。
- `gateway_model` 是实际发送给统一网关的模型名称。
- `label` 仅用于界面展示，不得用于关联、报价或执行模型查询。
- `supportedModes` 必须使用本文第 7 节的规范模式名称。
- 已禁用模型不得出现在新建任务的可选列表中，后端也必须拒绝直接调用。
- 前端只负责提前提示；后端必须重新校验模式、档位和素材限制。

### 11.1 素材限制

模型目录可以声明：

- `referenceImageMax`
- `referenceVideoMax`
- `referenceAudioMax`
- `referenceAudioMinSeconds`
- `referenceAudioMaxSeconds`
- `referenceAudioTotalMinSeconds`
- `referenceAudioTotalMaxSeconds`
- `referenceVideoMinSeconds`
- `referenceVideoMaxSeconds`
- `referenceVideoTotalMinSeconds`
- `referenceVideoTotalMaxSeconds`

单条限制和合计限制应分别校验。留空表示 DramaClaw 不增加目录限制，不代表供应商没有限制；网关仍应执行供应商最终约束。

### 11.2 声明式专用参数

模型可以声明额外参数，但必须映射到安全的公共请求路径：

```json
{
  "request": {
    "endpoint": "video/generations",
    "parameters": [
      {
        "key": "output_format",
        "label": "输出格式",
        "control": "select",
        "requestPath": "metadata.output_format",
        "options": ["mp4", "mov"],
        "default": "mp4",
        "required": false,
        "modes": ["text_to_video", "video_edit"]
      }
    ]
  }
}
```

安全要求：

- 不得允许配置覆盖 `model`、`prompt`、鉴权头、API Key、网关地址等保留字段。
- 参数值必须在后端按控件类型、范围和选项重新校验。
- 模式限定使用规范模式名称。
- 可选且未选择的参数应省略。

## 12. 报价与执行一致性

本文不定义价格，但定义计费输入的一致性：

- 报价、预扣和执行必须使用同一个 `catalog_id`。
- 网关模型必须由该目录项解析，不得由客户端分别提交另一模型名称。
- `resolution` 必须使用规范化后的档位。
- 固定时长必须使用实际发送的规范化时长。
- `duration = "auto"` 的用户计费数量由业务规则从输入素材计算，不能把字符串 `auto` 直接作为数量。
- 是否使用“有视频输入”价格必须根据本次真实存在的 `reference_videos` 判断，不能根据模型素材上限判断。
- 批量数量必须来自本次实际请求的生成单位，不得因计费单位为 `call` 而误用字符数或时长。

用户积分账和供应商成本账可以采用不同结算口径，但差异必须是明确的产品决策，不能由参数转换意外产生。

## 13. 校验与错误

能够在本地确定的错误必须在创建任务和调用供应商前拒绝：

- 模型不存在或已禁用；
- 模型不支持所选模式；
- 网关推断出的调用形态不受目标渠道或供应商支持；
- 分辨率、比例或专用参数未配置；
- `width`、`height` 缺少一项或与比例明显冲突；
- `auto` 比例与固定尺寸同时出现；
- 素材数量超限；
- 单条或合计素材时长超限；
- 缺少模式所需的首帧、关键帧或源视频；
- 图片参考被错误放入首帧字段；
- `catalog_id` 与执行模型不一致。

错误响应应该包含稳定错误码和可读信息，不应依赖解析供应商英文错误文本来识别业务错误。

## 14. 兼容与弃用

以下字段或值属于兼容输入，不得由新代码继续产生：

| 旧表示 | 规范表示 |
|---|---|
| `adaptive` | `auto` |
| `duration: -1` | `duration: "auto"` |
| `seconds` | `duration` |
| `size` | `width` + `height` |
| 顶层 `aspect_ratio` | `metadata.ratio` |
| `image_url` / `first_frame_image` | 顶层 `image` |
| `end_image_url` | `metadata.last_frame_image` |
| `image_urls` | 图片接口顶层 `image` 或视频 `metadata.reference_images` |
| `video_urls` | `metadata.reference_videos` |
| `audio_urls` | `metadata.reference_audios` |

兼容规则：

- 兼容转换只能存在于明确的边界函数中。
- 规范化完成后，发送给网关的最终请求不得包含新旧字段的重复副本。
- 旧 backend 名称可以映射到 `newapi_*`，但不应继续维护新的供应商直连实现。
- 删除兼容输入前必须先确认历史项目、队列消息和外部 API 调用不再依赖。

## 15. 新模型接入检查清单

新增图片或视频模型时，至少完成以下检查：

1. 在模型目录创建稳定 `catalog_id`。
2. 配置正确的 `gateway_model` 和媒体类型。
3. 声明全部支持模式，不从模型名称猜测能力。
4. 配置分辨率、比例、输出时长及素材限制。
5. 仅通过声明式参数开放模型专用选项。
6. 在网关实现供应商适配，不在 DramaClaw 增加供应商专属请求分支。
7. 验证固定比例请求同时保留 `ratio`、`resolution` 和匹配的宽高。
8. 验证自动比例请求不包含 `width`、`height`。
9. 验证自动时长仅发送 `duration: "auto"`。
10. 验证首帧、尾帧和参考素材进入正确字段。
11. 验证素材数量和时长在前端提示、后端校验和网关约束三层一致。
12. 验证报价、预扣和执行使用同一个模型及同一组规范化参数。
13. 为每个支持模式增加至少一条请求契约测试。
14. 为非法字段组合增加拒绝测试。
15. 在 PR 中明确迁移、配置、供应商和计费影响；没有影响也应明确写明。

## 16. 契约测试最低要求

公共契约测试应至少覆盖：

- 图片固定比例与自动比例；
- 图片单图编辑和多图参考；
- 视频文生、图生、首帧、首尾帧、图片参考、全能参考和视频编辑；
- 单张参考图、多张参考图、视频/音频参考及自动时长视频编辑的调用形态推断；
- 单图图片参考和单图全能参考归一化为图生视频；
- `auto` 与 `adaptive` 的兼容归一化；
- 大小写分辨率归一化；
- 固定比例与宽高一致性；
- 自动比例与固定尺寸互斥；
- 固定时长与自动时长；
- 可选布尔参数显式 `true`、显式 `false` 和省略三种情况；
- 模型目录不支持模式时的拒绝；
- 素材数量与时长边界；
- 旧字段进入后最终只产生规范字段；
- 报价参数与执行请求参数一致。
- 视频提交与查询只返回规范任务状态；
- 单结果和多结果都使用 `results` 数组；
- 错误响应包含稳定 `code`、`retryable` 和可选的上游 request ID。

任何公共协议字段的新增、删除或语义变更，都必须先更新本文档和契约测试，再修改供应商适配器。
