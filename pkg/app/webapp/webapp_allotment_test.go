package webapp

import "testing"

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
			name:       "normal_lottery_from_remark",
			applicants: 12869,
			winLots:    1,
			remark:     "12869名申請人中的2592名獲發100股H股",
			want:       float64(2592) * 100 / float64(12869),
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
