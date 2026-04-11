package ipoprior

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"

	"hk_ipo/pkg/ipo_predict"
)

const (
	LowEntryFeeThresholdHKD  = 5000.0
	SmallPublicLotsThreshold = 100000.0
)

type StockFeature struct {
	ID                    int64
	Code                  string
	Name                  string
	ApplyStartDate        time.Time
	ApplyEndDate          time.Time
	ListDate              time.Time
	PublicOfferShares     int64
	LotSize               int
	Price                 float64
	AdmissionFeeHKD       float64
	SubscriptionMultiple  float64
	ActualTotalApplicants int
	ActualBRatio          float64
	ActualAOneLotRatio    float64
	PublicLots            float64
	PerLotMoney           float64
}

type ExportRow struct {
	StockID                     int64
	StockCode                   string
	StockName                   string
	ApplyStartDate              string
	ApplyEndDate                string
	ListDate                    string
	ActualTotalApplicants       int
	ActualBRatio                float64
	ActualAOneLotRatio          float64
	PublicLots                  float64
	PerLotMoney                 float64
	AdmissionFeeHKD             float64
	SubscriptionMultiple        float64
	OverlapStockCount           int
	OverlapPublicLotsSum        float64
	OverlapEntryFeeSum          float64
	OverlapLowEntryCount        int
	OverlapSmallPublicLotsCount int
	PublicLotsBucket            string
	PerLotMoneyBucket           string
	SubscriptionBucket          string
}

type comparableCandidate struct {
	feature   StockFeature
	score     int
	closeness float64
}

type rawFeatureRow struct {
	ID                    int64
	Code                  string
	Name                  string
	ApplyStartDate        sql.NullTime
	ApplyEndDate          sql.NullTime
	ListDate              sql.NullTime
	PublicOfferShares     int64
	LotSize               int
	Price                 float64
	AdmissionFeeHKD       float64
	SubscriptionMultiple  float64
	ActualTotalApplicants int
	BApplicants           int
	AApplicants           int
	AOneLotApplicants     int
}

func LoadStockFeatures(ctx context.Context, db *sql.DB) ([]StockFeature, error) {
	const query = `
SELECT
	s.id,
	s.stock_code,
	s.stock_name,
	o.apply_start_date,
	o.apply_end_date,
	o.list_date,
	o.public_offer_shares,
	o.lot_size,
	COALESCE(NULLIF(o.offer_price, 0), o.offer_price_high) AS price,
	o.admission_fee_hkd,
	COALESCE(a.subscription_multiple, 0) AS subscription_multiple,
	COALESCE(a.applicants, 0) AS actual_total_applicants,
	COALESCE(SUM(CASE WHEN t.group_code = 'B' THEN t.applicants ELSE 0 END), 0) AS b_applicants,
	COALESCE(SUM(CASE WHEN t.group_code <> 'B' THEN t.applicants ELSE 0 END), 0) AS a_applicants,
	COALESCE(SUM(CASE WHEN t.group_code <> 'B' AND t.lots = o.lot_size THEN t.applicants ELSE 0 END), 0) AS a_one_lot_applicants
FROM stocks s
JOIN stock_offerings o ON o.stock_id = s.id
LEFT JOIN stock_allotment_summary a ON a.stock_id = s.id
LEFT JOIN stock_allotment_tiers t ON t.stock_id = s.id
WHERE o.apply_start_date IS NOT NULL
  AND o.apply_end_date IS NOT NULL
  AND o.public_offer_shares > 0
  AND o.lot_size > 0
  AND COALESCE(NULLIF(o.offer_price, 0), o.offer_price_high) > 0
GROUP BY
	s.id,
	s.stock_code,
	s.stock_name,
	o.apply_start_date,
	o.apply_end_date,
	o.list_date,
	o.public_offer_shares,
	o.lot_size,
	price,
	o.admission_fee_hkd,
	a.subscription_multiple,
	a.applicants
ORDER BY datetime(o.apply_start_date) ASC, s.stock_code ASC`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]StockFeature, 0, 128)
	for rows.Next() {
		var row rawFeatureRow
		if err := rows.Scan(
			&row.ID,
			&row.Code,
			&row.Name,
			&row.ApplyStartDate,
			&row.ApplyEndDate,
			&row.ListDate,
			&row.PublicOfferShares,
			&row.LotSize,
			&row.Price,
			&row.AdmissionFeeHKD,
			&row.SubscriptionMultiple,
			&row.ActualTotalApplicants,
			&row.BApplicants,
			&row.AApplicants,
			&row.AOneLotApplicants,
		); err != nil {
			return nil, err
		}
		if !row.ApplyStartDate.Valid || !row.ApplyEndDate.Valid {
			continue
		}
		f := StockFeature{
			ID:                    row.ID,
			Code:                  row.Code,
			Name:                  row.Name,
			ApplyStartDate:        row.ApplyStartDate.Time,
			ApplyEndDate:          row.ApplyEndDate.Time,
			PublicOfferShares:     row.PublicOfferShares,
			LotSize:               row.LotSize,
			Price:                 row.Price,
			AdmissionFeeHKD:       row.AdmissionFeeHKD,
			SubscriptionMultiple:  row.SubscriptionMultiple,
			ActualTotalApplicants: row.ActualTotalApplicants,
		}
		if row.ListDate.Valid {
			f.ListDate = row.ListDate.Time
		}
		f.PublicLots = float64(row.PublicOfferShares) / float64(row.LotSize)
		f.PerLotMoney = float64(row.LotSize) * row.Price
		f.ActualBRatio = -1
		f.ActualAOneLotRatio = -1
		if row.ActualTotalApplicants > 0 {
			f.ActualBRatio = float64(row.BApplicants) / float64(row.ActualTotalApplicants)
		}
		if row.AApplicants > 0 {
			f.ActualAOneLotRatio = float64(row.AOneLotApplicants) / float64(row.AApplicants)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func FindStockByID(stocks []StockFeature, stockID int64) (StockFeature, bool) {
	for _, stock := range stocks {
		if stock.ID == stockID {
			return stock, true
		}
	}
	return StockFeature{}, false
}

func BuildAutoEstimateContext(stocks []StockFeature, current StockFeature, currentSubscriptionMultiple float64) ipo_predict.AutoEstimateContext {
	overlap := computeOverlapFeatures(stocks, current)
	peers := selectComparablePeers(stocks, current, currentSubscriptionMultiple)
	applicants := make([]float64, 0, len(peers))
	bRatios := make([]float64, 0, len(peers))
	aOneRatios := make([]float64, 0, len(peers))
	for _, peer := range peers {
		if peer.ActualTotalApplicants > 0 {
			applicants = append(applicants, float64(peer.ActualTotalApplicants))
		}
		if peer.ActualBRatio >= 0 {
			bRatios = append(bRatios, peer.ActualBRatio)
		}
		if peer.ActualAOneLotRatio >= 0 {
			aOneRatios = append(aOneRatios, peer.ActualAOneLotRatio)
		}
	}
	return ipo_predict.AutoEstimateContext{
		ComparablePeerCount:                    len(peers),
		ComparableApplicantMedian:              percentileInt(applicants, 0.5),
		ComparableApplicantP10:                 percentileInt(applicants, 0.1),
		ComparableApplicantP90:                 percentileInt(applicants, 0.9),
		ComparableBGroupApplicantRatioMedian:   percentileFloat(bRatios, 0.5),
		ComparableAOneHandApplicantRatioMedian: percentileFloat(aOneRatios, 0.5),
		OverlapStockCount:                      overlap.OverlapStockCount,
		OverlapPublicLotsSum:                   overlap.OverlapPublicLotsSum,
		OverlapEntryFeeSum:                     overlap.OverlapEntryFeeSum,
		OverlapLowEntryCount:                   overlap.OverlapLowEntryCount,
		OverlapSmallPublicLotsCount:            overlap.OverlapSmallPublicLotsCount,
	}
}

func BuildAutoEstimateContextByStockID(stocks []StockFeature, stockID int64, currentSubscriptionMultiple float64) (ipo_predict.AutoEstimateContext, StockFeature, error) {
	current, ok := FindStockByID(stocks, stockID)
	if !ok {
		return ipo_predict.AutoEstimateContext{}, StockFeature{}, fmt.Errorf("stock_id=%d not found in feature set", stockID)
	}
	return BuildAutoEstimateContext(stocks, current, currentSubscriptionMultiple), current, nil
}

func BuildExportRows(stocks []StockFeature) []ExportRow {
	out := make([]ExportRow, 0, len(stocks))
	for _, stock := range stocks {
		ctx := BuildAutoEstimateContext(stocks, stock, stock.SubscriptionMultiple)
		row := ExportRow{
			StockID:                     stock.ID,
			StockCode:                   stock.Code,
			StockName:                   stock.Name,
			ApplyStartDate:              stock.ApplyStartDate.Format("2006-01-02"),
			ApplyEndDate:                stock.ApplyEndDate.Format("2006-01-02"),
			ActualTotalApplicants:       stock.ActualTotalApplicants,
			ActualBRatio:                stock.ActualBRatio,
			ActualAOneLotRatio:          stock.ActualAOneLotRatio,
			PublicLots:                  stock.PublicLots,
			PerLotMoney:                 stock.PerLotMoney,
			AdmissionFeeHKD:             stock.AdmissionFeeHKD,
			SubscriptionMultiple:        stock.SubscriptionMultiple,
			OverlapStockCount:           ctx.OverlapStockCount,
			OverlapPublicLotsSum:        ctx.OverlapPublicLotsSum,
			OverlapEntryFeeSum:          ctx.OverlapEntryFeeSum,
			OverlapLowEntryCount:        ctx.OverlapLowEntryCount,
			OverlapSmallPublicLotsCount: ctx.OverlapSmallPublicLotsCount,
			PublicLotsBucket:            PublicLotsBucket(stock.PublicLots),
			PerLotMoneyBucket:           PerLotMoneyBucket(stock.PerLotMoney),
			SubscriptionBucket:          SubscriptionMultipleBucket(stock.SubscriptionMultiple),
		}
		if !stock.ListDate.IsZero() {
			row.ListDate = stock.ListDate.Format("2006-01-02")
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ApplyStartDate == out[j].ApplyStartDate {
			return out[i].StockCode < out[j].StockCode
		}
		return out[i].ApplyStartDate < out[j].ApplyStartDate
	})
	return out
}

func PublicLotsBucket(publicLots float64) string {
	switch {
	case publicLots <= 40000:
		return "<=40k"
	case publicLots <= 100000:
		return "40k-100k"
	case publicLots <= 250000:
		return "100k-250k"
	default:
		return ">250k"
	}
}

func PerLotMoneyBucket(perLotMoney float64) string {
	switch {
	case perLotMoney <= 4000:
		return "<=4k"
	case perLotMoney <= 8000:
		return "4k-8k"
	case perLotMoney <= 15000:
		return "8k-15k"
	default:
		return ">15k"
	}
}

func SubscriptionMultipleBucket(subscriptionMultiple float64) string {
	switch {
	case subscriptionMultiple <= 10:
		return "<=10"
	case subscriptionMultiple <= 80:
		return "10-80"
	case subscriptionMultiple <= 300:
		return "80-300"
	case subscriptionMultiple <= 1000:
		return "300-1000"
	default:
		return ">1000"
	}
}

func computeOverlapFeatures(stocks []StockFeature, current StockFeature) ipo_predict.AutoEstimateContext {
	var out ipo_predict.AutoEstimateContext
	for _, other := range stocks {
		if other.ID == current.ID {
			continue
		}
		if other.ApplyStartDate.IsZero() || other.ApplyEndDate.IsZero() {
			continue
		}
		if other.ApplyStartDate.After(current.ApplyEndDate) || other.ApplyEndDate.Before(current.ApplyStartDate) {
			continue
		}
		out.OverlapStockCount++
		out.OverlapPublicLotsSum += other.PublicLots
		out.OverlapEntryFeeSum += other.AdmissionFeeHKD
		if other.AdmissionFeeHKD > 0 && other.AdmissionFeeHKD <= LowEntryFeeThresholdHKD {
			out.OverlapLowEntryCount++
		}
		if other.PublicLots > 0 && other.PublicLots <= SmallPublicLotsThreshold {
			out.OverlapSmallPublicLotsCount++
		}
	}
	return out
}

func selectComparablePeers(stocks []StockFeature, current StockFeature, currentSubscriptionMultiple float64) []StockFeature {
	targetPublicBucket := PublicLotsBucket(current.PublicLots)
	targetPerLotBucket := PerLotMoneyBucket(current.PerLotMoney)
	targetSubBucket := SubscriptionMultipleBucket(currentSubscriptionMultiple)

	matched := make([]comparableCandidate, 0, len(stocks))
	fallback := make([]comparableCandidate, 0, len(stocks))
	for _, other := range stocks {
		if other.ID == current.ID {
			continue
		}
		if other.ApplyEndDate.IsZero() || !other.ApplyEndDate.Before(current.ApplyStartDate) {
			continue
		}
		if other.ActualTotalApplicants <= 0 {
			continue
		}
		score := 0
		if PublicLotsBucket(other.PublicLots) == targetPublicBucket {
			score += 4
		}
		if PerLotMoneyBucket(other.PerLotMoney) == targetPerLotBucket {
			score += 2
		}
		if SubscriptionMultipleBucket(other.SubscriptionMultiple) == targetSubBucket {
			score += 1
		}
		candidate := comparableCandidate{
			feature: other,
			score:   score,
			closeness: math.Abs(safeLog10Ratio(current.PublicLots, other.PublicLots)) +
				math.Abs(safeLog10Ratio(current.PerLotMoney, other.PerLotMoney)) +
				0.5*math.Abs(safeLog10Ratio(max(currentSubscriptionMultiple, 1), max(other.SubscriptionMultiple, 1))),
		}
		if score > 0 {
			matched = append(matched, candidate)
		} else {
			fallback = append(fallback, candidate)
		}
	}

	sortComparableCandidates(matched)
	sortComparableCandidates(fallback)

	selected := selectPreferredMatchedPeers(matched)
	if len(selected) > 0 {
		return selected
	}

	out := make([]StockFeature, 0, min(80, len(matched)+len(fallback)))
	for i := 0; i < min(40, len(matched)); i++ {
		out = append(out, matched[i].feature)
	}
	for _, candidate := range fallback {
		if len(out) >= 80 {
			break
		}
		out = append(out, candidate.feature)
	}
	return out
}

func sortComparableCandidates(list []comparableCandidate) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		if !list[i].feature.ApplyEndDate.Equal(list[j].feature.ApplyEndDate) {
			return list[i].feature.ApplyEndDate.After(list[j].feature.ApplyEndDate)
		}
		if list[i].closeness != list[j].closeness {
			return list[i].closeness < list[j].closeness
		}
		return list[i].feature.Code < list[j].feature.Code
	})
}

func selectPreferredMatchedPeers(matched []comparableCandidate) []StockFeature {
	thresholds := []int{5, 4, 2}
	for _, threshold := range thresholds {
		group := make([]StockFeature, 0, min(40, len(matched)))
		for _, candidate := range matched {
			if candidate.score < threshold {
				continue
			}
			group = append(group, candidate.feature)
			if len(group) >= 40 {
				break
			}
		}
		if len(group) >= 10 {
			return group
		}
	}
	return nil
}

func safeLog10Ratio(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return math.Log10(a / b)
}

func percentileFloat(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func percentileInt(values []float64, p float64) int {
	if len(values) == 0 {
		return 0
	}
	return int(math.Round(percentileFloat(values, p)))
}
