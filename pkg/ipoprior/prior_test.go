package ipoprior

import (
	"testing"
	"time"
)

func mustDate(t *testing.T, v string) time.Time {
	t.Helper()
	got, err := time.Parse("2006-01-02", v)
	if err != nil {
		t.Fatalf("parse date %q: %v", v, err)
	}
	return got
}

func TestBuildAutoEstimateContext_UsesHistoricalPeersAndOverlap(t *testing.T) {
	current := StockFeature{
		ID:             100,
		Code:           "CURRENT",
		ApplyStartDate: mustDate(t, "2025-03-10"),
		ApplyEndDate:   mustDate(t, "2025-03-14"),
		PublicLots:     50000,
		PerLotMoney:    6000,
	}
	peers := []StockFeature{
		current,
		{
			ID:                    1,
			Code:                  "P1",
			ApplyStartDate:        mustDate(t, "2025-02-20"),
			ApplyEndDate:          mustDate(t, "2025-03-01"),
			PublicLots:            52000,
			PerLotMoney:           6100,
			SubscriptionMultiple:  130,
			ActualTotalApplicants: 100,
			ActualBRatio:          0.04,
			ActualAOneLotRatio:    0.30,
		},
		{
			ID:                    2,
			Code:                  "P2",
			ApplyStartDate:        mustDate(t, "2025-02-25"),
			ApplyEndDate:          mustDate(t, "2025-03-09"),
			PublicLots:            48000,
			PerLotMoney:           5900,
			SubscriptionMultiple:  90,
			ActualTotalApplicants: 200,
			ActualBRatio:          0.08,
			ActualAOneLotRatio:    0.25,
		},
		{
			ID:                    3,
			Code:                  "OVERLAP_ONLY",
			ApplyStartDate:        mustDate(t, "2025-03-11"),
			ApplyEndDate:          mustDate(t, "2025-03-16"),
			PublicLots:            80000,
			PerLotMoney:           4500,
			AdmissionFeeHKD:       4800,
			SubscriptionMultiple:  1000,
			ActualTotalApplicants: 999,
			ActualBRatio:          0.15,
			ActualAOneLotRatio:    0.20,
		},
	}

	ctx := BuildAutoEstimateContext(peers, current, 120)
	if ctx.ComparablePeerCount != 2 {
		t.Fatalf("ComparablePeerCount=%d want=2", ctx.ComparablePeerCount)
	}
	if ctx.ComparableApplicantMedian != 150 {
		t.Fatalf("ComparableApplicantMedian=%d want=150", ctx.ComparableApplicantMedian)
	}
	if ctx.ComparableApplicantP10 != 110 {
		t.Fatalf("ComparableApplicantP10=%d want=110", ctx.ComparableApplicantP10)
	}
	if ctx.ComparableApplicantP90 != 190 {
		t.Fatalf("ComparableApplicantP90=%d want=190", ctx.ComparableApplicantP90)
	}
	if ctx.OverlapStockCount != 1 {
		t.Fatalf("OverlapStockCount=%d want=1", ctx.OverlapStockCount)
	}
	if ctx.OverlapLowEntryCount != 1 {
		t.Fatalf("OverlapLowEntryCount=%d want=1", ctx.OverlapLowEntryCount)
	}
	if ctx.OverlapSmallPublicLotsCount != 1 {
		t.Fatalf("OverlapSmallPublicLotsCount=%d want=1", ctx.OverlapSmallPublicLotsCount)
	}
}
