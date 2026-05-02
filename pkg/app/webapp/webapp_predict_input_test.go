package webapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

func TestParsePredictInputPostJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/predict/01879", strings.NewReader(`{
		"sub": 5001,
		"margin": "",
		"estimatedApplicants": 12345,
		"bRatio": "3",
		"aOneLotRatio": "0.42"
	}`))
	got, err := parsePredictInput(r)
	if err != nil {
		t.Fatalf("parsePredictInput: %v", err)
	}
	if got.Sub != "5001" {
		t.Fatalf("Sub=%q want 5001", got.Sub)
	}
	if got.Margin != "" {
		t.Fatalf("Margin=%q want empty", got.Margin)
	}
	if got.EstimatedApplicants != "12345" {
		t.Fatalf("EstimatedApplicants=%q want 12345", got.EstimatedApplicants)
	}
	if got.BRatio != "3" {
		t.Fatalf("BRatio=%q want 3", got.BRatio)
	}
	if got.AOneLotRatio != "0.42" {
		t.Fatalf("AOneLotRatio=%q want 0.42", got.AOneLotRatio)
	}
}

func TestParsePredictInputGetDefaultsToEmpty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/predict/01879?sub=5001", nil)
	got, err := parsePredictInput(r)
	if err != nil {
		t.Fatalf("parsePredictInput: %v", err)
	}
	if got != (predictInputPayload{}) {
		t.Fatalf("got=%+v want empty payload", got)
	}
}

func TestResolveDefaultPredictSubscriptionMultiple(t *testing.T) {
	tests := []struct {
		name   string
		public float64
		want   float64
	}{
		{name: "use_public_subscription_multiple", public: 523.45, want: 523.45},
		{name: "fallback_to_1000_when_missing", public: 0, want: 1000},
		{name: "fallback_to_1000_when_invalid", public: -1, want: 1000},
	}
	for _, tt := range tests {
		got := resolveDefaultPredictSubscriptionMultiple(tt.public)
		if got != tt.want {
			t.Fatalf("%s: got=%v want=%v", tt.name, got, tt.want)
		}
	}
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

func TestFormatHKDMoney(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{in: 34583000000, want: "345.83 亿"},
		{in: 100002000, want: "1.00 亿"},
		{in: 3910000, want: "391.00 万"},
		{in: 390000, want: "39.00 万"},
	}
	for _, tt := range tests {
		if got := formatHKDMoney(tt.in); got != tt.want {
			t.Fatalf("formatHKDMoney(%v)=%q want %q", tt.in, got, tt.want)
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
