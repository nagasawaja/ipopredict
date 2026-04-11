package webapp

import "testing"

func TestResolvePredictInputs(t *testing.T) {
	fundraising := 100.0

	t.Run("prefer_sub_when_present", func(t *testing.T) {
		subRaw := "20"
		marginRaw := "999"
		subInput, err := parsePositiveFloatQuery(subRaw)
		if err != nil {
			t.Fatalf("parse sub: %v", err)
		}
		marginInput, err := parsePositiveFloatQuery(marginRaw)
		if err != nil {
			t.Fatalf("parse margin: %v", err)
		}

		effectiveSub := 0.0
		brokerMarginSum := 0.0
		inputSubText := ""
		inputMarginText := ""
		switch {
		case subRaw != "":
			effectiveSub = subInput
			brokerMarginSum = inferBrokerMarginSumFromSubscriptionMultiple(fundraising, subInput)
			inputSubText = "20"
		case marginRaw != "":
			brokerMarginSum = marginInput
			effectiveSub = inferRealSubscriptionMultipleFromBrokerMarginSum(fundraising, marginInput)
			inputMarginText = "999"
		}

		if effectiveSub != 20 {
			t.Fatalf("effectiveSub=%v want 20", effectiveSub)
		}
		if brokerMarginSum != 700 {
			t.Fatalf("brokerMarginSum=%v want 700", brokerMarginSum)
		}
		if inputSubText != "20" {
			t.Fatalf("inputSubText=%q want 20", inputSubText)
		}
		if inputMarginText != "" {
			t.Fatalf("inputMarginText=%q want empty", inputMarginText)
		}
	})

	t.Run("use_margin_when_sub_absent", func(t *testing.T) {
		subRaw := ""
		marginRaw := "4000"
		subInput, err := parsePositiveFloatQuery(subRaw)
		if err != nil {
			t.Fatalf("parse sub: %v", err)
		}
		marginInput, err := parsePositiveFloatQuery(marginRaw)
		if err != nil {
			t.Fatalf("parse margin: %v", err)
		}

		effectiveSub := 0.0
		brokerMarginSum := 0.0
		inputSubText := ""
		inputMarginText := ""
		switch {
		case subRaw != "":
			effectiveSub = subInput
			brokerMarginSum = inferBrokerMarginSumFromSubscriptionMultiple(fundraising, subInput)
			inputSubText = subRaw
		case marginRaw != "":
			brokerMarginSum = marginInput
			effectiveSub = inferRealSubscriptionMultipleFromBrokerMarginSum(fundraising, marginInput)
			inputMarginText = marginRaw
		}

		if effectiveSub != 80 {
			t.Fatalf("effectiveSub=%v want 80", effectiveSub)
		}
		if brokerMarginSum != 4000 {
			t.Fatalf("brokerMarginSum=%v want 4000", brokerMarginSum)
		}
		if inputSubText != "" {
			t.Fatalf("inputSubText=%q want empty", inputSubText)
		}
		if inputMarginText != "4000" {
			t.Fatalf("inputMarginText=%q want 4000", inputMarginText)
		}
	})
}

func TestInferBrokerMarginSumFromSubscriptionMultiple(t *testing.T) {
	fundraising := 100.0
	tests := []struct {
		name        string
		realOversub float64
		want        float64
	}{
		{name: "low_oversub", realOversub: 10, want: 350},
		{name: "mid_oversub", realOversub: 80, want: 4000},
		{name: "high_oversub", realOversub: 500, want: 49000},
	}
	for _, tt := range tests {
		got := inferBrokerMarginSumFromSubscriptionMultiple(fundraising, tt.realOversub)
		if got != tt.want {
			t.Fatalf("%s: got %.2f want %.2f", tt.name, got, tt.want)
		}
	}
}

func TestInferBrokerMarginSumFromSubscriptionMultipleInvalid(t *testing.T) {
	if got := inferBrokerMarginSumFromSubscriptionMultiple(0, 10); got != 0 {
		t.Fatalf("fundraising=0: got %.2f want 0", got)
	}
	if got := inferBrokerMarginSumFromSubscriptionMultiple(100, 0); got != 0 {
		t.Fatalf("realOversub=0: got %.2f want 0", got)
	}
}

func TestInferRealSubscriptionMultipleFromBrokerMarginSum(t *testing.T) {
	tests := []struct {
		name      string
		fundraise float64
		margin    float64
		want      float64
	}{
		{name: "low_oversub", fundraise: 100, margin: 350, want: 10},
		{name: "mid_oversub", fundraise: 100, margin: 4000, want: 80},
		{name: "high_oversub", fundraise: 100, margin: 49000, want: 500},
	}
	for _, tt := range tests {
		got := inferRealSubscriptionMultipleFromBrokerMarginSum(tt.fundraise, tt.margin)
		if got != tt.want {
			t.Fatalf("%s: got %.2f want %.2f", tt.name, got, tt.want)
		}
	}
}

func TestParsePositiveIntQuery(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{in: "", want: 0},
		{in: "123", want: 123},
		{in: "0", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "1.2", wantErr: true},
		{in: "abc", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parsePositiveIntQuery(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("in=%q: want error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("in=%q: unexpected error=%v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("in=%q: got=%d want=%d", tt.in, got, tt.want)
		}
	}
}

func TestBuildDefaultBuckets_CapAtHalfPublicShares(t *testing.T) {
	lotSize := 200
	price := 20.09
	publicShares := int64(13999000)
	buckets := buildDefaultBuckets(lotSize, price, publicShares)
	if len(buckets) == 0 {
		t.Fatalf("buildDefaultBuckets returned empty")
	}
	capShares := (publicShares / 2 / int64(lotSize)) * int64(lotSize)
	for _, b := range buckets {
		if b.Lots > capShares {
			t.Fatalf("bucket lots=%d > cap=%d", b.Lots, capShares)
		}
	}
	last := buckets[len(buckets)-1]
	if last.Lots != capShares {
		t.Fatalf("last bucket lots=%d want cap=%d", last.Lots, capShares)
	}
}

func TestParseRatioQuery(t *testing.T) {
	tests := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{in: "", want: 0},
		{in: "0.03", want: 0.03},
		{in: "3", want: 0.03},
		{in: "100", want: 1},
		{in: "0", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "101", wantErr: true},
		{in: "abc", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseRatioQuery(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("in=%q: want error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("in=%q: unexpected error=%v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("in=%q: got=%v want=%v", tt.in, got, tt.want)
		}
	}
}
