package webapp

import "testing"

func TestMinWinLotsExamples(t *testing.T) {
	if got := calcMinWinLots(23226, 1967, 1967); got != 1 {
		t.Fatalf("case1: got=%d want=1", got)
	}
	// 114名申请人获发2手，另加38名获发额外1手 => 总手数 266，最小中签数应为 2手
	if got := calcMinWinLots(114, 114, 266); got != 2 {
		t.Fatalf("case2: got=%d want=2", got)
	}
}
