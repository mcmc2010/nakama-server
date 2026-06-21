# Changelog

## Unreleased

- 移除 Lua 脚本运行时支持，仅保留 Go 和 JavaScript/TypeScript 运行时
- 移除 gopher-lua (Lua 5.1 VM) 内嵌库
- 移除 Lua 相关配置项：lua_min_count, lua_max_count, lua_call_stack_size, lua_registry_size, lua_read_only_globals, lua_api_stacktrace
- Prometheus 指标 `lua_runtimes` 改为 `runtimes`
- 移除 sample_go_module 示例代码
- 移除全部单元测试文件
- 移除 docker-compose-tests.yml
- 移除 Satori Analytics 集成（internal/satori/、server/console_satori.go 及所有运行时绑定）
- 移除 PostgreSQL 兼容支持，仅保留 CockroachDB
- 删除 docker-compose-postgres.yml
- 移除 IAP 应用内购买和订阅功能（iap/、purchase/subscription API、运行时绑定、配置）
- 移除 Segment.io 遥测模块（se/），禁用匿名数据上报
- 移除第三方登录（Apple、Google、Facebook、Game Center、Steam），仅保留 Email、Device、Custom
- 精简 common/ 目录，移除 .git、文档、proto 源文件、vendor 等非必要文件
