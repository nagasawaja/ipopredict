# 中文友好命名方案（SQLite/GORM）

本文给出两套方案：
- 方案 A（推荐）：不改物理表名，只加“中文显示名 + 术语对照”，零数据迁移风险。
- 方案 B（重构）：改表名/字段名为更短更直观英文，需迁移脚本。

## 1. 命名问题定位

当前“难懂”主要来自两点：
- 金融术语本身较生僻：`allotment`（配售）、`green_shoe`（绿鞋）、`bookrunner`（账簿管理人）。
- 表名过长且重复：`ipo_stock_xxx` 前缀重复，阅读负担大。

这不是英文能力问题，是领域术语 + 命名风格导致的认知负担。

## 2. 术语速查（建议固定在团队字典）

- `offering`: 发行信息
- `allotment`: 配售信息
- `tier`: 档位
- `intermediary`: 中介机构
- `sponsor`: 保荐人
- `underwriter`: 承销商
- `bookrunner`: 账簿管理人
- `global coordinator`: 全球协调人
- `grey market`: 暗盘
- `green shoe`: 绿鞋（超额配售权）
- `use of proceeds`: 募资用途

## 3. 表级命名方案

### 3.1 方案 A（推荐，兼容现状）

物理表名不变，仅定义“中文显示名”：

| 现表名 | 中文友好名 |
|---|---|
| `ipo_stock` | 股票主表 |
| `ipo_stock_offering` | 发行信息 |
| `ipo_stock_company` | 公司信息 |
| `ipo_stock_green_shoe` | 绿鞋信息 |
| `ipo_stock_grey_market` | 暗盘信息 |
| `ipo_stock_performance` | 上市表现 |
| `ipo_stock_raise_money` | 募资信息 |
| `ipo_intermediary` | 中介机构 |
| `ipo_stock_intermediary` | 股票-中介关系 |
| `ipo_stock_company_secretary` | 公司秘书 |
| `ipo_stock_major_shareholder` | 主要股东 |
| `ipo_stock_use_of_proceeds` | 募资用途 |
| `ipo_stock_management` | 管理层 |
| `ipo_stock_allotment_summary` | 配售摘要 |
| `ipo_stock_allotment_tier` | 配售档位 |
| `ipo_stock_raw_section` | 原始分区（兜底） |
| `ipo_stock_raw_item` | 原始条目（兜底） |

### 3.2 方案 B（重构命名）

如果接受迁移，可改为更短命名：

| 现表名 | 建议新表名 |
|---|---|
| `ipo_stock` | `stocks` |
| `ipo_stock_offering` | `stock_offerings` |
| `ipo_stock_company` | `stock_companies` |
| `ipo_stock_green_shoe` | `stock_greenshoe` |
| `ipo_stock_grey_market` | `stock_grey_market` |
| `ipo_stock_performance` | `stock_performance` |
| `ipo_stock_raise_money` | `stock_fundraising` |
| `ipo_intermediary` | `intermediaries` |
| `ipo_stock_intermediary` | `stock_intermediaries` |
| `ipo_stock_company_secretary` | `stock_secretaries` |
| `ipo_stock_major_shareholder` | `stock_major_shareholders` |
| `ipo_stock_use_of_proceeds` | `stock_use_of_proceeds` |
| `ipo_stock_management` | `stock_management` |
| `ipo_stock_allotment_summary` | `stock_allotment_summary` |
| `ipo_stock_allotment_tier` | `stock_allotment_tiers` |
| `ipo_stock_raw_section` | `stock_raw_sections` |
| `ipo_stock_raw_item` | `stock_raw_items` |

## 4. 字段级中文友好对照（高频）

| 字段 | 中文友好名 |
|---|---|
| `stock_code` | 股票代码 |
| `hk_symbol` | 港股代码 |
| `stock_name` | 股票名称 |
| `offer_price_low` | 招股价下限 |
| `offer_price_high` | 招股价上限 |
| `offer_price` | 发行价 |
| `lot_size` | 每手股数 |
| `public_offer_shares` | 公开发售股数 |
| `admission_fee_hkd` | 入场费(港元) |
| `market_cap_hkd` | 市值(港元) |
| `pe` | 市盈率 |
| `applicants` | 申请人数 |
| `one_lot_win_rate_pct` | 一手中签率(%) |
| `subscription_multiple` | 认购倍数 |
| `first_day_incr_rate_pct` | 首日涨幅(%) |
| `total_incr_rate_pct` | 累计涨幅(%) |
| `clawback_rate_pct` | 回拨比例(%) |
| `grey_date` | 暗盘日期 |
| `incr_rate_pct` | 暗盘涨幅(%) |
| `amount_hkd` | 金额(港元) |
| `amount_text` | 金额文本 |
| `raw_text` | 原始文本 |
| `seq` | 顺序号 |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |

## 5. GORM 结构体命名建议（代码可读性）

不改 DB 表也可改 Go 类型名（通过 `TableName()` 兼容）：

| 现类型 | 建议类型 |
|---|---|
| `StockOffering` | `StockIssueInfo` |
| `StockCompany` | `StockCompanyInfo` |
| `StockRaiseMoney` | `StockFundraising` |
| `StockAllotmentSummary` | `StockPlacementSummary` |
| `StockAllotmentTier` | `StockPlacementTier` |
| `StockIntermediary` | `StockIntermediaryLink` |
| `StockRawSection` | `StockRawBlock` |
| `StockRawItem` | `StockRawField` |

说明：`allotment` 可统一替换成 `placement`（更直观）。

## 6. 落地步骤（建议顺序）

1. 先落地方案 A：在 Web 展示层与文档中全面使用中文友好名。  
2. 增加一个“字段字典”文件（建议 `pkg/storage/gormmodel/labels_zh.go`）供 UI/API 复用。  
3. 观察 1-2 周后，如仍觉得命名重，执行方案 B 的迁移。  
4. 方案 B 必做：迁移脚本 + 回滚脚本 + 数据校验（行数、主键、唯一键、外键抽样）。  

## 7. 结论

优先建议：先做“显示层中文化 + 术语统一”，不要先动物理表名。  
原因：你当前痛点是“理解成本”，不是“数据库性能或结构错误”。
