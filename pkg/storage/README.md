# Storage 层

港股 IPO 数据的持久化统一通过本目录实现。

## 实现

- **gormstore**：唯一在用实现。基于 GORM，依赖 `hk_ipo/orm` 的全局 DB，将 `collector.StockDetail` 写入/更新到 SQLite。
- **gormmodel**：GORM 表模型，供 gormstore 与 web 查询使用。

写入入口：`gormstore.UpsertStockDetail(ctx, detail)`。  
读取目前由 `cmd/web` 直接使用 `orm.DB` + `gormmodel` 查询；若后续抽象为“通过 storage 读”，建议在本层增加 List/GetByCode 等接口，避免上层直接依赖 orm。

## 约定

- 新增/修改表结构时需同步更新 `gormmodel/models.go`（`orm.Init` 会自动执行 `AutoMigrate`）。
- 不要在此目录下新增另一套“裸 sql”或其它 ORM 的写库实现，保持单一存储实现。
