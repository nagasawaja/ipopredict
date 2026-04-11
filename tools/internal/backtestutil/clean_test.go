package backtestutil

import "testing"

func TestCheckCompleteness(t *testing.T) {
	c := Candidate{
		PublicShares:         1000,
		LotSize:              100,
		Price:                10,
		SubscriptionMultiple: 5,
		OneLotWinRatePct:     12,
		ProspectusURL:        "https://example.com/a.pdf",
	}
	tiers := []TierRow{
		{Seq: 1, Lots: 100},
		{Seq: 2, Lots: 200},
	}
	if got := checkCompleteness(c, tiers); got != ReasonOK {
		t.Fatalf("checkCompleteness()=%s want=%s", got, ReasonOK)
	}
}

func TestCheckCompletenessMissingOneLot(t *testing.T) {
	c := Candidate{
		PublicShares:         1000,
		LotSize:              100,
		Price:                10,
		SubscriptionMultiple: 5,
		OneLotWinRatePct:     12,
		ProspectusURL:        "https://example.com/a.pdf",
	}
	tiers := []TierRow{
		{Seq: 1, Lots: 200},
	}
	if got := checkCompleteness(c, tiers); got != ReasonMissingOneLotTier {
		t.Fatalf("checkCompleteness()=%s want=%s", got, ReasonMissingOneLotTier)
	}
}

func TestEqualLots(t *testing.T) {
	if !equalLots([]int64{100, 200}, []int64{100, 200}) {
		t.Fatalf("equalLots should be true")
	}
	if equalLots([]int64{100, 200}, []int64{100, 300}) {
		t.Fatalf("equalLots should be false")
	}
}
