package collectorapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"hk_ipo/orm"
	"hk_ipo/pkg/collector"
	"hk_ipo/pkg/storage/gormstore"
)

func Run(mode, symbol string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	symbol = strings.TrimSpace(symbol)
	if mode == "" {
		mode = "detail"
	}
	ctx := context.Background()
	switch mode {
	case "list":
		ctxList, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return runList(ctxList)
	case "detail":
		if symbol == "" {
			symbol = "00100"
		}
		ctxDetail, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return runDetail(ctxDetail, symbol)
	case "write-db":
		if err := orm.Init(); err != nil {
			return fmt.Errorf("db init error: %w", err)
		}
		return SyncToDB(ctx, symbol)
	default:
		return fmt.Errorf("unknown mode: %s (use list | detail | write-db)", mode)
	}
}

func matchesSymbol(got, want string) bool {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == "" || want == "" {
		return false
	}
	if got == want {
		return true
	}
	return strings.TrimLeft(got, "0") == strings.TrimLeft(want, "0")
}

func fetchDetail(ctx context.Context, symbol string) (collector.StockDetail, error) {
	client := collector.NewStockDetailClient(nil)

	// 先按原样请求（满足“模拟参数是 00100”），如果失败且带前导 0，再尝试去掉前导 0 兼容部分数据源习惯。
	detail, err := client.FetchStockDetail(ctx, symbol)
	if err != nil && strings.TrimLeft(symbol, "0") != "" && strings.HasPrefix(symbol, "0") {
		detail, err = client.FetchStockDetail(ctx, strings.TrimLeft(symbol, "0"))
	}
	return detail, err
}

func runDetail(ctx context.Context, symbol string) error {
	detail, err := fetchDetail(ctx, symbol)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal detail: %w", err)
	}
	fmt.Println(string(b))
	return nil
}

// 单只股票任务超时（拉详情+入库），超时则跳过该只继续下一只。
const perStockTimeout = 3 * time.Minute

// SyncToDB 从列表接口获取股票列表，整合详情接口后写入数据库；每只股票独立超时，超时仅跳过该只。
func SyncToDB(ctx context.Context, symbolFilter string) error {
	listClient := collector.NewStockListClient(nil)
	listResp, err := listClient.FetchStockList(ctx)
	if err != nil {
		return fmt.Errorf("fetch stock list: %w", err)
	}

	detailClient := collector.NewStockDetailClient(nil)

	for _, row := range listResp.Rows {
		if ctx.Err() != nil {
			break
		}
		item := row.Item
		symbol := collector.FirstOfCell(item, "stock_cd", "stockCd", "symbol", "code", "stock_code")
		if symbol == "" {
			continue
		}
		if symbolFilter != "" && !matchesSymbol(symbol, symbolFilter) {
			continue
		}
		stockCtx, cancel := context.WithTimeout(context.Background(), perStockTimeout)
		detail, err := detailClient.FetchStockDetail(stockCtx, symbol)
		if err != nil && strings.TrimLeft(symbol, "0") != "" && strings.HasPrefix(symbol, "0") {
			detail, err = detailClient.FetchStockDetail(stockCtx, strings.TrimLeft(symbol, "0"))
		}
		if err != nil {
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				fmt.Fprintf(os.Stderr, "skip %s: fetch timeout\n", symbol)
				continue
			}
			return fmt.Errorf("fetch detail %s: %w", symbol, err)
		}

		collector.MergeListIntoDetail(&detail, item)

		if err := gormstore.UpsertStockDetail(stockCtx, detail); err != nil {
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				fmt.Fprintf(os.Stderr, "skip %s: upsert timeout\n", symbol)
				continue
			}
			return fmt.Errorf("upsert %s: %w", symbol, err)
		}
		cancel()
		fmt.Printf("ok: wrote stockCode=%s\n", detail.StockCode)
	}
	return nil
}

// diagnoseGreyAndFirstDay 在 merge 前打印：列表 cell 的 key、列表里取到的暗盘/首日值、详情当前值。
func diagnoseGreyAndFirstDay(w io.Writer, symbol string, item collector.StockListItem, d *collector.StockDetail) {
	if w == nil || item == nil || d == nil {
		return
	}
	keys := make([]string, 0, len(item))
	for k := range item {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "[diagnose %s] list cell keys (%d): %s\n", symbol, len(keys), strings.Join(keys, ", "))
	greyDateFromList := collector.FirstOfCell(item, "gray_dt", "grey_dt", "grey_date", "gray_date", "暗盘日期", "暗盤日期", "暗盘", "暗盤", "灰度日期", "灰度")
	firstDayFromList := collector.FirstOfCell(item, "first_incr_rt", "jsl_first_incr_rt", "first_day_incr_rate_pct", "firstDayIncrRatePct", "首日升幅", "首日涨幅", "首日表现", "首日表現", "首日回报", "首日回報")
	totalFromList := collector.FirstOfCell(item, "total_incr_rt", "total_incr_rate_pct", "totalIncrRatePct", "累计升幅", "累計升幅")
	fmt.Fprintf(w, "[diagnose %s] from list: grey_date=%q first_day=%q total=%q\n", symbol, greyDateFromList, firstDayFromList, totalFromList)
	fmt.Fprintf(w, "[diagnose %s] detail before merge: grey_date=%q grey_incr=%.4f grey_incr2=%.4f first_day=%.4f total=%.4f\n",
		symbol, d.GreyMarket.Date, d.GreyMarket.IncrRatePct, d.GreyMarket.IncrRatePct2, d.Performance.FirstDayIncrRatePct, d.Performance.TotalIncrRatePct)
}

func diagnoseGreyAndFirstDayAfter(w io.Writer, symbol string, d *collector.StockDetail) {
	if w == nil || d == nil {
		return
	}
	fmt.Fprintf(w, "[diagnose %s] detail after merge:  grey_date=%q grey_incr=%.4f grey_incr2=%.4f first_day=%.4f total=%.4f\n",
		symbol, d.GreyMarket.Date, d.GreyMarket.IncrRatePct, d.GreyMarket.IncrRatePct2, d.Performance.FirstDayIncrRatePct, d.Performance.TotalIncrRatePct)
}

// runList 直接返回 Jisilu 列表接口的完整 JSON（page + rows，每行 cell 为完整对象）。
func runList(ctx context.Context) error {
	client := collector.NewStockListClient(nil)
	resp, err := client.FetchStockList(ctx)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal list: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
