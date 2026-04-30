package webapp

import (
	"testing"

	"hk_ipo/pkg/storage/gormmodel"
)

func TestCalcHouseholdWinRatePct(t *testing.T) {
	tests := []struct {
		name       string
		applicants int
		winLots    int64
		remark     string
		want       float64
	}{
		{
			name:       "guaranteed_with_extra_clause",
			applicants: 21,
			winLots:    16,
			remark:     "加上21名中14名獲發額外200股股份",
			want:       100,
		},
		{
			name:       "guaranteed_with_lingjia_extra_clause",
			applicants: 1003,
			winLots:    1,
			remark:     "另加1003名申請人中的187名獲發額外100股H股",
			want:       100,
		},
		{
			name:       "guaranteed_with_qude_extra_clause",
			applicants: 2163,
			winLots:    5,
			remark:     "加2163名申請人中的1099名取得額外20股股份",
			want:       100,
		},
		{
			name:       "guaranteed_with_huode_extra_clause",
			applicants: 2462,
			winLots:    1,
			remark:     "加2462名中的1967名獲得額外100股股份",
			want:       100,
		},
		{
			name:       "normal_lottery_from_remark",
			applicants: 12869,
			winLots:    1,
			remark:     "12869名申請人中的2592名獲發100股H股",
			want:       float64(2592) * 100 / float64(12869),
		},
		{
			name:       "lottery_from_share_application_remark",
			applicants: 82364,
			winLots:    1,
			remark:     "82364份中有50份獲發200股股份",
			want:       float64(50) * 100 / float64(82364),
		},
		{
			name:       "lottery_from_applicant_word_shenqingzhe",
			applicants: 93509,
			winLots:    1,
			remark:     "93509名申請者中有1871名獲發100股H股",
			want:       float64(1871) * 100 / float64(93509),
		},
		{
			name:       "lottery_from_among_remark",
			applicants: 52515,
			winLots:    1,
			remark:     "其中314名獲發500股股份",
			want:       float64(314) * 100 / float64(52515),
		},
		{
			name:       "fallback_winlots_ge_2",
			applicants: 107,
			winLots:    14,
			remark:     "",
			want:       100,
		},
		{
			name:       "fallback_winlots_lt_2",
			applicants: 107,
			winLots:    1,
			remark:     "",
			want:       0,
		},
	}

	for _, tt := range tests {
		got := calcHouseholdWinRatePct(tt.applicants, tt.winLots, tt.remark)
		if diff := got - tt.want; diff < -1e-9 || diff > 1e-9 {
			t.Fatalf("%s: got=%v want=%v", tt.name, got, tt.want)
		}
	}
}

func TestCalcTierHouseholdWinRatePct(t *testing.T) {
	got := calcTierHouseholdWinRatePct(5704, 1, "", 4000, 20, 0.5)
	if got != 100 {
		t.Fatalf("guaranteed empty remark tier: got=%v want=100", got)
	}

	got = calcTierHouseholdWinRatePct(5704, 1, "", 4000, 20, 0.4)
	if got != 0 {
		t.Fatalf("non-guaranteed empty remark tier: got=%v want=0", got)
	}
}

func TestDetailAllotmentTierPrice(t *testing.T) {
	got := detailAllotmentTierPrice(
		&gormmodel.StockAllotmentSummary{OfferPrice: 0, OfferPriceHigh: 12.34},
		&gormmodel.StockOffering{OfferPrice: 10.5, OfferPriceHigh: 11},
	)
	if got != 12.34 {
		t.Fatalf("fallback to summary high price: got=%v want=12.34", got)
	}

	got = detailAllotmentTierPrice(
		&gormmodel.StockAllotmentSummary{OfferPrice: 8.88, OfferPriceHigh: 12.34},
		&gormmodel.StockOffering{OfferPrice: 10.5, OfferPriceHigh: 11},
	)
	if got != 8.88 {
		t.Fatalf("use final summary price first: got=%v want=8.88", got)
	}

	got = detailAllotmentTierPrice(nil, &gormmodel.StockOffering{OfferPrice: 0, OfferPriceHigh: 9.99})
	if got != 9.99 {
		t.Fatalf("fallback to offering high price: got=%v want=9.99", got)
	}
}

func TestCalcAllotmentTierAmountHKD(t *testing.T) {
	got := calcAllotmentTierAmountHKD(500, 12.34)
	want := 500 * 12.34 * ipoSubscriptionFeeMultiplier
	if diff := got - want; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("got=%v want=%v", got, want)
	}

	got = calcAllotmentTierAmountHKD(500, 0)
	if got != 0 {
		t.Fatalf("zero price: got=%v want=0", got)
	}
}
