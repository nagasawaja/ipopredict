package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"hk_ipo/pkg/ipo_predict"
	"hk_ipo/pkg/ipoprior"
	"hk_ipo/tools/internal/backtestutil"
)

const (
	modelBGroupApplicantRatio = 0.03
	modelAOneLotFraction      = 0.42610359528714076
)

type tierActual struct {
	Seq        int
	GroupCode  string
	Lots       int64
	Applicants int
	WinRatePct float64
}

type stockOutlier struct {
	Code              string
	Name              string
	ListDate          time.Time
	SubscriptionMulti float64
	ActualOneLotPct   float64
	PredOneLotPct     float64
	OneLotAbsErr      float64
	ActualApplicants  int
	PredApplicants    int
	ApplicantAPE      float64
}

type metric struct {
	abs    []float64
	signed []float64
	sumAbs float64
	sumSq  float64
	sum    float64
}

func (m *metric) Add(actual, pred float64) {
	err := pred - actual
	abs := math.Abs(err)
	m.abs = append(m.abs, abs)
	m.signed = append(m.signed, err)
	m.sumAbs += abs
	m.sumSq += err * err
	m.sum += err
}

func (m *metric) Count() int {
	return len(m.abs)
}

func (m *metric) MAE() float64 {
	if len(m.abs) == 0 {
		return 0
	}
	return m.sumAbs / float64(len(m.abs))
}

func (m *metric) RMSE() float64 {
	if len(m.abs) == 0 {
		return 0
	}
	return math.Sqrt(m.sumSq / float64(len(m.abs)))
}

func (m *metric) Bias() float64 {
	if len(m.signed) == 0 {
		return 0
	}
	return m.sum / float64(len(m.signed))
}

func (m *metric) MedianAbs() float64 {
	return percentile(m.abs, 0.5)
}

func (m *metric) P90Abs() float64 {
	return percentile(m.abs, 0.9)
}

func (m *metric) HitRate(threshold float64) float64 {
	if len(m.abs) == 0 {
		return 0
	}
	hit := 0
	for _, v := range m.abs {
		if v <= threshold {
			hit++
		}
	}
	return float64(hit) / float64(len(m.abs))
}

type ratioMetric struct {
	values []float64
	sum    float64
}

func (m *ratioMetric) Add(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	m.values = append(m.values, v)
	m.sum += v
}

func (m *ratioMetric) Count() int {
	return len(m.values)
}

func (m *ratioMetric) Mean() float64 {
	if len(m.values) == 0 {
		return 0
	}
	return m.sum / float64(len(m.values))
}

func (m *ratioMetric) Median() float64 {
	return percentile(m.values, 0.5)
}

func (m *ratioMetric) P10() float64 {
	return percentile(m.values, 0.1)
}

func (m *ratioMetric) P90() float64 {
	return percentile(m.values, 0.9)
}

type bandStat struct {
	label              string
	oneLot             metric
	totalApplicantsAPE ratioMetric
}

type evaluationStats struct {
	label                 string
	oneLotMetric          metric
	tierRateMetric        metric
	totalApplicantsMetric metric
	totalApplicantsAPE    ratioMetric
	tierApplicantsMetric  metric
	tierApplicantsAPE     ratioMetric
	bRatioActual          ratioMetric
	aOneLotFractionActual ratioMetric
	outliers              []stockOutlier
	bands                 []*bandStat
	predictedStocks       int
}

func newEvaluationStats(label string) *evaluationStats {
	return &evaluationStats{
		label: label,
		bands: []*bandStat{
			{label: "low_sub_le_10"},
			{label: "mid_sub_10_to_80"},
			{label: "high_sub_gt_80"},
		},
	}
}

func (s *evaluationStats) observe(c backtestutil.Candidate, actualApplicants int, actualTiers []tierActual, pred ipo_predict.PredictResult) {
	predByLots := make(map[int64]ipo_predict.WinRateInfo, len(pred.WinRates))
	predApplicants := 0
	for _, row := range pred.WinRates {
		predByLots[row.Lots] = row
		predApplicants += row.Applicants
	}
	predOneLotPct := backtestutil.FindOneLotPerLotRate(pred.WinRates, int64(c.LotSize)) * 100
	if predOneLotPct <= 0 {
		return
	}
	s.predictedStocks++

	s.oneLotMetric.Add(c.OneLotWinRatePct, predOneLotPct)
	s.totalApplicantsMetric.Add(float64(actualApplicants), float64(predApplicants))
	s.totalApplicantsAPE.Add(absPercentError(float64(actualApplicants), float64(predApplicants)))

	for _, t := range actualTiers {
		p, ok := predByLots[t.Lots]
		if !ok {
			continue
		}
		s.tierRateMetric.Add(t.WinRatePct, p.PerLotRate*100)
		if t.Applicants > 0 {
			s.tierApplicantsMetric.Add(float64(t.Applicants), float64(p.Applicants))
			s.tierApplicantsAPE.Add(absPercentError(float64(t.Applicants), float64(p.Applicants)))
		}
	}

	bRatio, aOneLotFraction := actualStructureRatios(actualTiers, c.LotSize, actualApplicants)
	if bRatio >= 0 {
		s.bRatioActual.Add(bRatio)
	}
	if aOneLotFraction >= 0 {
		s.aOneLotFractionActual.Add(aOneLotFraction)
	}

	bandFor(c.SubscriptionMultiple, s.bands).oneLot.Add(c.OneLotWinRatePct, predOneLotPct)
	bandFor(c.SubscriptionMultiple, s.bands).totalApplicantsAPE.Add(absPercentError(float64(actualApplicants), float64(predApplicants)))

	s.outliers = append(s.outliers, stockOutlier{
		Code:              c.Code,
		Name:              c.Name,
		ListDate:          c.ListDate,
		SubscriptionMulti: c.SubscriptionMultiple,
		ActualOneLotPct:   c.OneLotWinRatePct,
		PredOneLotPct:     predOneLotPct,
		OneLotAbsErr:      math.Abs(predOneLotPct - c.OneLotWinRatePct),
		ActualApplicants:  actualApplicants,
		PredApplicants:    predApplicants,
		ApplicantAPE:      absPercentError(float64(actualApplicants), float64(predApplicants)),
	})
}

func (s *evaluationStats) sortOutliers() {
	sort.Slice(s.outliers, func(i, j int) bool {
		if s.outliers[i].OneLotAbsErr == s.outliers[j].OneLotAbsErr {
			return s.outliers[i].Code < s.outliers[j].Code
		}
		return s.outliers[i].OneLotAbsErr > s.outliers[j].OneLotAbsErr
	})
}

func (s *evaluationStats) printSummary(topN int) {
	s.sortOutliers()
	fmt.Printf("%s predicted=%d\n", s.label, s.predictedStocks)
	fmt.Printf("one_lot_pct_points samples=%d mae=%.4f rmse=%.4f bias=%.4f median_abs=%.4f p90_abs=%.4f hit_le_1pp=%.2f%% hit_le_3pp=%.2f%% hit_le_5pp=%.2f%% hit_le_10pp=%.2f%%\n",
		s.oneLotMetric.Count(), s.oneLotMetric.MAE(), s.oneLotMetric.RMSE(), s.oneLotMetric.Bias(), s.oneLotMetric.MedianAbs(), s.oneLotMetric.P90Abs(),
		s.oneLotMetric.HitRate(1)*100, s.oneLotMetric.HitRate(3)*100, s.oneLotMetric.HitRate(5)*100, s.oneLotMetric.HitRate(10)*100)
	fmt.Printf("tier_win_rate_pct_points samples=%d mae=%.4f rmse=%.4f bias=%.4f median_abs=%.4f p90_abs=%.4f hit_le_0.5pp=%.2f%% hit_le_1pp=%.2f%% hit_le_2pp=%.2f%%\n",
		s.tierRateMetric.Count(), s.tierRateMetric.MAE(), s.tierRateMetric.RMSE(), s.tierRateMetric.Bias(), s.tierRateMetric.MedianAbs(), s.tierRateMetric.P90Abs(),
		s.tierRateMetric.HitRate(0.5)*100, s.tierRateMetric.HitRate(1)*100, s.tierRateMetric.HitRate(2)*100)
	fmt.Printf("total_applicants_people samples=%d mae=%.2f rmse=%.2f bias=%.2f\n",
		s.totalApplicantsMetric.Count(), s.totalApplicantsMetric.MAE(), s.totalApplicantsMetric.RMSE(), s.totalApplicantsMetric.Bias())
	fmt.Printf("total_applicants_ape samples=%d mean=%.2f%% median=%.2f%% p90=%.2f%% hit_le_10pct=%.2f%% hit_le_20pct=%.2f%% hit_le_50pct=%.2f%%\n",
		s.totalApplicantsAPE.Count(), s.totalApplicantsAPE.Mean()*100, s.totalApplicantsAPE.Median()*100, s.totalApplicantsAPE.P90()*100,
		ratioHitRate(s.totalApplicantsAPE.values, 0.10)*100, ratioHitRate(s.totalApplicantsAPE.values, 0.20)*100, ratioHitRate(s.totalApplicantsAPE.values, 0.50)*100)
	fmt.Printf("tier_applicants_people samples=%d mae=%.2f rmse=%.2f bias=%.2f\n",
		s.tierApplicantsMetric.Count(), s.tierApplicantsMetric.MAE(), s.tierApplicantsMetric.RMSE(), s.tierApplicantsMetric.Bias())
	fmt.Printf("tier_applicants_ape samples=%d mean=%.2f%% median=%.2f%% p90=%.2f%% hit_le_20pct=%.2f%% hit_le_50pct=%.2f%% hit_le_100pct=%.2f%%\n",
		s.tierApplicantsAPE.Count(), s.tierApplicantsAPE.Mean()*100, s.tierApplicantsAPE.Median()*100, s.tierApplicantsAPE.P90()*100,
		ratioHitRate(s.tierApplicantsAPE.values, 0.20)*100, ratioHitRate(s.tierApplicantsAPE.values, 0.50)*100, ratioHitRate(s.tierApplicantsAPE.values, 1.00)*100)
	fmt.Printf("actual_b_group_ratio model=%.2f%% mean=%.2f%% median=%.2f%% p10=%.2f%% p90=%.2f%%\n",
		modelBGroupApplicantRatio*100, s.bRatioActual.Mean()*100, s.bRatioActual.Median()*100, s.bRatioActual.P10()*100, s.bRatioActual.P90()*100)
	fmt.Printf("actual_a_one_lot_fraction model=%.2f%% mean=%.2f%% median=%.2f%% p10=%.2f%% p90=%.2f%%\n",
		modelAOneLotFraction*100, s.aOneLotFractionActual.Mean()*100, s.aOneLotFractionActual.Median()*100, s.aOneLotFractionActual.P10()*100, s.aOneLotFractionActual.P90()*100)
	fmt.Println("one_lot_by_oversub_band:")
	for _, band := range s.bands {
		fmt.Printf("  %s samples=%d mae=%.4f total_applicants_mean_ape=%.2f%%\n",
			band.label, band.oneLot.Count(), band.oneLot.MAE(), band.totalApplicantsAPE.Mean()*100)
	}
	printTopOutliers(s.outliers, topN)
}

func main() {
	dbPath := flag.String("db", "sql/hk_ipo.db", "sqlite db path")
	cacheDir := flag.String("cache-dir", "/tmp/hkipo_prospectus_cache", "prospectus pdf cache dir")
	limit := flag.Int("limit", 0, "candidate limit (0 means all)")
	topN := flag.Int("top", 10, "top outliers to print")
	flag.Parse()

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	candidates, err := backtestutil.LoadCandidates(db, *limit)
	if err != nil {
		log.Fatal(err)
	}
	if len(candidates) == 0 {
		fmt.Println("no candidates")
		return
	}

	ctx := context.Background()
	httpClient := &http.Client{Timeout: 90 * time.Second}
	stockFeatures, err := ipoprior.LoadStockFeatures(ctx, db)
	if err != nil {
		log.Fatal(err)
	}

	reasons := make(map[string]int)
	completeLocal := 0
	prospectusOK := 0
	exactClean := 0
	oldStats := newEvaluationStats("old")
	newStats := newEvaluationStats("new")

	for _, c := range candidates {
		tiers, err := backtestutil.LoadAnnouncementTiers(db, c.ID)
		if err != nil {
			log.Fatalf("load tiers %s: %v", c.Code, err)
		}

		validation := backtestutil.ValidateCandidate(ctx, httpClient, c, tiers, *cacheDir)
		reasons[validation.Reason]++
		if validation.Reason == backtestutil.ReasonOK ||
			validation.Reason == backtestutil.ReasonTierCountMismatch ||
			validation.Reason == backtestutil.ReasonTierLotsMismatch {
			completeLocal++
		}
		if len(validation.ProspectusBuckets) > 0 {
			prospectusOK++
		}
		if !validation.ExactMatch {
			continue
		}
		exactClean++

		actualApplicants, actualTiers, err := loadActualSummary(db, c.ID)
		if err != nil {
			log.Fatalf("load actual summary %s: %v", c.Code, err)
		}
		if actualApplicants <= 0 || len(actualTiers) == 0 {
			continue
		}

		marginSum := backtestutil.InferBrokerMarginSum(float64(c.PublicShares)*c.Price, c.SubscriptionMultiple)
		oldPred, err := ipo_predict.Predict(ipo_predict.MarginRequest{
			PublicShares:    c.PublicShares,
			LotSize:         c.LotSize,
			Price:           c.Price,
			BrokerMarginSum: marginSum,
			Buckets:         validation.ProspectusBuckets,
		})
		if err != nil {
			continue
		}
		oldStats.observe(c, actualApplicants, actualTiers, oldPred)

		autoCtx, _, err := ipoprior.BuildAutoEstimateContextByStockID(stockFeatures, c.ID, c.SubscriptionMultiple)
		if err != nil {
			log.Printf("build auto context %s: %v", c.Code, err)
			continue
		}
		newPred, err := ipo_predict.Predict(ipo_predict.MarginRequest{
			PublicShares:        c.PublicShares,
			LotSize:             c.LotSize,
			Price:               c.Price,
			BrokerMarginSum:     marginSum,
			Buckets:             validation.ProspectusBuckets,
			AutoEstimateContext: &autoCtx,
		})
		if err != nil {
			continue
		}
		newStats.observe(c, actualApplicants, actualTiers, newPred)
	}

	fmt.Printf("coverage candidates=%d complete_local=%d prospectus_extract_ok=%d exact_clean=%d predicted_old=%d predicted_new=%d\n",
		len(candidates), completeLocal, prospectusOK, exactClean, oldStats.predictedStocks, newStats.predictedStocks)
	printReasonSummary(reasons)
	fmt.Println("=== old ===")
	oldStats.printSummary(*topN)
	fmt.Println("=== new ===")
	newStats.printSummary(*topN)
	fmt.Println("=== delta_new_minus_old ===")
	fmt.Printf("one_lot_mae_delta=%.4fpp\n", newStats.oneLotMetric.MAE()-oldStats.oneLotMetric.MAE())
	fmt.Printf("tier_win_rate_mae_delta=%.4fpp\n", newStats.tierRateMetric.MAE()-oldStats.tierRateMetric.MAE())
	fmt.Printf("total_applicants_mean_ape_delta=%.2fpp median_ape_delta=%.2fpp\n",
		(newStats.totalApplicantsAPE.Mean()-oldStats.totalApplicantsAPE.Mean())*100,
		(newStats.totalApplicantsAPE.Median()-oldStats.totalApplicantsAPE.Median())*100)
	fmt.Printf("tier_applicants_mean_ape_delta=%.2fpp median_ape_delta=%.2fpp\n",
		(newStats.tierApplicantsAPE.Mean()-oldStats.tierApplicantsAPE.Mean())*100,
		(newStats.tierApplicantsAPE.Median()-oldStats.tierApplicantsAPE.Median())*100)
}

func loadActualSummary(db *sql.DB, stockID int64) (int, []tierActual, error) {
	var applicants int
	if err := db.QueryRow(`SELECT applicants FROM stock_allotment_summary WHERE stock_id = ?`, stockID).Scan(&applicants); err != nil {
		return 0, nil, err
	}

	rs, err := db.Query(`
SELECT seq, group_code, lots, applicants, win_rate_pct
FROM stock_allotment_tiers
WHERE stock_id = ?
ORDER BY seq`, stockID)
	if err != nil {
		return 0, nil, err
	}
	defer rs.Close()

	out := make([]tierActual, 0, 64)
	for rs.Next() {
		var row tierActual
		if err := rs.Scan(&row.Seq, &row.GroupCode, &row.Lots, &row.Applicants, &row.WinRatePct); err != nil {
			return 0, nil, err
		}
		out = append(out, row)
	}
	return applicants, out, rs.Err()
}

func actualStructureRatios(tiers []tierActual, lotSize int, totalApplicants int) (float64, float64) {
	if totalApplicants <= 0 {
		return -1, -1
	}
	bApplicants := 0
	aApplicants := 0
	aOneLotApplicants := 0
	for _, t := range tiers {
		switch t.GroupCode {
		case "B":
			bApplicants += t.Applicants
		default:
			aApplicants += t.Applicants
			if t.Lots == int64(lotSize) {
				aOneLotApplicants += t.Applicants
			}
		}
	}
	bRatio := float64(bApplicants) / float64(totalApplicants)
	aOneLotFraction := -1.0
	if aApplicants > 0 {
		aOneLotFraction = float64(aOneLotApplicants) / float64(aApplicants)
	}
	return bRatio, aOneLotFraction
}

func bandFor(sub float64, bands []*bandStat) *bandStat {
	switch {
	case sub <= 10:
		return bands[0]
	case sub <= 80:
		return bands[1]
	default:
		return bands[2]
	}
}

func absPercentError(actual, pred float64) float64 {
	if actual == 0 {
		return 0
	}
	return math.Abs(pred-actual) / math.Abs(actual)
}

func ratioHitRate(values []float64, threshold float64) float64 {
	if len(values) == 0 {
		return 0
	}
	hit := 0
	for _, v := range values {
		if v <= threshold {
			hit++
		}
	}
	return float64(hit) / float64(len(values))
}

func percentile(values []float64, p float64) float64 {
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

func printReasonSummary(reasons map[string]int) {
	type row struct {
		reason string
		count  int
	}
	list := make([]row, 0, len(reasons))
	for reason, count := range reasons {
		list = append(list, row{reason: reason, count: count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count == list[j].count {
			return list[i].reason < list[j].reason
		}
		return list[i].count > list[j].count
	})
	fmt.Println("reason_summary:")
	for _, item := range list {
		fmt.Printf("  %s=%d\n", item.reason, item.count)
	}
}

func printTopOutliers(outliers []stockOutlier, topN int) {
	if topN > len(outliers) {
		topN = len(outliers)
	}
	fmt.Println("top_one_lot_outliers:")
	for i := 0; i < topN; i++ {
		o := outliers[i]
		fmt.Printf("  %2d %s %-16s sub=%.2f actual=%.2f pred=%.2f abs=%.2f applicants_actual=%d applicants_pred=%d applicants_ape=%.2f%%\n",
			i+1, o.Code, truncateName(o.Name, 16), o.SubscriptionMulti, o.ActualOneLotPct, o.PredOneLotPct, o.OneLotAbsErr,
			o.ActualApplicants, o.PredApplicants, o.ApplicantAPE*100)
	}

	sort.Slice(outliers, func(i, j int) bool {
		if outliers[i].ApplicantAPE == outliers[j].ApplicantAPE {
			return outliers[i].Code < outliers[j].Code
		}
		return outliers[i].ApplicantAPE > outliers[j].ApplicantAPE
	})
	fmt.Println("top_total_applicant_outliers:")
	for i := 0; i < topN; i++ {
		o := outliers[i]
		fmt.Printf("  %2d %s %-16s sub=%.2f applicants_actual=%d applicants_pred=%d ape=%.2f%% one_lot_actual=%.2f one_lot_pred=%.2f\n",
			i+1, o.Code, truncateName(o.Name, 16), o.SubscriptionMulti, o.ActualApplicants, o.PredApplicants, o.ApplicantAPE*100,
			o.ActualOneLotPct, o.PredOneLotPct)
	}
}

func truncateName(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
