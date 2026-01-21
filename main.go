package main

import (
	"fmt"
	"math"
)

var currentIPO = struct {
	StockName         string  // 股票名称（含代码）
	PriceMax          float64 // 最高发行价（HKD）
	LotSize           int     // 每手股数
	TotalApplicants   int     // 总申请人数
	BGroupApplicants  int     // 乙组申请人数（0表示自动计算为总人数的5%）
	PublicOfferShares float64 // 公開發售股數（单位：万股，计算时自动换算为股数）
	OneLotRate        float64 // 一手档人数占比（默认0.40，即40%）
	ATailRate         float64 // 甲尾人数占比（默认0.02，即总人数的2%）
	BHeadRate         float64 // 乙头人数占比（默认0.03，即总人数的3%）
}{StockName: "兆易創新 (03986.HK)", PriceMax: 162.0, LotSize: 100, TotalApplicants: 148419, BGroupApplicants: 0, PublicOfferShares: 289.16, OneLotRate: 0.34, ATailRate: 0.02, BHeadRate: 0.03}

func getValidLots(priceMax float64, lotSize int) (aTail, bHead int64) {
	oneLotPrice := priceMax * float64(lotSize)
	var lastLots int64
	steps := []struct{ limit, step int64 }{{10, 1}, {100, 10}, {1000, 100}, {10000, 500}, {100000, 1000}}
	for _, s := range steps {
		for i := lastLots + s.step; i <= s.limit; i += s.step {
			if cost := float64(i) * oneLotPrice; cost > 5000000 {
				return lastLots, i
			}
			lastLots = i
		}
	}
	return
}

// TierInfo 存储每个档位的信息
type TierInfo struct {
	Lots       int64
	Applicants int
	Group      string
}

// WinRateInfo 存储中签率信息
type WinRateInfo struct {
	Lots             int64
	Applicants       int // 申请人数
	Group            string
	SubscribedShares int64   // 申购总股数
	AllocatedShares  int64   // 分配到的股数
	WinRate          float64 // 每手中签率（分配股数/申购股数）
	WinApplicants    int     // 中签人数
	AllocatedLots    int64   // 中签手数（整数）
	LotDistribution  string  // 手数分配详情（如"X人中n手，Y人中m手"）
}

// formatLotDistribution 格式化手数分配详情
// 格式：X人中n手，另外Y人多分配m手
func formatLotDistribution(allocatedShares int64, winApplicants int, sharesPerLot int64, maxLots int64) string {
	if winApplicants == 0 {
		return "无人中签"
	}

	totalLots := allocatedShares / sharesPerLot
	if totalLots == 0 {
		return "无人中签"
	}

	avgLots := float64(totalLots) / float64(winApplicants)
	avgLotsInt := int64(avgLots)

	// 限制不超过最大申购手数
	if avgLotsInt > maxLots {
		avgLotsInt = maxLots
	}

	if avgLots == float64(avgLotsInt) {
		// 所有人中签手数相同
		return fmt.Sprintf("%d人中%d手", winApplicants, avgLotsInt)
	}

	// 分配不均，需要计算不同手数的分配
	// 基础手数：avgLotsInt手
	// 多分配手数：higherLots = avgLotsInt + 1手
	// 多分配人数：higherCount = totalLots - avgLotsInt*winApplicants
	higherLots := avgLotsInt + 1
	if higherLots > maxLots {
		higherLots = maxLots
		avgLotsInt = maxLots
		// 如果都超过maxLots，所有人都中maxLots手
		if avgLotsInt >= maxLots {
			return fmt.Sprintf("%d人中%d手", winApplicants, maxLots)
		}
	}

	higherCount := totalLots - avgLotsInt*int64(winApplicants)
	lowerCount := int64(winApplicants) - higherCount

	if higherCount <= 0 {
		// 所有人都中基础手数
		return fmt.Sprintf("%d人中%d手", winApplicants, avgLotsInt)
	}

	if lowerCount <= 0 {
		// 所有人都中higherLots手
		return fmt.Sprintf("%d人中%d手", winApplicants, higherLots)
	}

	// 格式：X人中n手，另外Y人多分配m手
	extraLots := higherLots - avgLotsInt
	return fmt.Sprintf("%d人中%d手，另外%d人多分配%d手", winApplicants, avgLotsInt, higherCount, extraLots)
}

// EstimateAllTiers 推算所有档位的人数分布
func EstimateAllTiers(totalApplicants, bGroupApplicants int, oneLotRate, aTailRate, bHeadRate float64, aTail, bHead int64) []TierInfo {
	var tiers []TierInfo

	// 计算各组总人数
	bGroupTotal := bGroupApplicants
	if bGroupTotal == 0 && totalApplicants > 0 {
		bGroupTotal = int(float64(totalApplicants) * 0.05)
	}
	aGroupTotal := totalApplicants - bGroupTotal

	// 设置默认值
	if oneLotRate == 0 {
		oneLotRate = 0.40
	}
	if aTailRate == 0 {
		aTailRate = 0.02
	}
	if bHeadRate == 0 {
		bHeadRate = 0.03
	}

	// 计算已知档位人数
	oneLotApplicants := int(float64(aGroupTotal) * oneLotRate)
	aTailApplicants := int(float64(totalApplicants) * aTailRate)
	bHeadApplicants := int(float64(totalApplicants) * bHeadRate)

	// 生成甲组所有档位（1手到甲尾）
	var aTiers []TierInfo
	var aLots []int64
	aLots = append(aLots, 1) // 一手档
	var lastLots int64 = 1
	steps := []struct{ limit, step int64 }{{10, 1}, {100, 10}, {1000, 100}, {10000, 500}, {100000, 1000}}
	for _, s := range steps {
		for i := lastLots + s.step; i <= s.limit && i <= aTail; i += s.step {
			if i != 1 { // 避免重复添加1手
				aLots = append(aLots, i)
			}
			lastLots = i
		}
		if lastLots >= aTail {
			break
		}
	}
	if aTail > 1 && (len(aLots) == 0 || aLots[len(aLots)-1] < aTail) {
		aLots = append(aLots, aTail)
	}

	// 分配甲组人数（指数衰减模型）
	aOtherApplicants := aGroupTotal - oneLotApplicants - aTailApplicants
	if aOtherApplicants < 0 {
		aOtherApplicants = 0
	}

	// 添加一手档
	aTiers = append(aTiers, TierInfo{Lots: 1, Applicants: oneLotApplicants, Group: "甲组"})

	// 分配中间档位人数
	if len(aLots) > 2 {
		totalWeight := 0.0
		weights := make([]float64, len(aLots)-2)
		for i := 1; i < len(aLots)-1; i++ {
			weight := 1.0 / float64(aLots[i]) // 指数衰减
			weights[i-1] = weight
			totalWeight += weight
		}

		for i := 1; i < len(aLots)-1; i++ {
			applicants := int(float64(aOtherApplicants) * weights[i-1] / totalWeight)
			aTiers = append(aTiers, TierInfo{Lots: aLots[i], Applicants: applicants, Group: "甲组"})
		}
	}

	// 添加甲尾
	if aTail > 1 {
		aTiers = append(aTiers, TierInfo{Lots: aTail, Applicants: aTailApplicants, Group: "甲组"})
	}

	// 生成乙组档位（乙头、乙二、乙三...）
	bOtherApplicants := bGroupTotal - bHeadApplicants
	if bOtherApplicants < 0 {
		bOtherApplicants = 0
	}

	var bTiers []TierInfo
	var bLots []int64
	bLots = append(bLots, bHead)
	lastLots = bHead

	// 根据步长生成后续档位（最多生成5个乙组档位）
	for _, s := range steps {
		if lastLots >= s.limit {
			continue
		}
		for i := lastLots + s.step; i <= s.limit && len(bLots) < 6; i += s.step {
			bLots = append(bLots, i)
			lastLots = i
		}
		if len(bLots) >= 6 {
			break
		}
	}

	// 分配乙组人数（指数衰减）
	if len(bLots) > 1 {
		totalWeight := 0.0
		weights := make([]float64, len(bLots))
		weights[0] = 1.0
		totalWeight = 1.0
		for i := 1; i < len(bLots); i++ {
			weight := 1.0 / float64(i+1)
			weights[i] = weight
			totalWeight += weight
		}

		for i := 0; i < len(bLots); i++ {
			var applicants int
			if i == 0 {
				applicants = bHeadApplicants
			} else {
				applicants = int(float64(bOtherApplicants) * weights[i] / (totalWeight - 1.0))
			}
			bTiers = append(bTiers, TierInfo{Lots: bLots[i], Applicants: applicants, Group: "乙组"})
		}
	} else {
		bTiers = append(bTiers, TierInfo{Lots: bHead, Applicants: bHeadApplicants, Group: "乙组"})
	}

	// 合并所有档位
	tiers = append(aTiers, bTiers...)
	return tiers
}

// CalculateWinRates 计算各档位中签率（B机制，无回拨，优先一手原则）
// 严格遵守港股IPO分配原则：
// 1. 甲组优先一手原则：一手档优先分配，分配比例高于其他档位
// 2. 中签率随档位上升而下降：档位越高，中签率（分配股数/申购股数）越低
// 3. 中签人数 = 分配股数 / 每手股数（取整），不是申请人数*中签率
func CalculateWinRates(tiers []TierInfo, publicOfferShares float64, lotSize int) []WinRateInfo {
	var results []WinRateInfo

	// B机制下：公開發售股數固定，无回拨
	totalPublicShares := int64(publicOfferShares * 10000)
	aGroupShares := totalPublicShares / 2 // 甲组分配股数
	bGroupShares := totalPublicShares / 2 // 乙组分配股数

	sharesPerLot := int64(lotSize)

	// 分离甲组和乙组档位
	var aGroupTiers, bGroupTiers []TierInfo
	for _, tier := range tiers {
		if tier.Group == "甲组" {
			aGroupTiers = append(aGroupTiers, tier)
		} else {
			bGroupTiers = append(bGroupTiers, tier)
		}
	}

	// 计算甲组分配（优先一手原则）
	aGroupResults := calculateAGroupAllocation(aGroupTiers, aGroupShares, sharesPerLot)

	// 计算乙组分配（按比例分配，但中签率随档位下降）
	bGroupResults := calculateBGroupAllocation(bGroupTiers, bGroupShares, sharesPerLot)

	// 合并结果
	results = append(results, aGroupResults...)
	results = append(results, bGroupResults...)

	return results
}

// calculateAGroupAllocation 计算甲组分配（优先一手原则）
// 优先一手原则：一手档优先分配，分配比例高于其他档位
// 中签率随档位上升而下降：档位越高，中签率越低
func calculateAGroupAllocation(tiers []TierInfo, totalShares int64, sharesPerLot int64) []WinRateInfo {
	var results []WinRateInfo
	if len(tiers) == 0 {
		return results
	}

	// 计算总申购股数
	var totalSubscribed int64
	for _, tier := range tiers {
		totalSubscribed += tier.Lots * sharesPerLot * int64(tier.Applicants)
	}

	// 根据真实数据，先确定第一档（1手）的目标中签率，然后按比例确定其他档位的中签率
	// 真实数据：1手2.00%，2手1.50%（75%），3手1.28%（64%），10手0.80%（40%）
	// 使用幂函数拟合：winRate = baseRate * lots^(-beta)
	// 1手：baseRate = 2.00%
	// 2手：2.00% * 2^(-beta) = 1.50%，解得 beta ≈ 0.415
	// 10手：2.00% * 10^(-beta) = 0.80%，验证：2.00% * 10^(-0.415) ≈ 0.80%
	baseWinRate := 0.02 // 1手的目标中签率 2.00%
	beta := 0.415       // 中签率衰减系数，根据真实数据拟合

	// 第一步：计算每个档位的目标中签率
	type TierTarget struct {
		tier                  TierInfo
		targetWinRate         float64
		subscribedShares      int64
		targetAllocatedShares int64
	}

	var tierTargets []TierTarget
	for _, tier := range tiers {
		tierSubscribedShares := tier.Lots * sharesPerLot * int64(tier.Applicants)
		// 计算目标中签率：winRate = baseRate * lots^(-beta)
		targetWinRate := baseWinRate * math.Pow(float64(tier.Lots), -beta)
		// 根据目标中签率和申购股数，计算目标分配股数
		targetAllocatedShares := int64(float64(tierSubscribedShares) * targetWinRate)

		tierTargets = append(tierTargets, TierTarget{
			tier:                  tier,
			targetWinRate:         targetWinRate,
			subscribedShares:      tierSubscribedShares,
			targetAllocatedShares: targetAllocatedShares,
		})
	}

	// 第二步：计算总目标分配股数
	var totalTargetAllocated int64
	for _, tt := range tierTargets {
		totalTargetAllocated += tt.targetAllocatedShares
	}

	// 第三步：如果总目标分配股数超过总股数，按比例缩减
	var scaleFactor float64 = 1.0
	if totalTargetAllocated > totalShares {
		scaleFactor = float64(totalShares) / float64(totalTargetAllocated)
	}

	// 第四步：按比例分配股数，并计算中签率和中签人数
	for _, tt := range tierTargets {
		allocatedShares := int64(float64(tt.targetAllocatedShares) * scaleFactor)

		// 确保每个档位至少分配1手，保证有人中签
		if allocatedShares < sharesPerLot && tt.tier.Applicants > 0 {
			allocatedShares = sharesPerLot
		}

		// 计算实际中签率
		winRate := float64(allocatedShares) / float64(tt.subscribedShares)

		// 计算中签人数和中签手数
		var winApplicants int
		var allocatedLots int64

		if tt.tier.Applicants > 0 && allocatedShares > 0 {
			avgAllocatedSharesPerApplicant := float64(allocatedShares) / float64(tt.tier.Applicants)
			avgAllocatedLotsPerApplicant := avgAllocatedSharesPerApplicant / float64(sharesPerLot)

			if avgAllocatedLotsPerApplicant >= 1.0 {
				winApplicants = tt.tier.Applicants
				allocatedLots = int64(avgAllocatedLotsPerApplicant)
				if allocatedLots > int64(tt.tier.Lots) {
					allocatedLots = int64(tt.tier.Lots)
				}
			} else {
				winApplicants = int(allocatedShares / sharesPerLot)
				if winApplicants > tt.tier.Applicants {
					winApplicants = tt.tier.Applicants
				}
				if winApplicants == 0 && allocatedShares >= sharesPerLot {
					winApplicants = 1
				}
				if winApplicants > 0 {
					allocatedLots = 1
				}
			}
		}

		lotDistribution := formatLotDistribution(allocatedShares, winApplicants, sharesPerLot, tt.tier.Lots)
		results = append(results, WinRateInfo{
			Lots:             tt.tier.Lots,
			Applicants:       tt.tier.Applicants,
			Group:            "甲组",
			SubscribedShares: tt.subscribedShares,
			AllocatedShares:  allocatedShares,
			WinRate:          winRate,
			WinApplicants:    winApplicants,
			AllocatedLots:    allocatedLots,
			LotDistribution:  lotDistribution,
		})
	}

	return results
}

// calculateBGroupAllocation 计算乙组分配（按比例分配，但中签率随档位下降）
func calculateBGroupAllocation(tiers []TierInfo, totalShares int64, sharesPerLot int64) []WinRateInfo {
	var results []WinRateInfo
	if len(tiers) == 0 {
		return results
	}

	// 根据真实数据，乙组的中签率更低，且随档位下降
	// 真实数据：40000手0.20%，50000手0.18%
	// 使用幂函数拟合：winRate = baseRate * lots^(-beta)
	// 找到乙组最低档位作为基准
	var minLots int64 = 999999
	for _, tier := range tiers {
		if tier.Lots < minLots {
			minLots = tier.Lots
		}
	}

	// 根据真实数据，40000手0.20%，50000手0.18%
	// 40000手：baseRate = 0.20%
	// 50000手：0.20% * (50000/40000)^(-beta) = 0.18%，解得 beta ≈ 0.2
	baseWinRate := 0.002 // 乙组首档的目标中签率 0.20%
	beta := 0.2          // 中签率衰减系数，乙组衰减较慢

	// 计算每个档位的目标中签率
	type TierTarget struct {
		tier                  TierInfo
		targetWinRate         float64
		subscribedShares      int64
		targetAllocatedShares int64
	}

	var tierTargets []TierTarget
	for _, tier := range tiers {
		tierSubscribedShares := tier.Lots * sharesPerLot * int64(tier.Applicants)
		// 计算目标中签率：winRate = baseRate * (lots/minLots)^(-beta)
		targetWinRate := baseWinRate * math.Pow(float64(tier.Lots)/float64(minLots), -beta)
		// 根据目标中签率和申购股数，计算目标分配股数
		targetAllocatedShares := int64(float64(tierSubscribedShares) * targetWinRate)

		tierTargets = append(tierTargets, TierTarget{
			tier:                  tier,
			targetWinRate:         targetWinRate,
			subscribedShares:      tierSubscribedShares,
			targetAllocatedShares: targetAllocatedShares,
		})
	}

	// 计算总目标分配股数
	var totalTargetAllocated int64
	for _, tt := range tierTargets {
		totalTargetAllocated += tt.targetAllocatedShares
	}

	// 如果总目标分配股数超过总股数，按比例缩减
	var scaleFactor float64 = 1.0
	if totalTargetAllocated > totalShares {
		scaleFactor = float64(totalShares) / float64(totalTargetAllocated)
	}

	// 按比例分配股数，并计算中签率和中签人数
	for _, tt := range tierTargets {
		allocatedShares := int64(float64(tt.targetAllocatedShares) * scaleFactor)

		// 确保每个档位至少分配1手，保证有人中签
		if allocatedShares < sharesPerLot && tt.tier.Applicants > 0 {
			allocatedShares = sharesPerLot
		}

		// 计算实际中签率
		winRate := float64(allocatedShares) / float64(tt.subscribedShares)

		// 计算中签人数和中签手数
		var winApplicants int
		var allocatedLots int64

		if tt.tier.Applicants > 0 && allocatedShares > 0 {
			avgAllocatedSharesPerApplicant := float64(allocatedShares) / float64(tt.tier.Applicants)
			avgAllocatedLotsPerApplicant := avgAllocatedSharesPerApplicant / float64(sharesPerLot)

			if avgAllocatedLotsPerApplicant >= 1.0 {
				winApplicants = tt.tier.Applicants
				allocatedLots = int64(avgAllocatedLotsPerApplicant)
				if allocatedLots > int64(tt.tier.Lots) {
					allocatedLots = int64(tt.tier.Lots)
				}
			} else {
				winApplicants = int(allocatedShares / sharesPerLot)
				if winApplicants > tt.tier.Applicants {
					winApplicants = tt.tier.Applicants
				}
				if winApplicants == 0 && allocatedShares >= sharesPerLot {
					winApplicants = 1
				}
				if winApplicants > 0 {
					allocatedLots = 1
				}
			}
		}

		lotDistribution := formatLotDistribution(allocatedShares, winApplicants, sharesPerLot, tt.tier.Lots)
		results = append(results, WinRateInfo{
			Lots:             tt.tier.Lots,
			Applicants:       tt.tier.Applicants,
			Group:            "乙组",
			SubscribedShares: tt.subscribedShares,
			AllocatedShares:  allocatedShares,
			WinRate:          winRate,
			WinApplicants:    winApplicants,
			AllocatedLots:    allocatedLots,
			LotDistribution:  lotDistribution,
		})
	}

	return results
}

func main() {
	aTail, bHead := getValidLots(currentIPO.PriceMax, currentIPO.LotSize)
	oneLotPrice := currentIPO.PriceMax * float64(currentIPO.LotSize)
	bGroupApplicants := currentIPO.BGroupApplicants
	if bGroupApplicants == 0 && currentIPO.TotalApplicants > 0 {
		bGroupApplicants = int(float64(currentIPO.TotalApplicants) * 0.05)
	}
	oneLotRate := currentIPO.OneLotRate
	if oneLotRate == 0 {
		oneLotRate = 0.40
	}
	aTailRate := currentIPO.ATailRate
	if aTailRate == 0 {
		aTailRate = 0.02
	}
	bHeadRate := currentIPO.BHeadRate
	if bHeadRate == 0 {
		bHeadRate = 0.03
	}
	fmt.Println("==================================================")
	fmt.Printf("🔍 自动推算申购阶梯: %s\n", currentIPO.StockName)
	fmt.Println("==================================================")
	fmt.Printf("🏠 [甲组末档 - 甲尾]\n   申购手数: %d 手\n   申购金额: %.2f HKD\n", aTail, float64(aTail)*oneLotPrice)
	fmt.Println("--------------------------------------------------")
	fmt.Printf("🚀 [乙组首档 - 乙头]\n   申购手数: %d 手\n   申购金额: %.2f HKD\n", bHead, float64(bHead)*oneLotPrice)
	fmt.Println("==================================================")
	if currentIPO.TotalApplicants > 0 {
		// 推算所有档位人数
		tiers := EstimateAllTiers(currentIPO.TotalApplicants, currentIPO.BGroupApplicants, oneLotRate, aTailRate, bHeadRate, aTail, bHead)

		// 计算并显示各档位中签率
		winRates := CalculateWinRates(tiers, currentIPO.PublicOfferShares, currentIPO.LotSize)
		fmt.Println("🎯 各档位中签率预测 (B机制，无回拨，优先一手原则):")
		fmt.Println("--------------------------------------------------")
		fmt.Println("【甲组】")
		for _, wr := range winRates {
			if wr.Group == "甲组" {
				shares := wr.Lots * int64(currentIPO.LotSize)
				if wr.WinApplicants > 0 {
					fmt.Printf("   %d手（%d股）: 中签率 %.4f%%, 申请人数 %d人, 中签人数 %d人, 中签手数 %s\n", wr.Lots, shares, wr.WinRate*100, wr.Applicants, wr.WinApplicants, wr.LotDistribution)
				} else {
					fmt.Printf("   %d手（%d股）: 中签率 %.4f%%, 申请人数 %d人, 中签人数 0人, 中签手数 无人中签\n", wr.Lots, shares, wr.WinRate*100, wr.Applicants)
				}
			}
		}
		fmt.Println("--------------------------------------------------")
		fmt.Println("【乙组】")
		for _, wr := range winRates {
			if wr.Group == "乙组" {
				shares := wr.Lots * int64(currentIPO.LotSize)
				if wr.WinApplicants > 0 {
					fmt.Printf("   %d手（%d股）: 中签率 %.4f%%, 申请人数 %d人, 中签人数 %d人, 中签手数 %s\n", wr.Lots, shares, wr.WinRate*100, wr.Applicants, wr.WinApplicants, wr.LotDistribution)
				} else {
					fmt.Printf("   %d手（%d股）: 中签率 %.4f%%, 申请人数 %d人, 中签人数 0人, 中签手数 无人中签\n", wr.Lots, shares, wr.WinRate*100, wr.Applicants)
				}
			}
		}
		fmt.Println("==================================================")
	}
}
