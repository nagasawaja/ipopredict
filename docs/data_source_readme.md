# 数据来源与基础格式说明

本文档描述当前项目正在使用的数据来源、标准化格式、入库结构和 Web 输出口径。内容以当前代码实现为准。

## 1. 当前数据来源

### 1.1 列表数据源（Jisilu）

- 来源：`https://www.jisilu.cn`
- 接口：`GET /data/new_stock/hkipo/?___jsl=LST___t=<timestamp>`
- 在代码中的入口：
  - `pkg/collector/stock_list.go`
  - `collector.NewStockListClient().FetchStockList(...)`

返回结构特点：

- 顶层可能是 `rows` / `data` / `list` / `result`。
- 每行有效数据可能在 `row.cell` / `row.data`，也可能直接在整行对象里。
- 项目把每行 `cell` 保留为 `map[string]interface{}`（不预先删字段）。

常见列表 key（示例）：

- `stock_cd`、`stock_nm`
- `issue_price`、`price_range`
- `list_dt2`、`apply_dt2`、`apply_end_dt2`
- `gray_dt`、`gray_incr_rt`、`gray_incr_rt2`
- `market`

### 1.2 详情数据源（Chiefgroup）

- 来源：`https://www.chiefgroup.com.hk`
- 页面：`GET /hk/securities/hk-ipo-detail/dp?symbol=<symbol>`
- 在代码中的入口：
  - `pkg/collector/stock_detail.go`
  - `collector.NewStockDetailClient().FetchStockDetail(...)`

实现方式：

- 详情是 HTML 页面，不是公开 JSON API。
- 通过 `goquery` 解析页面分区（发行资料、公司概况、用途、配售结果、管理层等）。
- 解析后映射为统一结构 `collector.StockDetail`。

## 2. 采集与合并流程

同步入口：

- `go run ./cmd/hkipo sync`
- `go run ./cmd/hkipo sync -symbol 00100`

流程（当前实现）：

1. 拉 Jisilu 列表。
2. 逐只按股票代码拉 Chiefgroup 详情。
3. 详情优先，列表补全：
   - 在 `pkg/collector/merge.go` 中执行 `MergeListIntoDetail`。
   - 仅当详情字段为空/零值时，用列表字段补。
4. 列表完整 `cell` 会作为 `RawSection(sectionId=list)` 一并入库，避免字段丢失。
5. 最终通过 `gormstore.UpsertStockDetail(...)` 写入 SQLite。

补充规则：

- 对带前导零代码会自动兼容重试，例如 `00100` 失败时尝试 `100`。
- 单只股票有独立超时控制（避免全量同步被单个异常卡死）。

## 3. 标准化数据格式（内部）

统一对象：`collector.StockDetail`（`pkg/collector/stock_detail.go`）。

字段约定：

- 日期：`YYYY-MM-DD` 字符串（如 `listDate`、`applyStartDate`）。
- 时间戳：`source.fetchedAt` 为 Unix 秒级时间戳。
- 比例：`26.79` 代表 `26.79%`（不带 `%`）。
- 金额：港元数值（`float64`）。
- 股数：整数（`int`/`int64`）。

注意：

- `raise_money` 纯数字且无单位时，当前逻辑按“亿港元”解释。
- 无法稳定解析的字段会尽量保留到 `rawSections`，用于后续补解析。

## 4. SQLite 入库格式

默认库路径：

- `./sql/hk_ipo.db`
- 可通过 `HK_IPO_DB_PATH` 覆盖。

核心表（主干）：

- `stocks`：股票主表 + source 元信息（`source_provider`、`source_url`、`source_fetched_at`）。
- `stock_offerings`：发行信息（价格、日期、股数、市值、入场费等）。
- `stock_companies`：公司概况。
- `stock_greenshoe`：绿鞋。
- `stock_grey_market`：暗盘日期和涨幅。
- `stock_performance`：首日/累计表现。
- `stock_fundraising`：募资金额及原文。
- `stock_allotment_summary`、`stock_allotment_tiers`：配售摘要与分档。

关联/数组表：

- `intermediaries` + `stock_intermediaries`（保荐人/包销商/账簿管理人/全球协调人）。
- `stock_secretaries`
- `stock_major_shareholders`
- `stock_use_of_proceeds`
- `stock_management`

原始保留表（兜底）：

- `stock_raw_sections`
- `stock_raw_items`

常见 `section_id`：

- `list`（Jisilu 列表原始 cell）
- `issuance`
- `company`
- `purpose`
- `allotmentSummary`

## 5. Web 层输出口径（当前）

列表接口：`POST /api/stocks`

- `listing_mechanism`：优先从 raw `list.market` 提取。
- `grey_date`：按以下顺序回填：
  1. raw `list.gray_dt/grey_dt/...`
  2. `stock_grey_market.grey_date`
  3. `stock_offerings.list_date - 1 day`（推导值）

详情页：`/stocks/{stock_code}`

- 主体来自标准化表。
- 同时展示 `RawSections` 便于排查解析缺失。

## 6. 00100 实际样例（当前库）

来自 `sql/hk_ipo.db` 当前数据：

```text
stocks:
stock_code=00100
source_provider=chiefgroup
source_request_symbol=00100
source_url=https://www.chiefgroup.com.hk/hk/securities/hk-ipo-detail/dp?symbol=00100

stock_offerings:
offer_price_low=151
offer_price_high=165
offer_price=165
lot_size=20
global_offer_shares=29197600
public_offer_shares=5077900
international_offer_shares=24119699
apply_start_date=2025-12-31
apply_end_date=2026-01-06
list_date=2026-01-09
admission_fee_hkd=3333.28
market_cap_hkd=51027000000

stock_grey_market:
grey_date=NULL
incr_rate_pct=26.79
incr_rate_pct2=24.61

raw(list):
market=主板
gray_dt=
gray_incr_rt=26.79
gray_incr_rt2=24.61
```

## 7. 快速自查命令

```bash
# 看 source 信息
sqlite3 sql/hk_ipo.db "SELECT stock_code,source_provider,source_url FROM stocks WHERE stock_code='00100';"

# 看 list 原始 key/value
sqlite3 sql/hk_ipo.db "SELECT ri.label,ri.value FROM stock_raw_sections rs JOIN stock_raw_items ri ON ri.raw_section_id=rs.id WHERE rs.stock_id=(SELECT id FROM stocks WHERE stock_code='00100') AND rs.section_id='list' ORDER BY ri.seq;"

# 看发行核心字段
sqlite3 sql/hk_ipo.db "SELECT offer_price_low,offer_price_high,offer_price,list_date,market_cap_hkd FROM stock_offerings WHERE stock_id=(SELECT id FROM stocks WHERE stock_code='00100');"
```
