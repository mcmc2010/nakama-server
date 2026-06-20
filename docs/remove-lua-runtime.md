# 移除 Lua 运行时

日期: 2026-06-20

基于上游 `heroiclabs/nakama` v3.39.0，在 `main` 分支上移除 Lua 脚本运行时支持，仅保留 Go 和 JavaScript/TypeScript 运行时。

## 删除的文件

### server/runtime_lua*.go（10个文件）

| 文件 | 说明 |
|------|------|
| `server/runtime_lua.go` | Lua 运行时主引擎，VM 池管理 |
| `server/runtime_lua_nakama.go` | `nk` 模块，300+ Lua 绑定函数 |
| `server/runtime_lua_match_core.go` | Lua 权威匹配处理器 |
| `server/runtime_lua_context.go` | Lua 上下文表构建 |
| `server/runtime_lua_loadlib.go` | 自定义 Lua `require` 加载器 |
| `server/runtime_lua_oslib.go` | 自定义 Lua `os` 库 |
| `server/runtime_lua_bit32.go` | Lua `bit32` 库 |
| `server/runtime_lua_localcache.go` | Lua 本地缓存 |
| `server/runtime_lua_utils.go` | Lua strftime 工具 |
| `server/runtime_lua_logger.go` | Lua 日志 |

### internal/gopher-lua/

完整的 gopher-lua（Lua 5.1 VM）fork，约 40+ Go 源文件及测试。

### data/modules/*.lua

9 个 Lua 示例模块：clientrpc、debug_utils、iap_verifier、match、match_init、p2prelayer、runonce_check、tournament 等。

### server/runtime_test.go

Lua 专用运行时测试文件（1056 行）。

## 修改的文件

### server/config.go

- 移除 `RuntimeConfig` 结构体中的 Lua 字段：
  - `LuaMinCount`, `LuaMaxCount`, `LuaCallStackSize`, `LuaRegistrySize`
  - `LuaReadOnlyGlobals`, `LuaApiStacktrace`
  - `MinCount`, `MaxCount`, `CallStackSize`, `RegistrySize`, `ReadOnlyGlobals`（向后兼容别名）
- 移除 5 个 `GetLua*()` getter 方法
- 移除 `ValidateConfig` 中的 Lua 配置验证（5 项）
- 移除废弃参数警告（5 项）
- 更新 `NewRuntimeConfig()` 默认值

### server/runtime.go

- 移除 `RuntimeInfo` 结构体中的 `LuaRpcFunctions` 和 `LuaModules` 字段
- 移除 `CheckRuntimeProviderLua()` 调用
- 移除 `NewRuntimeProviderLua()` 初始化及所有返回值解构
- 移除 `allModules`、`allRPCFunctions`、`allBeforeRtFunctions`、`allAfterRtFunctions` 中的 Lua 合并逻辑
- 移除所有 Lua Before/After Req 函数注册块（约 650 行）
- 移除 matchmaker matched、tournament、leaderboard reset、shutdown、purchase/subscription notification 等 switch 中的 Lua case
- 移除 storage index filter 中的 Lua 部分
- 更新 `runtimeInfo()` 函数签名和实现

### server/metrics.go

- 移除 `Metrics` 接口中的 `GaugeLuaRuntimes` 方法
- 移除 `LocalMetrics.GaugeLuaRuntimes()` 实现
- 更新 `GaugeRuntimes()` 的 Prometheus 指标名从 `lua_runtimes` 改为 `runtimes`

### main.go

- 更新 `--runtime.path` 标志帮助文本，移除 "Lua" 字样

### .golangci.yml

- 移除 `internal/gopher-lua` 排除路径

## 编译验证

```
go build -trimpath -mod=vendor
```

编译通过，无错误。

## 保留的运行时

- **Go** - 原生 Go 插件运行时
- **JavaScript/TypeScript** - goja (JS VM) 运行时
