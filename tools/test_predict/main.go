package main

import (
	"encoding/json"
	"fmt"
	"log"

	"hk_ipo/pkg/ipo_predict"
)

func main() {
	raw := `{"PublicShares":2772300,"LotSize":100,"Price":43.24,"BrokerMarginSum":130895490728.88,"Buckets":[{"Lots":100,"AmountHKD":4324},{"Lots":200,"AmountHKD":8648},{"Lots":300,"AmountHKD":12972},{"Lots":400,"AmountHKD":17296},{"Lots":500,"AmountHKD":21620},{"Lots":600,"AmountHKD":25944},{"Lots":700,"AmountHKD":30268},{"Lots":800,"AmountHKD":34592},{"Lots":900,"AmountHKD":38916},{"Lots":1000,"AmountHKD":43240},{"Lots":1500,"AmountHKD":64860},{"Lots":2000,"AmountHKD":86480},{"Lots":2500,"AmountHKD":108100},{"Lots":3000,"AmountHKD":129720},{"Lots":3500,"AmountHKD":151340},{"Lots":4000,"AmountHKD":172960},{"Lots":4500,"AmountHKD":194580},{"Lots":5000,"AmountHKD":216200},{"Lots":6000,"AmountHKD":259440},{"Lots":7000,"AmountHKD":302680},{"Lots":8000,"AmountHKD":345920},{"Lots":9000,"AmountHKD":389160},{"Lots":10000,"AmountHKD":432400},{"Lots":20000,"AmountHKD":864800},{"Lots":30000,"AmountHKD":1297200},{"Lots":40000,"AmountHKD":1729600},{"Lots":50000,"AmountHKD":2162000},{"Lots":60000,"AmountHKD":2594400},{"Lots":70000,"AmountHKD":3026800},{"Lots":80000,"AmountHKD":3459200},{"Lots":90000,"AmountHKD":3891600},{"Lots":100000,"AmountHKD":4324000},{"Lots":200000,"AmountHKD":8648000},{"Lots":300000,"AmountHKD":12972000},{"Lots":400000,"AmountHKD":17296000},{"Lots":500000,"AmountHKD":21620000},{"Lots":600000,"AmountHKD":25944000},{"Lots":700000,"AmountHKD":30268000},{"Lots":800000,"AmountHKD":34592000},{"Lots":900000,"AmountHKD":38916000},{"Lots":1000000,"AmountHKD":43240000},{"Lots":1386100,"AmountHKD":59934964}]}`

	var req ipo_predict.MarginRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		log.Fatalf("unmarshal: %v", err)
	}

	result, err := ipo_predict.Predict(req)
	if err != nil {
		log.Fatalf("Predict: %v", err)
	}

	fmt.Printf("Tiers: %d, WinRates: %d\n", len(result.Tiers), len(result.WinRates))
	for i, wr := range result.WinRates {
		fmt.Printf("%d %s %d手: WinRate=%.4f Applicants=%d AllocatedShares=%d %s\n",
			i+1, wr.Group, wr.Lots, wr.WinRate, wr.Applicants, wr.AllocatedShares, wr.LotDistribution)
	}
}
