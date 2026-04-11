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
	"hk_ipo/tools/internal/backtestutil"
)

type sample struct {
	Code         string
	ActualRate   float64
	CurrentRate  float64
	RealOversub  float64
	PerLotMoney  float64
	PublicLots   float64
	FeatureVec   []float64
	FeatureLabel string
}

func main() {
	dbPath := flag.String("db", "sql/hk_ipo.db", "sqlite db path")
	cacheDir := flag.String("cache-dir", "/tmp/hkipo_prospectus_cache", "prospectus pdf cache dir")
	folds := flag.Int("folds", 5, "k-fold cross validation")
	limit := flag.Int("limit", 0, "candidate limit")
	flag.Parse()

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	samples, err := loadSamples(db, *cacheDir, *limit)
	if err != nil {
		log.Fatal(err)
	}
	if len(samples) == 0 {
		fmt.Println("no clean samples")
		return
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i].Code < samples[j].Code })

	currentMAE := 0.0
	for _, s := range samples {
		currentMAE += math.Abs(s.CurrentRate - s.ActualRate)
	}
	currentMAE /= float64(len(samples))

	foldCount := *folds
	if foldCount < 2 {
		foldCount = 2
	}
	if foldCount > len(samples) {
		foldCount = len(samples)
	}

	bandMAE, bandScales := crossValidateBandScales(samples, foldCount)

	cvMAE := 0.0
	cvCount := 0
	for fold := 0; fold < foldCount; fold++ {
		var train, test []sample
		for i, s := range samples {
			if i%foldCount == fold {
				test = append(test, s)
			} else {
				train = append(train, s)
			}
		}
		beta, err := fitLogLinear(train)
		if err != nil {
			log.Fatalf("fit fold %d: %v", fold, err)
		}
		for _, s := range test {
			pred := predictRate(beta, s)
			cvMAE += math.Abs(pred - s.ActualRate)
			cvCount++
		}
	}
	if cvCount > 0 {
		cvMAE /= float64(cvCount)
	}

	beta, err := fitLogLinear(samples)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("clean_samples=%d current_mae=%.4f band_cv_mae=%.4f fitted_cv_mae=%.4f\n", len(samples), currentMAE, bandMAE, cvMAE)
	fmt.Printf("band_scales low=%.12f mid=%.12f high=%.12f\n", bandScales[0], bandScales[1], bandScales[2])
	fmt.Printf("intercept=%.12f\n", beta[0])
	fmt.Printf("sub_log_coef=%.12f\n", beta[1])
	fmt.Printf("sub_log2_coef=%.12f\n", beta[2])
	fmt.Printf("per_lot_log_coef=%.12f\n", beta[3])
	fmt.Printf("public_lots_log_coef=%.12f\n", beta[4])
}

func loadSamples(db *sql.DB, cacheDir string, limit int) ([]sample, error) {
	candidates, err := backtestutil.LoadCandidates(db, limit)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 90 * time.Second}

	out := make([]sample, 0, len(candidates))
	for _, c := range candidates {
		tiers, err := backtestutil.LoadAnnouncementTiers(db, c.ID)
		if err != nil {
			return nil, err
		}
		v := backtestutil.ValidateCandidate(ctx, httpClient, c, tiers, cacheDir)
		if !v.ExactMatch {
			continue
		}
		marginSum := backtestutil.InferBrokerMarginSum(float64(c.PublicShares)*c.Price, c.SubscriptionMultiple)
		currentRate := runOneLotPredict(c, marginSum, v.ProspectusBuckets)
		if currentRate <= 0 {
			continue
		}
		perLotMoney := float64(c.LotSize) * c.Price
		publicLots := float64(c.PublicShares) / float64(c.LotSize)
		subLog := math.Log(c.SubscriptionMultiple)
		perLotLog := math.Log(perLotMoney / 5000.0)
		publicLotsLog := math.Log(publicLots / 100000.0)
		out = append(out, sample{
			Code:        c.Code,
			ActualRate:  c.OneLotWinRatePct,
			CurrentRate: currentRate,
			RealOversub: c.SubscriptionMultiple,
			PerLotMoney: perLotMoney,
			PublicLots:  publicLots,
			FeatureVec: []float64{
				1.0,
				subLog,
				subLog * subLog,
				perLotLog,
				publicLotsLog,
			},
		})
	}
	return out, nil
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

func fitLogLinear(samples []sample) ([]float64, error) {
	if len(samples) == 0 {
		return nil, fmt.Errorf("no samples")
	}
	const dim = 5
	xtx := make([][]float64, dim)
	for i := range xtx {
		xtx[i] = make([]float64, dim)
	}
	xty := make([]float64, dim)
	for _, s := range samples {
		y := math.Log(s.ActualRate)
		x := s.FeatureVec
		for i := 0; i < dim; i++ {
			xty[i] += x[i] * y
			for j := 0; j < dim; j++ {
				xtx[i][j] += x[i] * x[j]
			}
		}
	}
	return solveLinearSystem(xtx, xty)
}

func predictRate(beta []float64, s sample) float64 {
	score := 0.0
	for i := range beta {
		score += beta[i] * s.FeatureVec[i]
	}
	rate := math.Exp(score)
	if rate < 1.0 {
		return 1.0
	}
	if rate > 100.0 {
		return 100.0
	}
	return rate
}

func solveLinearSystem(a [][]float64, b []float64) ([]float64, error) {
	n := len(b)
	aug := make([][]float64, n)
	for i := 0; i < n; i++ {
		aug[i] = make([]float64, n+1)
		copy(aug[i], a[i])
		aug[i][n] = b[i]
	}
	for col := 0; col < n; col++ {
		pivot := col
		for row := col + 1; row < n; row++ {
			if math.Abs(aug[row][col]) > math.Abs(aug[pivot][col]) {
				pivot = row
			}
		}
		if math.Abs(aug[pivot][col]) < 1e-12 {
			return nil, fmt.Errorf("singular matrix")
		}
		aug[col], aug[pivot] = aug[pivot], aug[col]
		div := aug[col][col]
		for j := col; j <= n; j++ {
			aug[col][j] /= div
		}
		for row := 0; row < n; row++ {
			if row == col {
				continue
			}
			factor := aug[row][col]
			for j := col; j <= n; j++ {
				aug[row][j] -= factor * aug[col][j]
			}
		}
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = aug[i][n]
	}
	return out, nil
}

func crossValidateBandScales(samples []sample, folds int) (float64, [3]float64) {
	var totalErr float64
	var totalCount int
	final := fitBandScales(samples)
	for fold := 0; fold < folds; fold++ {
		var train, test []sample
		for i, s := range samples {
			if i%folds == fold {
				test = append(test, s)
			} else {
				train = append(train, s)
			}
		}
		scales := fitBandScales(train)
		for _, s := range test {
			pred := applyBandScale(s.CurrentRate, s.RealOversub, scales)
			totalErr += math.Abs(pred - s.ActualRate)
			totalCount++
		}
	}
	if totalCount == 0 {
		return 0, final
	}
	return totalErr / float64(totalCount), final
}

func fitBandScales(samples []sample) [3]float64 {
	var sumActual [3]float64
	var sumPred [3]float64
	for _, s := range samples {
		idx := bandIndex(s.RealOversub)
		sumActual[idx] += s.ActualRate
		sumPred[idx] += s.CurrentRate
	}
	out := [3]float64{1, 1, 1}
	for i := range out {
		if sumPred[i] > 0 {
			out[i] = sumActual[i] / sumPred[i]
		}
	}
	return out
}

func applyBandScale(pred, realOversub float64, scales [3]float64) float64 {
	v := pred * scales[bandIndex(realOversub)]
	if v < 1 {
		return 1
	}
	if v > 100 {
		return 100
	}
	return v
}

func bandIndex(realOversub float64) int {
	switch {
	case realOversub <= 10:
		return 0
	case realOversub <= 80:
		return 1
	default:
		return 2
	}
}
