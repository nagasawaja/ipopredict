# hk_ipo

港股 IPO 数据采集、入库、展示与预测项目（Go + SQLite）。

## 统一入口（推荐）

```bash
# 启动 Web
go run ./cmd/hkipo web -addr :8083

# 拉列表(JSON)
go run ./cmd/hkipo list

# 拉单只详情(JSON)
go run ./cmd/hkipo detail -symbol 00100

# 同步入库（全量）
go run ./cmd/hkipo sync

# 同步入库（单只）
go run ./cmd/hkipo sync -symbol 00100
```

## 兼容入口（仍可用）

```bash
go run ./cmd/web -addr :8083
go run ./cmd/collector -mode list
go run ./cmd/collector -mode detail -symbol 00100
go run ./cmd/collector -mode write-db -symbol 00100
```

## 数据库配置

- `HK_IPO_DB_DSN`：完整 sqlite DSN（优先）
- `HK_IPO_DB_PATH`：sqlite 文件路径（默认 `./sql/hk_ipo.db`）

> 首次运行会自动建表（GORM AutoMigrate）。

## 目录说明

- `cmd/hkipo`：统一 CLI 入口（推荐）
- `cmd/web`：Web 启动器（薄封装）
- `cmd/collector`：采集启动器（薄封装）
- `pkg/app/webapp`：Web 应用逻辑
- `pkg/app/collectorapp`：采集应用逻辑
- `tools/*`：实验/调试入口（非生产启动）

## 文档

- [数据来源与基础格式说明](./docs/data_source_readme.md)
