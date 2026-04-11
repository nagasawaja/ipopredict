package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math"
	"sort"

	_ "github.com/mattn/go-sqlite3"

	"hk_ipo/pkg/ipo_predict"
)

type stockRow struct {
	ID                   int64
	Code                 string
	Name                 string
	PublicShares         int64
	LotSize              int
	OfferPrice           float64
	OfferPriceHigh       float64
	SubscriptionMultiple float64
	OneLotWinRatePct     float64
}

type metric struct {
	Code   string
	Name   string
	Actual float64
	Pred   float64
	AbsErr float64
}

func main() {
	dbPath := flag.String("db", "sql/hk_ipo.db", "sqlite db path")
	topN := flag.Int("top", 20, "top N largest errors")
	flag.Parse()

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := loadStocks(db)
	if err != nil {
		log.Fatal(err)
	}

	all := make([]metric, 0, len(rows))
	sumAbsErr := 0.0
	for _, r := range rows {
		price := r.OfferPrice
		if price <= 0 {
			price = r.OfferPriceHigh
		}
		if r.PublicShares <= 0 || r.LotSize <= 0 || price <= 0 {
			continue
		}

		buckets := buildDefaultBuckets(r.LotSize, price, r.PublicShares)
		if len(buckets) == 0 {
			continue
		}

		fundraisingAmt := float64(r.PublicShares) * price
		req := ipo_predict.MarginRequest{
			PublicShares:    r.PublicShares,
			LotSize:         r.LotSize,
			Price:           price,
			BrokerMarginSum: inferBrokerMarginSumFromSubscriptionMultiple(fundraisingAmt, r.SubscriptionMultiple),
			Buckets:         buckets,
		}

		pred, err := ipo_predict.Predict(req)
		if err != nil {
			log.Printf("predict failed %s: %v", r.Code, err)
			continue
		}
		oneLotPred := findOneLotPerLotRate(pred.WinRates, int64(r.LotSize)) * 100
		if oneLotPred <= 0 {
			continue
		}

		absErr := math.Abs(oneLotPred - r.OneLotWinRatePct)
		all = append(all, metric{
			Code:   r.Code,
			Name:   r.Name,
			Actual: r.OneLotWinRatePct,
			Pred:   oneLotPred,
			AbsErr: absErr,
		})
		sumAbsErr += absErr
	}

	sort.Slice(all, func(i, j int) bool { return all[i].AbsErr > all[j].AbsErr })
	if len(all) == 0 {
		fmt.Println("no valid samples")
		return
	}

	mae := sumAbsErr / float64(len(all))
	fmt.Printf("samples=%d mae=%.4f\n", len(all), mae)

	n := *topN
	if n > len(all) {
		n = len(all)
	}
	for i := 0; i < n; i++ {
		m := all[i]
		fmt.Printf("%2d %s %-16s actual=%.2f pred=%.2f abs=%.2f\n", i+1, m.Code, m.Name, m.Actual, m.Pred, m.AbsErr)
	}
}

func loadStocks(db *sql.DB) ([]stockRow, error) {
	const q = `
SELECT s.id,s.stock_code,s.stock_name,
       o.public_offer_shares,o.lot_size,o.offer_price,o.offer_price_high,
       a.subscription_multiple,a.one_lot_win_rate_pct
FROM stocks s
JOIN stock_offerings o ON o.stock_id=s.id
JOIN stock_allotment_summary a ON a.stock_id=s.id
WHERE s.stock_code <> '02637'
  AND a.subscription_multiple > 0
  AND a.one_lot_win_rate_pct > 0
ORDER BY s.stock_code`
	rs, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	out := make([]stockRow, 0, 128)
	for rs.Next() {
		var r stockRow
		if err := rs.Scan(
			&r.ID, &r.Code, &r.Name,
			&r.PublicShares, &r.LotSize, &r.OfferPrice, &r.OfferPriceHigh,
			&r.SubscriptionMultiple, &r.OneLotWinRatePct,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func findOneLotPerLotRate(list []ipo_predict.WinRateInfo, oneLotShares int64) float64 {
	best := 0.0
	for _, w := range list {
		if w.Lots == oneLotShares && w.PerLotRate > best {
			best = w.PerLotRate
		}
	}
	return best
}

func inferBrokerMarginSumFromSubscriptionMultiple(fundraisingAmt, realOversub float64) float64 {
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

func buildDefaultBuckets(lotSize int, price float64, publicShares int64) []ipo_predict.Tier {
	if lotSize <= 0 || price <= 0 || publicShares <= 0 {
		return nil
	}
	capShares := (publicShares / 2 / int64(lotSize)) * int64(lotSize)
	if capShares < int64(lotSize) {
		capShares = int64(lotSize)
	}
	mults := []int64{
		1, 2, 3, 4, 5,
		10, 20, 30, 40, 50,
		100, 200, 300, 400, 500,
		600, 700, 800, 900, 1000,
		2000, 3000, 4000, 5000, 6000,
		7000, 8000, 9000, 10000,
		20000, 30000,
		40000, 50000, 60000, 70000, 80000, 90000, 100000,
		200000, 300000, 400000, 500000,
	}
	buckets := make([]ipo_predict.Tier, 0, len(mults))
	for _, m := range mults {
		shares := int64(lotSize) * m
		if shares <= 0 || shares > capShares {
			continue
		}
		buckets = append(buckets, ipo_predict.Tier{
			Lots:      shares,
			AmountHKD: float64(shares) * price,
		})
	}
	if len(buckets) == 0 {
		buckets = append(buckets, ipo_predict.Tier{
			Lots:      int64(lotSize),
			AmountHKD: float64(lotSize) * price,
		})
		return buckets
	}
	last := buckets[len(buckets)-1].Lots
	if capShares > last {
		buckets = append(buckets, ipo_predict.Tier{
			Lots:      capShares,
			AmountHKD: float64(capShares) * price,
		})
	}
	return buckets
}
