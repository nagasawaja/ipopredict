# ipo_predict（中文说明）

港股 IPO 公开发售中签率预测库，**孖展驱动**：以孖展总额为入口，内部推算倍数与人数，输出各档中签率。

## 安装 / 引用

本仓库是 Go module，直接引用：

```
import "hk_ipo/pkg/ipo_predict"
```

## 快速上手

```go
package main

import (
	"fmt"

	"hk_ipo/pkg/ipo_predict"
)

func main() {
	req := ipo_predict.MarginRequest{
		PublicShares:    25_000_000,
		LotSize:        100,
		Price:          50.0,
		BrokerMarginSum: 30_000_000_000, // 300 亿孖展
		Buckets: []ipo_predict.Tier{
			{Lots: 1, AmountHKD: 5000},
			{Lots: 10, AmountHKD: 50000},
			{Lots: 100, AmountHKD: 5_000_000},
			{Lots: 200, AmountHKD: 10_000_000},
		},
	}

	result, err := ipo_predict.Predict(req)
	if err != nil {
		panic(err)
	}

	for _, wr := range result.WinRates {
		fmt.Printf("%s %d手: 中签率=%.4f, 申请人数=%d\n",
			wr.Group, wr.Lots, wr.WinRate, wr.Applicants)
	}
}
```

## 入参（MarginRequest）

唯一入口为 `Predict(req MarginRequest)`，入参与 margin.md 一致：

| 参数 | 类型 | 说明 |
|------|------|------|
| `PublicShares` | int64 | 公开发售总股数 |
| `LotSize` | int | 每手股数 |
| `Price` | float64 | 发行价（通常取上限） |
| `BrokerMarginSum` | float64 | 孖展总额（各券商孖展汇总） |
| `Buckets` | []Tier | 申购阶梯表（Lots + AmountHKD） |

**孖展倍数**不在入参中，由内部用「孖展总额 ÷ 募资额」推算，并据此选择孖展覆盖率、红鞋系数等。

## 输出（PredictResult）

- `Tiers`：各档位信息（含推算的申请人数、组别）
- `WinRates`：各档位中签率与分配信息

`WinRateInfo` 字段：Group、Lots、Applicants、SubscribedShares、AllocatedShares、WinRate、PerLotRate、WinApplicants、AllocatedLots、LotDistribution。

## 说明

- 本库不解析 PDF、不请求网页。
- 无回拨假设：甲乙组供应 50 : 50；乙组门槛为金额 > 500 万 HKD。
- 算法详见 `margin.md`。
