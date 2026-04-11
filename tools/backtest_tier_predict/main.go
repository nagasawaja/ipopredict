package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"hk_ipo/pkg/ipo_predict"
)

type stockRow struct {
	ID           int64
	Code         string
	Name         string
	ListDate     time.Time
	PublicShares int64
	LotSize      int
	Price        float64
	SubMultiple  float64
}

type tierRow struct {
	Seq       int
	GroupCode string
	Lots      int64
	ActualPct float64
}

type compareRow struct {
	StockCode string
	StockName string
	ListDate  time.Time
	Seq       int
	GroupCode string
	Lots      int64
	ActualPct float64
	PredPct   float64
	AbsErr    float64
}

func main() {
	dbPath := flag.String("db", "sql/hk_ipo.db", "sqlite db path")
	latest := flag.Int("latest", 15, "latest N stocks by list_date")
	detail := flag.Bool("detail", true, "print each tier compare rows")
	flag.Parse()

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	stocks, err := loadLatestStocks(db, *latest)
	if err != nil {
		log.Fatal(err)
	}
	if len(stocks) == 0 {
		fmt.Println("no stocks")
		return
	}

	rows := make([]compareRow, 0, 1024)
	sumAbs := 0.0
	for _, s := range stocks {
		tiers, err := loadTiers(db, s.ID)
		if err != nil {
			log.Fatal(err)
		}
		if len(tiers) == 0 {
			continue
		}

		buckets := make([]ipo_predict.Tier, 0, len(tiers))
		for _, t := range tiers {
			buckets = append(buckets, ipo_predict.Tier{
				Lots:      t.Lots,
				AmountHKD: float64(t.Lots) * s.Price,
			})
		}

		req := ipo_predict.MarginRequest{
			PublicShares:    s.PublicShares,
			LotSize:         s.LotSize,
			Price:           s.Price,
			BrokerMarginSum: inferBrokerMarginSum(float64(s.PublicShares)*s.Price, s.SubMultiple),
			Buckets:         buckets,
		}
		pred, err := ipo_predict.Predict(req)
		if err != nil {
			log.Printf("predict failed %s: %v", s.Code, err)
			continue
		}

		predByLots := make(map[int64]ipo_predict.WinRateInfo, len(pred.WinRates))
		for _, p := range pred.WinRates {
			predByLots[p.Lots] = p
		}
		for _, t := range tiers {
			p := predByLots[t.Lots]
			predPct := p.PerLotRate * 100.0
			absErr := math.Abs(predPct - t.ActualPct)
			rows = append(rows, compareRow{
				StockCode: s.Code,
				StockName: s.Name,
				ListDate:  s.ListDate,
				Seq:       t.Seq,
				GroupCode: t.GroupCode,
				Lots:      t.Lots,
				ActualPct: t.ActualPct,
				PredPct:   predPct,
				AbsErr:    absErr,
			})
			sumAbs += absErr
		}
	}

	if len(rows) == 0 {
		fmt.Println("no tier rows")
		return
	}
	mae := sumAbs / float64(len(rows))
	fmt.Printf("latest=%d stocks=%d tier_samples=%d tier_mae_pct=%.4f\n", *latest, len(stocks), len(rows), mae)

	byStock := make(map[string][]compareRow, len(stocks))
	for _, r := range rows {
		byStock[r.StockCode] = append(byStock[r.StockCode], r)
	}
	ordered := make([]stockRow, len(stocks))
	copy(ordered, stocks)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ListDate.After(ordered[j].ListDate) })

	for _, s := range ordered {
		list := byStock[s.Code]
		if len(list) == 0 {
			continue
		}
		stockSum := 0.0
		for _, r := range list {
			stockSum += r.AbsErr
		}
		fmt.Printf("\n%s %s list=%s tiers=%d stock_mae=%.4f\n",
			s.Code, s.Name, s.ListDate.Format("2006-01-02"), len(list), stockSum/float64(len(list)))
		if !*detail {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Seq < list[j].Seq })
		for _, r := range list {
			fmt.Printf("  seq=%02d grp=%s lots=%d actual=%.4f pred=%.4f abs=%.4f\n",
				r.Seq, r.GroupCode, r.Lots, r.ActualPct, r.PredPct, r.AbsErr)
		}
	}
}

func loadLatestStocks(db *sql.DB, latest int) ([]stockRow, error) {
	if latest <= 0 {
		latest = 15
	}
	const q = `
SELECT s.id,s.stock_code,s.stock_name,o.list_date,o.public_offer_shares,o.lot_size,
       COALESCE(NULLIF(o.offer_price,0),o.offer_price_high) AS price,
       a.subscription_multiple
FROM stocks s
JOIN stock_offerings o ON o.stock_id=s.id
JOIN stock_allotment_summary a ON a.stock_id=s.id
WHERE a.subscription_multiple>0
  AND o.list_date IS NOT NULL
  AND EXISTS (SELECT 1 FROM stock_allotment_tiers t WHERE t.stock_id=s.id)
ORDER BY datetime(o.list_date) DESC
LIMIT ?`
	rs, err := db.Query(q, latest)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	out := make([]stockRow, 0, latest)
	for rs.Next() {
		var r stockRow
		if err := rs.Scan(
			&r.ID, &r.Code, &r.Name, &r.ListDate, &r.PublicShares, &r.LotSize, &r.Price, &r.SubMultiple,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func loadTiers(db *sql.DB, stockID int64) ([]tierRow, error) {
	const q = `
SELECT seq,group_code,lots,win_rate_pct
FROM stock_allotment_tiers
WHERE stock_id=?
ORDER BY seq`
	rs, err := db.Query(q, stockID)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	out := make([]tierRow, 0, 64)
	for rs.Next() {
		var r tierRow
		if err := rs.Scan(&r.Seq, &r.GroupCode, &r.Lots, &r.ActualPct); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func inferBrokerMarginSum(fundraisingAmt, realOversub float64) float64 {
	if fundraisingAmt <= 0 || realOversub <= 0 {
		return 0
	}
	approxOversub := realOversub
	switch {
	case realOversub > 200:
		approxOversub = realOversub * 0.98
	case realOversub > 30:
		approxOversub = realOversub * 0.50
	default:
		approxOversub = realOversub * 0.35
	}
	return fundraisingAmt * approxOversub
}
