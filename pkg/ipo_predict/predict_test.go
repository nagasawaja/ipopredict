package ipo_predict

import "testing"

func TestAllocateApplicantsByWeight_MinEachPositive(t *testing.T) {
	weights := []float64{100, 1, 1, 1}
	got := allocateApplicantsByWeight(8, weights, true)
	if len(got) != len(weights) {
		t.Fatalf("len(got)=%d want=%d", len(got), len(weights))
	}
	sum := 0
	for i := range got {
		sum += got[i]
		if got[i] <= 0 {
			t.Fatalf("bucket[%d] got=%d, want > 0 with minEachPositive", i, got[i])
		}
	}
	if sum != 8 {
		t.Fatalf("sum=%d want=8", sum)
	}
}

func TestPredict09981_BGroupAllBucketsHaveApplicants(t *testing.T) {
	publicShares := int64(13999000)
	lotSize := 200
	price := 20.09
	subscriptionMultiple := 569.58

	req := MarginRequest{
		PublicShares:    publicShares,
		LotSize:         lotSize,
		Price:           price,
		BrokerMarginSum: inferBrokerMarginSum(float64(publicShares)*price, subscriptionMultiple),
		Buckets:         buildDefaultBuckets(lotSize, price),
	}
	res, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}

	bCount := 0
	for _, w := range res.WinRates {
		if w.Group != "乙组" {
			continue
		}
		bCount++
		if w.Applicants <= 0 {
			t.Fatalf("乙组档位 lots=%d applicants=%d, want > 0", w.Lots, w.Applicants)
		}
	}
	if bCount == 0 {
		t.Fatalf("no B-group tiers predicted")
	}
}

func TestPredict_BGroupApplicantRatioOverride(t *testing.T) {
	req := MarginRequest{
		PublicShares:    25_000_000,
		LotSize:         100,
		Price:           10,
		BrokerMarginSum: 8_000_000_000,
		Buckets: []Tier{
			{Lots: 100, AmountHKD: 1_000},
			{Lots: 1_000, AmountHKD: 10_000},
			{Lots: 100_000, AmountHKD: 1_000_000},
		},
	}
	base, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() base error: %v", err)
	}
	req.BGroupApplicantRatio = 0.2
	highB, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() highB error: %v", err)
	}

	baseB := 0
	highBB := 0
	for _, w := range base.WinRates {
		if w.Group == "乙组" {
			baseB += w.Applicants
		}
	}
	for _, w := range highB.WinRates {
		if w.Group == "乙组" {
			highBB += w.Applicants
		}
	}
	if highBB <= baseB {
		t.Fatalf("override not effective: baseB=%d highB=%d", baseB, highBB)
	}
	if !highB.Meta.UsedBGroupApplicantRatioOverride {
		t.Fatalf("expected b-group override meta flag")
	}
}

func TestPredict_AOneHandApplicantRatioOverride(t *testing.T) {
	req := MarginRequest{
		PublicShares:    25_000_000,
		LotSize:         100,
		Price:           10,
		BrokerMarginSum: 8_000_000_000,
		Buckets: []Tier{
			{Lots: 100, AmountHKD: 1_000},
			{Lots: 200, AmountHKD: 2_000},
			{Lots: 500, AmountHKD: 5_000},
			{Lots: 1_000, AmountHKD: 10_000},
			{Lots: 100_000, AmountHKD: 1_000_000},
		},
	}
	lowAOne, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() lowAOne error: %v", err)
	}
	req.AOneHandApplicantRatio = 0.9
	highAOne, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() highAOne error: %v", err)
	}

	lowOneLotA := 0
	highOneLotA := 0
	for _, w := range lowAOne.WinRates {
		if w.Group == "甲组" && w.Lots == int64(req.LotSize) {
			lowOneLotA += w.Applicants
		}
	}
	for _, w := range highAOne.WinRates {
		if w.Group == "甲组" && w.Lots == int64(req.LotSize) {
			highOneLotA += w.Applicants
		}
	}
	if highOneLotA <= lowOneLotA {
		t.Fatalf("override not effective: lowOneLotA=%d highOneLotA=%d", lowOneLotA, highOneLotA)
	}
	if !highAOne.Meta.UsedAOneHandApplicantOverride {
		t.Fatalf("expected a-one-hand override meta flag")
	}
}

func TestPredict_EstimatedApplicantsOverride(t *testing.T) {
	req := MarginRequest{
		PublicShares:    25_000_000,
		LotSize:         100,
		Price:           10,
		BrokerMarginSum: 8_000_000_000,
		Buckets: []Tier{
			{Lots: 100, AmountHKD: 1_000},
			{Lots: 1_000, AmountHKD: 10_000},
			{Lots: 100_000, AmountHKD: 1_000_000},
		},
		EstimatedApplicantsOverride: 1234,
	}
	res, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}
	totalApplicants := 0
	for _, w := range res.WinRates {
		totalApplicants += w.Applicants
	}
	if totalApplicants != 1234 {
		t.Fatalf("total applicants=%d want=1234", totalApplicants)
	}
	if !res.Meta.UsedEstimatedApplicantsOverride {
		t.Fatalf("expected estimated applicants override meta flag")
	}
	if res.Meta.FinalEstimatedApplicants != 1234 {
		t.Fatalf("final estimated applicants=%d want=1234", res.Meta.FinalEstimatedApplicants)
	}
}

func TestPredict_AutoEstimateContextShrinksApplicants(t *testing.T) {
	req := MarginRequest{
		PublicShares:    25_000_000,
		LotSize:         100,
		Price:           10,
		BrokerMarginSum: 8_000_000_000,
		Buckets: []Tier{
			{Lots: 100, AmountHKD: 1_000},
			{Lots: 1_000, AmountHKD: 10_000},
			{Lots: 100_000, AmountHKD: 1_000_000},
		},
	}
	base, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() base error: %v", err)
	}
	req.AutoEstimateContext = &AutoEstimateContext{
		ComparablePeerCount:                    40,
		ComparableApplicantMedian:              100,
		ComparableApplicantP10:                 90,
		ComparableApplicantP90:                 110,
		ComparableBGroupApplicantRatioMedian:   0.08,
		ComparableAOneHandApplicantRatioMedian: 0.25,
	}
	withPrior, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() with prior error: %v", err)
	}
	if withPrior.Meta.AutoEstimatedApplicants >= base.Meta.AutoEstimatedApplicants {
		t.Fatalf("expected prior to shrink applicants: base=%d prior=%d", base.Meta.AutoEstimatedApplicants, withPrior.Meta.AutoEstimatedApplicants)
	}
	if withPrior.Meta.AutoEstimatedApplicants < 72 || withPrior.Meta.AutoEstimatedApplicants > 132 {
		t.Fatalf("auto estimated applicants=%d outside expected clamp range", withPrior.Meta.AutoEstimatedApplicants)
	}
	if withPrior.Meta.FinalBGroupApplicantRatio <= DefaultBGroupApplicantRatio() {
		t.Fatalf("expected auto b ratio to move above default, got %.4f", withPrior.Meta.FinalBGroupApplicantRatio)
	}
	if withPrior.Meta.FinalAOneHandApplicantRatio >= DefaultAOneHandApplicantRatio() {
		t.Fatalf("expected auto a-one-hand ratio to move below default, got %.4f", withPrior.Meta.FinalAOneHandApplicantRatio)
	}
}

func TestPredict01609ExtremeOversubBGroupHouseholdRateMonotonic(t *testing.T) {
	req := MarginRequest{
		PublicShares:    842_200,
		LotSize:         50,
		Price:           98.5,
		BrokerMarginSum: inferBrokerMarginSum(float64(842_200)*98.5, 5000),
		AutoEstimateContext: &AutoEstimateContext{
			ComparablePeerCount:                    40,
			ComparableApplicantMedian:              160_949,
			ComparableApplicantP10:                 24_625,
			ComparableApplicantP90:                 260_504,
			ComparableBGroupApplicantRatioMedian:   0.0592861932924997,
			ComparableAOneHandApplicantRatioMedian: 0.48051023516678076,
			OverlapStockCount:                      2,
			OverlapPublicLotsSum:                   69_627,
			OverlapEntryFeeSum:                     6_483.74,
			OverlapLowEntryCount:                   2,
			OverlapSmallPublicLotsCount:            2,
		},
		Buckets: []Tier{
			{Lots: 50, AmountHKD: 4_974.67},
			{Lots: 100, AmountHKD: 9_949.34},
			{Lots: 150, AmountHKD: 14_924},
			{Lots: 200, AmountHKD: 19_898.67},
			{Lots: 250, AmountHKD: 24_873.34},
			{Lots: 300, AmountHKD: 29_848.01},
			{Lots: 350, AmountHKD: 34_822.68},
			{Lots: 400, AmountHKD: 39_797.35},
			{Lots: 450, AmountHKD: 44_772.02},
			{Lots: 500, AmountHKD: 49_746.68},
			{Lots: 600, AmountHKD: 59_696.03},
			{Lots: 700, AmountHKD: 69_645.36},
			{Lots: 800, AmountHKD: 79_594.7},
			{Lots: 900, AmountHKD: 89_544.03},
			{Lots: 1_000, AmountHKD: 99_493.38},
			{Lots: 1_500, AmountHKD: 149_240.06},
			{Lots: 2_000, AmountHKD: 198_986.75},
			{Lots: 2_500, AmountHKD: 248_733.43},
			{Lots: 3_000, AmountHKD: 298_480.12},
			{Lots: 3_500, AmountHKD: 348_226.81},
			{Lots: 4_000, AmountHKD: 397_973.49},
			{Lots: 4_500, AmountHKD: 447_720.17},
			{Lots: 5_000, AmountHKD: 497_466.87},
			{Lots: 10_000, AmountHKD: 994_933.73},
			{Lots: 15_000, AmountHKD: 1_492_400.59},
			{Lots: 20_000, AmountHKD: 1_989_867.46},
			{Lots: 25_000, AmountHKD: 2_487_334.31},
			{Lots: 30_000, AmountHKD: 2_984_801.18},
			{Lots: 35_000, AmountHKD: 3_482_268.03},
			{Lots: 40_000, AmountHKD: 3_979_734.9},
			{Lots: 45_000, AmountHKD: 4_477_201.77},
			{Lots: 50_000, AmountHKD: 4_974_668.63},
			{Lots: 60_000, AmountHKD: 5_969_602.36},
			{Lots: 70_000, AmountHKD: 6_964_536.08},
			{Lots: 80_000, AmountHKD: 7_959_469.8},
			{Lots: 90_000, AmountHKD: 8_954_403.53},
			{Lots: 100_000, AmountHKD: 9_949_337.26},
			{Lots: 150_000, AmountHKD: 14_924_005.88},
			{Lots: 200_000, AmountHKD: 19_898_674.5},
			{Lots: 250_000, AmountHKD: 24_873_343.13},
			{Lots: 300_000, AmountHKD: 29_848_011.76},
			{Lots: 350_000, AmountHKD: 34_822_680.38},
			{Lots: 421_100, AmountHKD: 41_896_659.17},
		},
	}
	res, err := Predict(req)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}

	var prev *WinRateInfo
	var b60000, b70000 *WinRateInfo
	for i := range res.WinRates {
		w := &res.WinRates[i]
		if w.Group != "乙组" {
			continue
		}
		if prev != nil && w.WinRate+1e-12 < prev.WinRate {
			t.Fatalf("B-group household win rate decreased: lots %d rate %.6f after lots %d rate %.6f", w.Lots, w.WinRate, prev.Lots, prev.WinRate)
		}
		if w.Lots == 60_000 {
			b60000 = w
		}
		if w.Lots == 70_000 {
			b70000 = w
		}
		prev = w
	}
	if b60000 == nil || b70000 == nil {
		t.Fatalf("missing 60000 or 70000 B-group tiers")
	}
	if b60000.Applicants > b70000.Applicants*5 {
		t.Fatalf("B-head applicants over-concentrated: 60000=%d 70000=%d", b60000.Applicants, b70000.Applicants)
	}
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

func buildDefaultBuckets(lotSize int, price float64) []Tier {
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
	out := make([]Tier, 0, len(mults))
	for _, m := range mults {
		shares := int64(lotSize) * m
		if shares <= 0 {
			continue
		}
		out = append(out, Tier{
			Lots:      shares,
			AmountHKD: float64(shares) * price,
		})
	}
	return out
}
