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
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"hk_ipo/pkg/ipo_predict"
	"hk_ipo/tools/internal/backtestutil"
)

type stockMetric struct {
	Code            string
	Name            string
	ListDate        time.Time
	ActualOneLotPct float64
	DefaultPredPct  float64
	ProspectPredPct float64
	DefaultAbsErr   float64
	ProspectAbsErr  float64
	TierMAE         float64
	TierCount       int
}

type summaryMetric struct {
	values []float64
	sum    float64
}

func (m *summaryMetric) Add(v float64) {
	m.values = append(m.values, v)
	m.sum += v
}

func (m *summaryMetric) Count() int {
	return len(m.values)
}

func (m *summaryMetric) MAE() float64 {
	if len(m.values) == 0 {
		return 0
	}
	return m.sum / float64(len(m.values))
}

func (m *summaryMetric) Median() float64 {
	return percentile(m.values, 0.5)
}

func (m *summaryMetric) P90() float64 {
	return percentile(m.values, 0.9)
}

func main() {
	dbPath := flag.String("db", "sql/hk_ipo.db", "sqlite db path")
	topN := flag.Int("top", 20, "top N largest clean-sample errors to print")
	limit := flag.Int("limit", 0, "limit candidate stocks (0 means all)")
	cacheDir := flag.String("cache-dir", "/tmp/hkipo_prospectus_cache", "prospectus pdf cache dir")
	printCodes := flag.Bool("codes", false, "print exact-match clean stock codes")
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

	reasons := make(map[string]int)
	var cleanMetrics []stockMetric
	var defaultMetric summaryMetric
	var prospectMetric summaryMetric
	var tierMetric summaryMetric
	completeLocal := 0
	prospectusOK := 0
	exactClean := 0

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
		fundraising := float64(c.PublicShares) * c.Price
		marginSum := backtestutil.InferBrokerMarginSum(fundraising, c.SubscriptionMultiple)

		defaultPredPct := runOneLotPredict(c, marginSum, backtestutil.BuildDefaultBuckets(c.LotSize, c.Price, c.PublicShares))
		prospectPredPct, tierMAE, tierCount := runExactPredict(c, tiers, marginSum, validation.ProspectusBuckets)
		if prospectPredPct <= 0 || tierCount == 0 {
			continue
		}

		actual := c.OneLotWinRatePct
		defaultAbs := 0.0
		if defaultPredPct > 0 {
			defaultAbs = math.Abs(defaultPredPct - actual)
			defaultMetric.Add(defaultAbs)
		}
		prospectAbs := math.Abs(prospectPredPct - actual)
		prospectMetric.Add(prospectAbs)
		tierMetric.Add(tierMAE)

		cleanMetrics = append(cleanMetrics, stockMetric{
			Code:            c.Code,
			Name:            c.Name,
			ListDate:        c.ListDate,
			ActualOneLotPct: actual,
			DefaultPredPct:  defaultPredPct,
			ProspectPredPct: prospectPredPct,
			DefaultAbsErr:   defaultAbs,
			ProspectAbsErr:  prospectAbs,
			TierMAE:         tierMAE,
			TierCount:       tierCount,
		})
	}

	fmt.Printf("candidates=%d complete_local=%d prospectus_extract_ok=%d exact_clean=%d\n",
		len(candidates), completeLocal, prospectusOK, exactClean)
	fmt.Printf("one_lot_default samples=%d mae=%.4f median=%.4f p90=%.4f\n",
		defaultMetric.Count(), defaultMetric.MAE(), defaultMetric.Median(), defaultMetric.P90())
	fmt.Printf("one_lot_prospectus samples=%d mae=%.4f median=%.4f p90=%.4f\n",
		prospectMetric.Count(), prospectMetric.MAE(), prospectMetric.Median(), prospectMetric.P90())
	fmt.Printf("tier_mae_on_exact_clean samples=%d mae=%.4f median=%.4f p90=%.4f\n",
		tierMetric.Count(), tierMetric.MAE(), tierMetric.Median(), tierMetric.P90())

	if defaultMetric.Count() > 0 && prospectMetric.Count() > 0 {
		improve := defaultMetric.MAE() - prospectMetric.MAE()
		fmt.Printf("one_lot_mae_improvement=%.4f (default -> prospectus)\n", improve)
	}

	printReasonSummary(reasons)

	if *printCodes {
		printCleanCodes(cleanMetrics)
	}

	sort.Slice(cleanMetrics, func(i, j int) bool {
		return cleanMetrics[i].ProspectAbsErr > cleanMetrics[j].ProspectAbsErr
	})
	n := *topN
	if n > len(cleanMetrics) {
		n = len(cleanMetrics)
	}
	for i := 0; i < n; i++ {
		m := cleanMetrics[i]
		fmt.Printf("%2d %s %-16s actual=%.2f default=%.2f prospect=%.2f abs=%.2f tier_mae=%.4f tiers=%d\n",
			i+1, m.Code, truncateName(m.Name, 16), m.ActualOneLotPct, m.DefaultPredPct, m.ProspectPredPct,
			m.ProspectAbsErr, m.TierMAE, m.TierCount)
	}
}

func runOneLotPredict(c backtestutil.Candidate, marginSum float64, buckets []ipo_predict.Tier) float64 {
	if marginSum <= 0 || len(buckets) == 0 {
		return 0
	}
	pred, err := ipo_predict.Predict(ipo_predict.MarginRequest{
		PublicShares:    c.PublicShares,
		LotSize:         c.LotSize,
		Price:           c.Price,
		BrokerMarginSum: marginSum,
		Buckets:         buckets,
	})
	if err != nil {
		return 0
	}
	return backtestutil.FindOneLotPerLotRate(pred.WinRates, int64(c.LotSize)) * 100
}

func runExactPredict(c backtestutil.Candidate, tiers []backtestutil.TierRow, marginSum float64, buckets []ipo_predict.Tier) (float64, float64, int) {
	if marginSum <= 0 || len(buckets) == 0 || len(tiers) == 0 {
		return 0, 0, 0
	}
	pred, err := ipo_predict.Predict(ipo_predict.MarginRequest{
		PublicShares:    c.PublicShares,
		LotSize:         c.LotSize,
		Price:           c.Price,
		BrokerMarginSum: marginSum,
		Buckets:         buckets,
	})
	if err != nil {
		return 0, 0, 0
	}
	predByLots := make(map[int64]ipo_predict.WinRateInfo, len(pred.WinRates))
	for _, w := range pred.WinRates {
		predByLots[w.Lots] = w
	}
	oneLot := backtestutil.FindOneLotPerLotRate(pred.WinRates, int64(c.LotSize)) * 100
	sumAbs := 0.0
	count := 0
	for _, t := range tiers {
		p, ok := predByLots[t.Lots]
		if !ok {
			return oneLot, 0, 0
		}
		sumAbs += math.Abs(p.PerLotRate*100 - t.ActualPct)
		count++
	}
	if count == 0 {
		return oneLot, 0, 0
	}
	return oneLot, sumAbs / float64(count), count
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

func printCleanCodes(metrics []stockMetric) {
	if len(metrics) == 0 {
		fmt.Println("clean_codes=")
		return
	}
	codes := make([]string, 0, len(metrics))
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Code < metrics[j].Code })
	for _, m := range metrics {
		codes = append(codes, m.Code)
	}
	fmt.Printf("clean_codes=%s\n", strings.Join(codes, ","))
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := make([]float64, len(values))
	copy(cp, values)
	sort.Float64s(cp)
	if p <= 0 {
		return cp[0]
	}
	if p >= 1 {
		return cp[len(cp)-1]
	}
	pos := p * float64(len(cp)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return cp[lo]
	}
	frac := pos - float64(lo)
	return cp[lo] + (cp[hi]-cp[lo])*frac
}

func truncateName(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}
