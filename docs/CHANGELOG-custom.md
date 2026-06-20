# Changelog

## Unreleased

- 移除 Lua 脚本运行时支持，仅保留 Go 和 JavaScript/TypeScript 运行时
- 移除 gopher-lua (Lua 5.1 VM) 内嵌库
- 移除 Lua 相关配置项：lua_min_count, lua_max_count, lua_call_stack_size, lua_registry_size, lua_read_only_globals, lua_api_stacktrace
- Prometheus 指标 `lua_runtimes` 改为 `runtimes`
- 移除 sample_go_module 示例代码
- 移除全部单元测试文件
- 移除 docker-compose-tests.yml
