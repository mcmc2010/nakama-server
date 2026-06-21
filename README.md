# Nakama Server (Custom)

基于 [Heroic Labs Nakama](https://github.com/heroiclabs/nakama) v3.39.0 定制的游戏服务器。

## 与上游的区别

| 功能 | 上游 | 本定制版 |
|------|------|---------|
| Lua 运行时 | 支持 | 已移除 |
| Go 运行时 | 支持 | 支持 |
| JavaScript/TypeScript 运行时 | 支持 | 支持 |
| 第三方登录 | Apple/Google/Facebook/Steam/Game Center | 已移除 |
| 账号登录 | Email/Device/Custom + 第三方 | Email/Device/Custom |
| IAP 购买验证 | Apple/Google/Huawei/Facebook | 已移除 |
| Satori Analytics | 支持 | 已移除 |
| 数据库 | CockroachDB + PostgreSQL | 仅 CockroachDB |
| 遥测 | Segment.io 匿名上报 | 已移除 |
| nakama-common | 外部依赖 | 已内化到 `common/` |
| sql-migrate | 外部依赖 | 已内化到 `internal/sql-migrate/` |

## 功能

- **用户** — 邮箱、设备 ID、自定义 Token 注册/登录
- **存储** — 在集合中存储用户记录、设置和其他对象
- **社交** — 好友系统、群组、社交图谱
- **聊天** — 1对1、群组、全局聊天，持久化消息历史
- **多人游戏** — 实时或回合制多人游戏
- **排行榜** — 动态、赛季、排行榜
- **锦标赛** — 玩家竞赛、联赛
- **队伍** — 组队玩法、队伍内通信
- **通知** — 向连接的客户端发送消息和通知
- **运行时代码** — 使用 Go 或 TypeScript/JavaScript 扩展服务器逻辑
- **匹配器**、**控制台**、**指标** 等

## 快速开始

### Docker

```shell
docker-compose up
```

### 从源码构建

```shell
go build -trimpath -mod=vendor
./nakama migrate up --database.address "root@127.0.0.1:26257"
./nakama --database.address "root@127.0.0.1:26257"
```

## 运行时

### Go

将 Go 插件编译为 `.so` 文件，放在 `data/modules/` 目录下。

### JavaScript/TypeScript

将 TypeScript 编译为 JavaScript bundle，通过 `--runtime.js_entrypoint` 指定入口文件。

## 目录结构

```
├── common/          # nakama-common（API 定义、运行时接口）
├── server/          # 核心服务器代码
├── console/         # 管理后台 UI（protobuf 定义）
├── apigrpc/         # gRPC API 定义
├── internal/        # 内部库
│   ├── cronexpr/    # Cron 表达式解析
│   ├── ctxkeys/     # 上下文键
│   ├── skiplist/    # 跳表数据结构
│   └── sql-migrate/ # 数据库迁移工具
├── migrate/         # 数据库迁移脚本
├── social/          # 社交登录（已精简为 stub）
├── flags/           # CLI 标志定义
├── data/            # 数据目录（模块、配置）
├── vendor/          # 第三方依赖
├── main.go          # 入口
└── docker-compose.yml
```

## License

[Apache-2 License](LICENSE)
