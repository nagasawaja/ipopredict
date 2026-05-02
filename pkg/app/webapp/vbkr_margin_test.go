package webapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVBrokerMarginClientFetchByStockCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ipo/hk-stock/query-applying", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": "00000",
			"data": {
				"applying": [{
					"ipoInfo": {
						"ipoId": 3861,
						"securityCode": "01236.HK",
						"securityName": "乐动机器人",
						"securityNameTc": "樂動機器人"
					}
				}]
			}
		}`))
	})
	mux.HandleFunc("/ipo/hk-stock/query-margin-brokers", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ipoId"); got != "3861" {
			t.Fatalf("ipoId=%q want 3861", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": "00000",
			"data": {
				"totalMarginMoney": 34583000000.00,
				"applyRate": "345.82",
				"raisingMoney": 100002000.00,
				"lastUpdateTime": "2026-05-02 21:10:00",
				"brokerList": [
					{"marginMoney": "24973000000.00", "brokerName": "富途证券"},
					{"marginMoney": "3910000.00", "brokerName": "艾德证券"}
				]
			}
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newVBrokerMarginClient(srv.Client())
	c.BaseURL = srv.URL

	got, err := c.fetchByStockCode(context.Background(), "1236")
	if err != nil {
		t.Fatalf("fetchByStockCode: %v", err)
	}
	if got.IPOID != 3861 {
		t.Fatalf("IPOID=%d want 3861", got.IPOID)
	}
	if got.SecurityCode != "01236.HK" {
		t.Fatalf("SecurityCode=%q want 01236.HK", got.SecurityCode)
	}
	if got.ApplyRate != 345.82 {
		t.Fatalf("ApplyRate=%v want 345.82", got.ApplyRate)
	}
	if got.TotalMarginMoney != 34583000000 {
		t.Fatalf("TotalMarginMoney=%v want 34583000000", got.TotalMarginMoney)
	}
	if len(got.Brokers) != 2 || got.Brokers[0].BrokerName != "富途证券" || got.Brokers[0].MarginMoney != 24973000000 {
		t.Fatalf("Brokers=%+v", got.Brokers)
	}
}

func TestNormalizeHKStockCode(t *testing.T) {
	tests := map[string]string{
		"1236":     "01236",
		"01236.HK": "01236",
		" 1609.hk": "01609",
		"abc":      "",
	}
	for in, want := range tests {
		if got := normalizeHKStockCode(in); got != want {
			t.Fatalf("normalizeHKStockCode(%q)=%q want %q", in, got, want)
		}
	}
}
