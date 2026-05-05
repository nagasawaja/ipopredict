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
- `HK_IPO_AUTO_SYNC_ON_START`：Web 启动后是否后台同步一次（`true/false`，默认 `false`）
- `HK_IPO_AUTO_SYNC_INTERVAL`：Web 后台自动同步间隔，如 `24h`；为空则不定时同步
- `HK_IPO_AUTO_SYNC_TIMEOUT`：单次后台同步超时，如 `30m`（默认 `30m`）
- `HK_IPO_AUTO_SYNC_SYMBOL`：只同步单只股票代码；为空则同步全量

> 首次运行会自动建表（GORM AutoMigrate）。

## Docker

```bash
docker compose up --build -d
```

服务地址：<http://localhost:8083>

Compose 默认挂载 `./sql` 到容器内 `/app/sql`，使用 `./sql/hk_ipo.db`，并开启启动后同步一次及每 12 小时自动同步。

## 目录说明

- `cmd/hkipo`：统一 CLI 入口（推荐）
- `cmd/web`：Web 启动器（薄封装）
- `cmd/collector`：采集启动器（薄封装）
- `pkg/app/webapp`：Web 应用逻辑
- `pkg/app/collectorapp`：采集应用逻辑
- `tools/*`：实验/调试入口（非生产启动）

## 文档

- [数据来源与基础格式说明](./docs/data_source_readme.md)
