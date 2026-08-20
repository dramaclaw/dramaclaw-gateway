# dramaclaw-gateway

简体中文 | [English](./README.md)

`dramaclaw-gateway` 是 [DramaClaw](https://github.com/dramaclaw/dramaclaw)
配套的开源模型网关。它接收 DramaClaw 使用的稳定 DC-Media 请求，统一媒体语义，并将
请求转换成不同供应商的图片和视频接口协议。

```text
DramaClaw
    -> DC-Media 标准请求
    -> dramaclaw-gateway 标准化
    -> 渠道适配器
    -> 供应商 API
```

本项目基于 [New API](https://github.com/QuantumNous/new-api) 修改开发，并持续复用其
网关基础设施和渠道适配能力。本仓库的媒体接口以 DC-Media 语义为准，不承诺兼容原
New API 的历史媒体任务请求和响应结构。

## 项目边界

网关负责：

- 接收 DramaClaw 使用的图片与异步视频公共协议；
- 保留首帧、尾帧、参考图、参考视频和参考音频的素材角色；
- 在请求供应商之前拒绝不支持的参数组合；
- 将标准请求转换为供应商请求；
- 将供应商任务 ID、状态、错误和结果地址转换回公共协议；
- 通过 `GET /api/channel/types` 暴露当前实际注册的渠道元数据。

DramaClaw 负责用户可见的模型目录和工作流控制。渠道适配器负责供应商鉴权、接口地址、
请求结构、能力限制和响应转换。网关不能依赖供应商模型名称推断 DramaClaw 的业务模式。

商业虾驿中的计费、调用审计、结果归档和运营能力不属于本仓库目标。

## DC-Media 协议

协议以根目录的 [`dc-media-protocol.md`](./dc-media-protocol.md) 为准。开发适配器前请从
文档导航开始：

- [DC-Media 文档导航](./docs/dc-media/README.md)
- [适配器开发指南](./docs/dc-media/adapter-development.md)
- [示例供应商适配器](./docs/dc-media/example-adapter.md)
- [模型接入检查清单](./docs/dc-media/model-onboarding-checklist.md)
- [渠道支持矩阵](./docs/providers/README.md)

## 本地开发

环境要求：

- [`go.mod`](./go.mod) 声明的 Go 版本；
- 前端使用 Bun `1.3.14`；
- Docker 和 Docker Compose，用于开发数据库及 Redis。

克隆并启动 API 依赖：

```bash
git clone https://github.com/dramaclaw/dramaclaw-gateway.git
cd dramaclaw-gateway
make dev-api
```

在第二个终端启动前端：

```bash
make dev-web
```

访问 `http://localhost:5173`。前端开发服务器会把 API 请求代理到 `3000` 端口。

修改 Go 代码后重建 API 容器：

```bash
make dev-api-rebuild
```

根目录 `docker-compose.yml` 可能继续跟踪上游部署默认值。社区开发应通过上述 Make 命令
使用 `docker-compose.dev.yml`，确保运行的是当前工作区源码。

## 贡献渠道适配器

开始编码前：

1. 创建“渠道适配申请”Issue，提供官方文档、鉴权方式、模型能力、限制及脱敏后的请求和
   响应示例。
2. 确认是否可以扩展现有 New API 适配器，同时不破坏 DC-Media 语义。
3. 阅读 [`CONTRIBUTING.zh_CN.md`](./CONTRIBUTING.zh_CN.md) 并认领 Issue。
4. 以 DC-Media 请求作为输入标准，供应商字段只能存在于渠道适配器内部。

可以用以下命令生成默认不可调用、但能够通过编译的适配器骨架：

```bash
make new-adapter PROVIDER=example TYPE=64 MODE=task CAPABILITIES=video
```

同步图片、音频或协议适配器使用 `MODE=sync`。命令会生成适配器、测试和中英文供应商文档骨架，
但不会自动修改共享渠道注册表。

单个请求成功不代表适配完成。适配器必须保留素材角色、拒绝不支持的组合、转换异步任务
状态和错误、声明渠道能力，并提供请求转换测试。

常用验证命令：

```bash
go test ./relay/common ./relay/channel/task/<provider>
go test ./... -run '^$'
cd relaykit && GOWORK=off go build ./...
cd ../web && bun install --frozen-lockfile
bun run typecheck
bun run build
```

注册位置、测试要求和完整 Definition of Done 见
[`CONTRIBUTING.zh_CN.md`](./CONTRIBUTING.zh_CN.md)。

## 仓库结构

```text
relay/common/                 DC-Media 规范化和校验
relay/channel/task/<provider> 异步媒体渠道适配器
relay/channel/<provider>      同步渠道适配器
relay/relay_adaptor.go        适配器工厂注册
constant/channel.go           稳定渠道类型编号和默认地址
relay/channel_types.go        可查询渠道元数据
docs/dc-media/                协议实现指南
docs/providers/               渠道支持情况和限制
web/                          渠道管理前端
```

## 安全

禁止提交 API Key、包含本地私有路径的 Workflow、生成媒体、数据库以及可能含凭据的完整
供应商响应。安全漏洞请使用仓库 GitHub Security Advisory 私下报告，不要直接创建公开
Issue。

## 许可证与署名

`dramaclaw-gateway` 使用 [GNU AGPLv3](./LICENSE) 发布。修改发行版及网络部署需要遵守
AGPLv3 的源码提供、法律声明和署名义务。

本仓库基于 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 修改开发，由
DramaClaw 社区独立维护，并非 New API 官方发行版本。仓库保留原始许可证、
[`NOTICE`](./NOTICE)、版权声明和第三方许可证声明。

Frontend design and development by New API contributors.

根据 `NOTICE` 中适用的 AGPLv3 Section 7 附加条款，用户界面必须继续保留指向 New API
原项目的可见链接。
