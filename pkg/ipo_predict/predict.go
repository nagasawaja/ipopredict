package ipo_predict

import (
	"fmt"
	"math"
	"sort"
)

// Tier 档位：Lots=股数，AmountHKD=该档所需资金（HKD）
type Tier struct {
	Lots      int64
	AmountHKD float64
}

// MarginRequest 孖展预测入参；倍数由 孖展总额/募资额 内部推算。
// PublicShares 为回拨后的公开发售总股数（甲乙组池子各占 50%）。
type MarginRequest struct {
	PublicShares    int64
	LotSize         int
	Price           float64
	BrokerMarginSum float64
	Buckets         []Tier
	// EstimatedApplicantsOverride 可选：总申请人数 override，>0 时直接替代内部自动估计值。
	EstimatedApplicantsOverride int
	// BGroupApplicantRatio 可选：乙组人数占总申请人数比例（0~1），未传时使用默认经验值。
	BGroupApplicantRatio float64
	// AOneHandApplicantRatio 可选：甲组里一手档申请人数占甲组总申请人数比例（0~1），未传时使用默认经验值。
	AOneHandApplicantRatio float64
	// AutoEstimateContext 由上层/helper 提供的历史先验和并行招股特征；pkg/ipo_predict 只做纯计算。
	AutoEstimateContext *AutoEstimateContext
}

// AutoEstimateContext 为纯计算层显式传入的先验统计与 overlap 特征。
type AutoEstimateContext struct {
	ComparablePeerCount                    int
	ComparableApplicantMedian              int
	ComparableApplicantP10                 int
	ComparableApplicantP90                 int
	ComparableBGroupApplicantRatioMedian   float64
	ComparableAOneHandApplicantRatioMedian float64
	OverlapStockCount                      int
	OverlapPublicLotsSum                   float64
	OverlapEntryFeeSum                     float64
	OverlapLowEntryCount                   int
	OverlapSmallPublicLotsCount            int
}

// PredictResult 预测结果
type PredictResult struct {
	Tiers    []TierInfo
	WinRates []WinRateInfo
	Meta     PredictMeta
}

// PredictMeta 记录自动估计与最终实际采用的关键参数，便于页面展示和回测对比。
type PredictMeta struct {
	RawEstimatedApplicants           int
	AutoEstimatedApplicants          int
	FinalEstimatedApplicants         int
	AutoBGroupApplicantRatio         float64
	FinalBGroupApplicantRatio        float64
	AutoAOneHandApplicantRatio       float64
	FinalAOneHandApplicantRatio      float64
	UsedEstimatedApplicantsOverride  bool
	UsedBGroupApplicantRatioOverride bool
	UsedAOneHandApplicantOverride    bool
	ComparablePeerCount              int
}

// TierInfo 档位信息（含推算申请人数）
type TierInfo struct {
	Lots       int64
	Applicants int
	Group      string
}

// WinRateInfo 档位中签率信息
type WinRateInfo struct {
	Lots             int64
	Applicants       int
	Group            string
	SubscribedShares int64
	AllocatedShares  int64
	PerLotRate       float64 // 每手中签率 = AllocatedShares/SubscribedShares（随档位上升阶梯降低）
	WinRate          float64 // 每户中签率 = WinApplicants/Applicants（获至少 1 手人数/申请人，随档位上升而上升）
	WinApplicants    int     // 获至少 1 手人数
	AllocatedLots    int64
	LotDistribution  string
}

const (
	groupBThresholdHKD   = 5000000.0
	maxAvgLots           = 10000 // 机制B 下大户顶格打新，上限提高
	minWinRate           = 0.01
	oneHandFractionA     = 0.42610359528714076
	bGroupApplicantRatio = 0.03
	// 一手档分配上限（按超购区间）
	oneHandCapRatioOver30  = 0.7527067503749538
	oneHandCapRatioOver100 = 0.2163062418018349
	oneHandCapRatioOver500 = 0.3172098833962666
	// 乙组大户相对乙头的中签率递减系数（每档略降）
	bGroupTierDecay = 0.04
	// 乙组申请人数分布：历史高倍数样本显示乙头确实最密集，但不应按 1/tl^2 过度集中。
	bGroupApplicantWeightPower = 1.35
	bGroupHeadWeightBoost      = 3.0
	bGroupTailWeightBoost      = 10.0
	// 甲组非一手档边际递减系数：0=纯按钱分(每手中签率恒定)，1=按人头分；0.3~0.5 使每手中签率随申购手数缓慢下降
	aGroupDecayFactor = 0.4
	// 一手中签率经验公式（仅使用预测前字段：sub/lot_size/price/public_offer_shares）
	oneLotCoefA                    = 1684.0287141872761
	oneLotCoefSubPow               = 0.8062044102582026
	oneLotCoefPerLotPow            = 0.17472180191732034
	oneLotCoefPublicLotsPow        = -0.11699874908986863
	oneLotScaleLowSub              = 0.8996412119712173
	oneLotScaleMidSub              = 0.45573355989597086
	oneLotScaleHighSub             = 1.322725854388661
	oneLotSmallPublicLotsThreshold = 108831.21989544969
	oneLotSmallPublicLotsGamma     = 1.0853510291523165
	oneLotExtraPenaltySubCutoff    = 8.576165167712722
	oneLotExtraPenaltyPublicLots   = 73739.83540147381
	oneLotExtraPenaltyPerLotHKD    = 12579.574312516323
	oneLotExtraPenaltyGamma        = 1.3797897303008322
	// 一手中签率校准：仅使用前置特征（sub/每手金额/public lots）做轻量幂次校准，压缩系统性误差。
	oneLotCalibSubPow        = -0.075
	oneLotCalibPerLotPow     = 0.235
	oneLotCalibPublicLotsPow = 0.035
	// 人均申购手数经验公式：基于历史 applicants 反推拟合，仅用前置特征（sub/每手金额/public lots）。
	avgLotsCoefA         = 0.086069
	avgLotsSubPow        = 0.517698
	avgLotsPerLotPow     = -0.275720
	avgLotsPublicLotsPow = 0.636956
)

// DefaultBGroupApplicantRatio 返回当前模型默认的乙组申请人数占比。
func DefaultBGroupApplicantRatio() float64 {
	return bGroupApplicantRatio
}

// DefaultAOneHandApplicantRatio 返回当前模型默认的甲组一手档申请人数占比。
func DefaultAOneHandApplicantRatio() float64 {
	return oneHandFractionA
}

func clampFloat(v, lo, hi float64) float64 {
	if hi < lo {
		lo, hi = hi, lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		lo, hi = hi, lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func safeLog10Ratio(value, baseline float64) float64 {
	if value <= 0 || baseline <= 0 {
		return 0
	}
	return math.Log10(value / baseline)
}

func peerShrinkWeight(peerCount int) float64 {
	switch {
	case peerCount >= 40:
		return 0.70
	case peerCount >= 20:
		return 0.60
	case peerCount >= 10:
		return 0.40
	case peerCount > 0:
		return 0.20
	default:
		return 0
	}
}

func estimateApplicantsWithPrior(rawApplicants int, realOversub, perLotMoney float64, ctx *AutoEstimateContext) int {
	if rawApplicants <= 0 {
		rawApplicants = 1
	}
	estimated := rawApplicants
	usePeerClamp := false
	if ctx == nil {
		return estimated
	}

	if ctx.ComparablePeerCount > 0 && ctx.ComparableApplicantMedian > 0 {
		w := peerShrinkWeight(ctx.ComparablePeerCount)
		peerRatio := 1.0
		if rawApplicants > 0 {
			peerRatio = float64(ctx.ComparableApplicantMedian) / float64(rawApplicants)
		}
		lo := ctx.ComparableApplicantP10
		hi := ctx.ComparableApplicantP90
		if lo <= 0 {
			lo = ctx.ComparableApplicantMedian
		}
		if hi <= 0 {
			hi = ctx.ComparableApplicantMedian
		}
		upperBand := int(math.Round(float64(hi) * 1.2))
		lowerBand := int(math.Round(float64(lo) * 0.8))
		switch {
		case peerRatio > 1.10:
			// 当前 raw 公式总体已偏轻微低估，不做向上补，避免伤害中位数样本。
			w = 0
		case rawApplicants > upperBand && peerRatio < 0.90:
			// raw 明显高于历史可比上沿时，才做向下 shrink。
			w *= 0.60
		default:
			w = 0
		}
		if ctx.ComparableApplicantP10 > 0 && ctx.ComparableApplicantP90 > 0 {
			if rawApplicants >= lowerBand && rawApplicants <= upperBand {
				w = 0
			}
		}
		switch {
		case peerRatio >= 1.5 || peerRatio <= 0.67:
			w *= 0.20
		case peerRatio >= 1.25 || peerRatio <= 0.80:
			w *= 0.40
		}
		if ctx.ComparableApplicantP10 > 0 && ctx.ComparableApplicantP90 > 0 &&
			rawApplicants >= ctx.ComparableApplicantP10 && rawApplicants <= ctx.ComparableApplicantP90 {
			w *= 0.25
		}
		if w > 0 {
			estimated = int(math.Round(float64(rawApplicants)*(1-w) + float64(ctx.ComparableApplicantMedian)*w))
			usePeerClamp = true
		}
	}

	adjustment := 1.0
	adjustment -= 0.005 * clampFloat(safeLog10Ratio(perLotMoney, 6000), -1.0, 1.0)
	adjustment -= 0.0010 * float64(min(ctx.OverlapStockCount, 6))
	adjustment -= 0.0005 * float64(min(ctx.OverlapLowEntryCount, 4))
	adjustment -= 0.0003 * float64(min(ctx.OverlapSmallPublicLotsCount, 4))
	if ctx.OverlapEntryFeeSum > 0 {
		adjustment -= 0.001 * clampFloat(math.Log1p(ctx.OverlapEntryFeeSum/25000.0), 0, 1.5)
	}
	adjustment = clampFloat(adjustment, 0.97, 1.03)
	estimated = int(math.Round(float64(estimated) * adjustment))

	if usePeerClamp && (ctx.ComparableApplicantP10 > 0 || ctx.ComparableApplicantP90 > 0) {
		lo := ctx.ComparableApplicantP10
		hi := ctx.ComparableApplicantP90
		if lo <= 0 {
			lo = ctx.ComparableApplicantMedian
		}
		if hi <= 0 {
			hi = ctx.ComparableApplicantMedian
		}
		if lo > 0 && hi > 0 {
			estimated = clampInt(
				estimated,
				max(1, int(math.Round(float64(lo)*0.8))),
				max(1, int(math.Round(float64(hi)*1.2))),
			)
		}
	}
	if estimated <= 0 {
		return 1
	}
	return estimated
}

func estimateAutoBGroupApplicantRatio(realOversub, publicLots float64, ctx *AutoEstimateContext) float64 {
	ratio := bGroupApplicantRatio
	if ctx != nil && ctx.ComparableBGroupApplicantRatioMedian > 0 {
		ratio = ctx.ComparableBGroupApplicantRatioMedian
	}
	ratio += 0.010 * clampFloat(safeLog10Ratio(realOversub, 80), -1.0, 1.2)
	ratio += 0.008 * clampFloat(safeLog10Ratio(publicLots, 100000), -1.0, 1.0)
	if ctx != nil {
		ratio -= 0.0015 * float64(min(ctx.OverlapStockCount, 6))
		ratio -= 0.0010 * float64(min(ctx.OverlapSmallPublicLotsCount, 4))
	}
	return clampFloat(ratio, 0.005, 0.15)
}

func estimateAutoAOneHandApplicantRatio(realOversub, publicLots, perLotMoney float64, ctx *AutoEstimateContext) float64 {
	ratio := oneHandFractionA
	if ctx != nil && ctx.ComparableAOneHandApplicantRatioMedian > 0 {
		ratio = ctx.ComparableAOneHandApplicantRatioMedian
	}
	ratio -= 0.070 * clampFloat(safeLog10Ratio(publicLots, 100000), -1.0, 1.2)
	ratio -= 0.045 * clampFloat(safeLog10Ratio(perLotMoney, 6000), -1.0, 1.0)
	ratio -= 0.060 * clampFloat(safeLog10Ratio(realOversub, 80), -1.0, 1.2)
	if ctx != nil {
		ratio -= 0.006 * float64(min(ctx.OverlapSmallPublicLotsCount, 4))
	}
	return clampFloat(ratio, 0.15, 0.85)
}

// applyBottomUpAllocation 按红鞋/普惠原则：一户一手优先，余货再分配。用 AllocatedShares 与 Applicants 反推 WinApplicants、LotDistribution；设 PerLotRate=每手中签率，WinRate=每户中签率。
func applyBottomUpAllocation(w *WinRateInfo, lotSize int) {
	if w.Applicants == 0 {
		w.WinApplicants = 0
		w.WinRate = 0
		w.PerLotRate = 0
		w.LotDistribution = "无人中签"
		return
	}
	totalLots := w.AllocatedShares / int64(lotSize)
	baseLotsPerUser := totalLots / int64(w.Applicants)
	surplusLots := totalLots % int64(w.Applicants)
	if baseLotsPerUser > 0 {
		w.WinApplicants = w.Applicants
	} else {
		w.WinApplicants = int(surplusLots)
	}
	// 每户中签率 = 获至少 1 手人数 / 申请人
	w.WinRate = float64(w.WinApplicants) / float64(w.Applicants)
	// 每手中签率 = 分配股数/申购股数（随档位上升应阶梯降低，由 enforcePerLotRateDecreasing 保证）
	if w.SubscribedShares > 0 {
		w.PerLotRate = float64(w.AllocatedShares) / float64(w.SubscribedShares)
	} else {
		w.PerLotRate = 0
	}
	if baseLotsPerUser == 0 {
		w.LotDistribution = fmt.Sprintf("%d名申请人中的%d名获发1手", w.Applicants, w.WinApplicants)
	} else {
		w.LotDistribution = fmt.Sprintf("%d名申请人获发%d手，另加%d名获发额外1手", w.Applicants, baseLotsPerUser, surplusLots)
	}
}

// Predict 孖展驱动中签率预测（唯一入口）
func Predict(req MarginRequest) (PredictResult, error) {
	if req.PublicShares <= 0 {
		return PredictResult{}, fmt.Errorf("PublicShares must be > 0")
	}
	if req.LotSize <= 0 {
		return PredictResult{}, fmt.Errorf("LotSize must be > 0")
	}
	if req.Price <= 0 {
		return PredictResult{}, fmt.Errorf("Price must be > 0")
	}
	if req.BrokerMarginSum <= 0 {
		return PredictResult{}, fmt.Errorf("BrokerMarginSum must be > 0")
	}
	if len(req.Buckets) == 0 {
		return PredictResult{}, fmt.Errorf("Buckets must not be empty")
	}
	for i, b := range req.Buckets {
		if b.Lots <= 0 {
			return PredictResult{}, fmt.Errorf("bucket[%d] Lots must be > 0", i)
		}
		if b.AmountHKD <= 0 {
			return PredictResult{}, fmt.Errorf("bucket[%d] AmountHKD must be > 0", i)
		}
	}
	return runMarginFlow(req)
}

// sortBucketsByAmount 按 AmountHKD 升序，保证乙头/档位递减逻辑正确
func sortBucketsByAmount(b []Tier) []Tier {
	out := make([]Tier, len(b))
	copy(out, b)
	sort.Slice(out, func(i, j int) bool { return out[i].AmountHKD < out[j].AmountHKD })
	return out
}

// capBucketsByPublicShares 将档位限制在公开发售半池以内（按每手取整），并补齐顶头槌档。
// 这样可以避免输入出现远超公开发售规模的异常超大档位，导致尾档人数畸小、单户中签手数失真。
func capBucketsByPublicShares(b []Tier, publicShares int64, lotSize int, price float64) []Tier {
	if len(b) == 0 || publicShares <= 0 || lotSize <= 0 || price <= 0 {
		return b
	}
	capShares := (publicShares / 2 / int64(lotSize)) * int64(lotSize)
	if capShares < int64(lotSize) {
		capShares = int64(lotSize)
	}
	out := make([]Tier, 0, len(b)+1)
	seen := make(map[int64]struct{}, len(b)+1)
	for _, t := range b {
		if t.Lots <= 0 || t.Lots > capShares {
			continue
		}
		if _, ok := seen[t.Lots]; ok {
			continue
		}
		out = append(out, t)
		seen[t.Lots] = struct{}{}
	}
	if len(out) == 0 {
		return []Tier{{
			Lots:      int64(lotSize),
			AmountHKD: float64(lotSize) * price,
		}}
	}
	if _, ok := seen[capShares]; !ok {
		out = append(out, Tier{
			Lots:      capShares,
			AmountHKD: float64(capShares) * price,
		})
	}
	return out
}

func estimateTargetOneHandRate(realOversub float64, publicShares int64, lotSize int, price float64, fallbackRate float64) float64 {
	if fallbackRate < minWinRate {
		fallbackRate = minWinRate
	}
	if fallbackRate > 1.0 {
		fallbackRate = 1.0
	}
	if realOversub <= 0 || publicShares <= 0 || lotSize <= 0 || price <= 0 {
		return fallbackRate
	}
	poolALots := float64(publicShares) / float64(lotSize) / 2.0
	if poolALots <= 0 {
		return fallbackRate
	}
	// 纯前置特征：sub、每手金额、公开发售总手数
	publicLots := float64(publicShares) / float64(lotSize)
	if publicLots <= 0 {
		return fallbackRate
	}
	perLotMoney := float64(lotSize) * price
	ratePct := oneLotCoefA * math.Pow(realOversub, -oneLotCoefSubPow) *
		math.Pow(perLotMoney/5000.0, oneLotCoefPerLotPow) *
		math.Pow(publicLots/100000.0, oneLotCoefPublicLotsPow)
	if publicLots < oneLotSmallPublicLotsThreshold {
		ratio := publicLots / oneLotSmallPublicLotsThreshold
		if ratio < 1 {
			ratePct *= math.Pow(ratio, oneLotSmallPublicLotsGamma)
		}
	}
	if lotSize <= 200 &&
		realOversub <= oneLotExtraPenaltySubCutoff &&
		publicLots < oneLotExtraPenaltyPublicLots &&
		perLotMoney < oneLotExtraPenaltyPerLotHKD {
		ratio := publicLots / oneLotExtraPenaltyPublicLots
		if ratio < 1 {
			ratePct *= math.Pow(ratio, oneLotExtraPenaltyGamma)
		}
	}
	switch {
	case realOversub <= 10:
		ratePct *= oneLotScaleLowSub
	case realOversub <= 80:
		ratePct *= oneLotScaleMidSub
	default:
		ratePct *= oneLotScaleHighSub
	}
	rate := ratePct / 100.0
	calib := math.Pow(realOversub/100.0, oneLotCalibSubPow) *
		math.Pow(perLotMoney/5000.0, oneLotCalibPerLotPow) *
		math.Pow(publicLots/100000.0, oneLotCalibPublicLotsPow)
	if !math.IsNaN(calib) && !math.IsInf(calib, 0) && calib > 0 {
		rate *= calib
	}
	// 极端热度校正（仅前置特征）：低客单价超热股额外下压；超热且小盘中等客单价上调。
	if realOversub > 350 && realOversub < 900 &&
		perLotMoney < 5000 && publicLots < 90000 {
		rate *= 0.08
	}
	if realOversub > 1200 &&
		perLotMoney >= 6500 && perLotMoney <= 8000 &&
		publicLots < 40000 {
		rate *= 2.6
	}
	if rate < minWinRate {
		return minWinRate
	}
	if rate > 1.0 {
		return 1.0
	}
	return rate
}

type weightedRemainder struct {
	idx    int
	frac   float64
	weight float64
}

// allocateApplicantsByWeight 使用最大余数法离散分配人数：
// 1) 保证分配总和严格等于 total；
// 2) 可选给每个正权重档位保底 1 人（仅 total 足够时）；
// 3) 避免简单 int 截断导致中后段整段归零。
func allocateApplicantsByWeight(total int, weights []float64, minEachPositive bool) []int {
	out := make([]int, len(weights))
	if total <= 0 || len(weights) == 0 {
		return out
	}
	positive := make([]int, 0, len(weights))
	sumW := 0.0
	for i, w := range weights {
		if w > 0 {
			positive = append(positive, i)
			sumW += w
		}
	}
	if len(positive) == 0 || sumW <= 0 {
		return out
	}

	remaining := total
	if minEachPositive && total >= len(positive) {
		for _, i := range positive {
			out[i] = 1
		}
		remaining -= len(positive)
	}
	if remaining <= 0 {
		return out
	}

	remainders := make([]weightedRemainder, 0, len(positive))
	distributed := 0
	for _, i := range positive {
		exact := float64(remaining) * weights[i] / sumW
		base := int(math.Floor(exact))
		out[i] += base
		distributed += base
		remainders = append(remainders, weightedRemainder{
			idx:    i,
			frac:   exact - float64(base),
			weight: weights[i],
		})
	}

	left := remaining - distributed
	if left <= 0 {
		return out
	}
	sort.Slice(remainders, func(i, j int) bool {
		if remainders[i].frac == remainders[j].frac {
			if remainders[i].weight == remainders[j].weight {
				return remainders[i].idx < remainders[j].idx
			}
			return remainders[i].weight > remainders[j].weight
		}
		return remainders[i].frac > remainders[j].frac
	})
	for k := 0; k < left; k++ {
		out[remainders[k%len(remainders)].idx]++
	}
	return out
}

func runMarginFlow(req MarginRequest) (PredictResult, error) {
	// 注意：publicShares 为机制 B 锁定的最终公开发售量（如总量的 10%），无回拨
	publicShares, lotSize, price, marginSum, buckets := req.PublicShares, req.LotSize, req.Price, req.BrokerMarginSum, req.Buckets
	buckets = capBucketsByPublicShares(buckets, publicShares, lotSize, price)
	buckets = sortBucketsByAmount(buckets)
	fundraisingAmt := float64(publicShares) * price
	if fundraisingAmt <= 0 {
		return PredictResult{}, fmt.Errorf("fundraising amount must be > 0")
	}
	// 机制B（无回拨）：公开发售量固定，不随超购调整。超购 100 倍以上几乎全为孖展
	approxOversub := marginSum / fundraisingAmt
	var marginCoverageRatio, redShoeFactor float64
	if approxOversub > 100 {
		marginCoverageRatio, redShoeFactor = 0.98, 1.5
	} else if approxOversub > 15 {
		marginCoverageRatio, redShoeFactor = 0.50, 1.2
	} else {
		marginCoverageRatio, redShoeFactor = 0.35, 1.0
	}
	totalFrozen := marginSum / marginCoverageRatio
	realOversub := totalFrozen / fundraisingAmt

	// 人均申购手数（前置估计）：中高超购时显著抬升，避免申请人数被系统性高估。
	perLotMoney := float64(lotSize) * price
	if perLotMoney <= 0 {
		return PredictResult{}, fmt.Errorf("per lot money must be > 0")
	}
	publicLots := float64(publicShares) / float64(lotSize)
	if publicLots <= 0 {
		return PredictResult{}, fmt.Errorf("public lots must be > 0")
	}
	avgLots := avgLotsCoefA *
		math.Pow(realOversub, avgLotsSubPow) *
		math.Pow(perLotMoney, avgLotsPerLotPow) *
		math.Pow(publicLots, avgLotsPublicLotsPow)
	if realOversub < 10 {
		avgLots *= 1.65
	}
	if math.IsNaN(avgLots) || math.IsInf(avgLots, 0) {
		avgLots = 1
	}
	if avgLots > float64(maxAvgLots) {
		avgLots = float64(maxAvgLots)
	}
	if avgLots < 1 {
		avgLots = 1
	}
	avgTicketMoney := avgLots * perLotMoney
	if avgTicketMoney <= 0 {
		return PredictResult{}, fmt.Errorf("avg ticket money must be > 0")
	}
	rawEstimatedApplicants := int(totalFrozen / avgTicketMoney)
	if rawEstimatedApplicants <= 0 {
		rawEstimatedApplicants = 1
	}
	autoEstimatedApplicants := estimateApplicantsWithPrior(rawEstimatedApplicants, realOversub, perLotMoney, req.AutoEstimateContext)
	estimatedApplicants := autoEstimatedApplicants
	usedEstimatedApplicantsOverride := false
	if req.EstimatedApplicantsOverride > 0 {
		estimatedApplicants = req.EstimatedApplicantsOverride
		usedEstimatedApplicantsOverride = true
	}
	if estimatedApplicants <= 0 {
		estimatedApplicants = 1
	}

	// 公开发售池子：入参 PublicShares 视为回拨后的公开发售总股数，甲乙组各 50%
	poolSharesF := float64(publicShares) / 2
	poolSharesI := publicShares / 2
	poolBDemandShares := (totalFrozen * 0.5) / price
	poolBRate := 1.0
	if poolBDemandShares > 0 {
		poolBRate = poolSharesF / poolBDemandShares
	}
	if poolBRate > 1.0 {
		poolBRate = 1.0
	}
	if poolBRate < 0 {
		poolBRate = 0
	}
	oneLotRate := (poolSharesF / float64(estimatedApplicants) / float64(lotSize)) * redShoeFactor
	if oneLotRate < minWinRate {
		oneLotRate = minWinRate
	}
	if oneLotRate > 1.0 {
		oneLotRate = 1.0
	}
	var aBuckets, bBuckets []Tier
	for _, b := range buckets {
		if b.AmountHKD > groupBThresholdHKD {
			bBuckets = append(bBuckets, b)
		} else {
			aBuckets = append(aBuckets, b)
		}
	}
	autoBGroupRatio := estimateAutoBGroupApplicantRatio(realOversub, publicLots, req.AutoEstimateContext)
	bGroupRatio := autoBGroupRatio
	usedBGroupRatioOverride := false
	if req.BGroupApplicantRatio > 0 {
		bGroupRatio = req.BGroupApplicantRatio
		usedBGroupRatioOverride = true
	}
	bGroupRatio = clampFloat(bGroupRatio, 0, 1)
	bTotal := int(float64(estimatedApplicants) * bGroupRatio)
	if bTotal < 1 {
		bTotal = 1
	}
	aTotal := estimatedApplicants - bTotal
	if aTotal < 0 {
		aTotal = 0
	}
	tierLots := func(shares int64) int64 {
		n := shares / int64(lotSize)
		if n < 1 {
			n = 1
		}
		return n
	}
	var maxAmountA float64
	for _, b := range aBuckets {
		if b.AmountHKD > maxAmountA {
			maxAmountA = b.AmountHKD
		}
	}
	var aWeights []float64
	for _, b := range aBuckets {
		tl := tierLots(b.Lots)
		w := 1.0
		if b.Lots > int64(lotSize) {
			w = 1.0 / math.Pow(float64(tl), 0.65)
		}
		if maxAmountA > 0 && b.AmountHKD >= maxAmountA*0.999 {
			w *= 25.0
		}
		aWeights = append(aWeights, w)
	}
	autoAOneHandRatio := estimateAutoAOneHandApplicantRatio(realOversub, publicLots, perLotMoney, req.AutoEstimateContext)
	aOneHandRatio := autoAOneHandRatio
	usedAOneHandRatioOverride := false
	if req.AOneHandApplicantRatio > 0 {
		aOneHandRatio = req.AOneHandApplicantRatio
		usedAOneHandRatioOverride = true
	}
	aOneHandRatio = clampFloat(aOneHandRatio, 0, 1)
	oneHandA := int(float64(aTotal) * aOneHandRatio)
	otherA := aTotal - oneHandA
	if otherA < 0 {
		otherA, oneHandA = 0, aTotal
	}
	aOneHandWeights := make([]float64, len(aBuckets))
	aOtherWeights := make([]float64, len(aBuckets))
	for i, b := range aBuckets {
		if b.Lots <= int64(lotSize) {
			aOneHandWeights[i] = aWeights[i]
		} else {
			aOtherWeights[i] = aWeights[i]
		}
	}
	aOneHandApplicants := allocateApplicantsByWeight(oneHandA, aOneHandWeights, false)
	aOtherApplicants := allocateApplicantsByWeight(otherA, aOtherWeights, false)
	// 乙头 + 顶头槌（乙尾）给大权重；中间档按历史样本采用温和幂次衰减。
	var bWeights []float64
	var bSumW float64
	var minAmountB float64
	for _, b := range bBuckets {
		if minAmountB == 0 || b.AmountHKD < minAmountB {
			minAmountB = b.AmountHKD
		}
	}
	for i, b := range bBuckets {
		tl := tierLots(b.Lots)
		w := 1.0 / math.Pow(float64(tl), bGroupApplicantWeightPower)
		if b.AmountHKD <= minAmountB*1.001 {
			w *= bGroupHeadWeightBoost
		} else if i == len(bBuckets)-1 {
			w *= bGroupTailWeightBoost
		}
		bWeights = append(bWeights, w)
		bSumW += w
	}
	if bSumW == 0 && len(bBuckets) > 0 {
		for i := range bBuckets {
			bWeights[i] = 1.0
			bSumW += 1.0
		}
	}
	bApplicantsByTier := allocateApplicantsByWeight(bTotal, bWeights, true)
	poolASlots := poolSharesI / int64(lotSize)
	if poolASlots < 1 {
		poolASlots = 1
	}
	// 甲组一手档目标：结合超购与人均认购手数的经验公式，冷门股可更高，超热股自动收敛。
	targetOneHandRate := estimateTargetOneHandRate(realOversub, publicShares, lotSize, price, oneLotRate)
	oneHandCapRatio := 1.0
	switch {
	case realOversub > 500:
		oneHandCapRatio = oneHandCapRatioOver500
	case realOversub > 100:
		oneHandCapRatio = oneHandCapRatioOver100
	case realOversub > 30:
		oneHandCapRatio = oneHandCapRatioOver30
	}
	oneHandSlots := int64(float64(oneHandA) * targetOneHandRate)
	if oneHandSlots > int64(float64(poolASlots)*oneHandCapRatio) {
		oneHandSlots = int64(float64(poolASlots) * oneHandCapRatio)
	}
	if oneHandSlots > poolASlots {
		oneHandSlots = poolASlots
	}
	if oneHandSlots < 0 {
		oneHandSlots = 0
	}
	remainingSlots := poolASlots - oneHandSlots
	if remainingSlots < 0 {
		remainingSlots = 0
	}
	// 甲组非一手档：边际递减——申购越多总货越多，但每手中签率随档位缓慢下降（加权总申购量）
	var totalWeightedShares float64
	for i, b := range aBuckets {
		if b.Lots <= int64(lotSize) {
			continue
		}
		applicants := aOtherApplicants[i]
		tl := tierLots(b.Lots)
		singleUserWeight := math.Pow(float64(tl), 1.0-aGroupDecayFactor)
		totalWeightedShares += singleUserWeight * float64(applicants)
	}
	var aTiers []TierInfo
	var aWinRates []WinRateInfo
	var aAllocatedTotal int64
	for i, b := range aBuckets {
		applicants := aOneHandApplicants[i] + aOtherApplicants[i]
		subscribed := int64(applicants) * b.Lots
		var slots int64
		if b.Lots <= int64(lotSize) {
			slots = oneHandSlots
			if int64(applicants) < slots {
				slots = int64(applicants)
			}
		} else {
			// 非一手档：按衰减权重分货，使每手中签率随申购手数单调递减（边际效应递减）
			if totalWeightedShares > 0 && remainingSlots > 0 {
				tl := tierLots(b.Lots)
				singleUserWeight := math.Pow(float64(tl), 1.0-aGroupDecayFactor)
				bucketWeight := singleUserWeight * float64(applicants)
				ratio := bucketWeight / totalWeightedShares
				slots = int64(float64(remainingSlots) * ratio)
			}
		}
		allocated := slots * int64(lotSize)
		aAllocatedTotal += allocated
		aTiers = append(aTiers, TierInfo{Lots: b.Lots, Applicants: applicants, Group: "甲组"})
		info := WinRateInfo{
			Lots: b.Lots, Applicants: applicants, Group: "甲组",
			SubscribedShares: subscribed, AllocatedShares: allocated,
			AllocatedLots: allocated / int64(lotSize),
		}
		aWinRates = append(aWinRates, info)
		applyBottomUpAllocation(&aWinRates[len(aWinRates)-1], lotSize)
	}
	// 乙组中签率：乙头最高，大户略降（递减系数）
	bRateForTier := func(idx int) float64 {
		r := poolBRate
		if len(bBuckets) > 1 {
			decay := bGroupTierDecay * float64(idx) / float64(len(bBuckets)-1)
			r = poolBRate * (1.0 - decay)
			if r < 0 {
				r = 0
			}
		}
		return r
	}
	var bTiers []TierInfo
	var bWinRates []WinRateInfo
	var bAllocatedTotal int64
	for i, b := range bBuckets {
		applicants := bApplicantsByTier[i]
		tierRate := bRateForTier(i)
		subscribed := int64(applicants) * b.Lots
		allocated := int64(float64(subscribed) * tierRate)
		bAllocatedTotal += allocated
		bTiers = append(bTiers, TierInfo{Lots: b.Lots, Applicants: applicants, Group: "乙组"})
		bWinRates = append(bWinRates, WinRateInfo{
			Lots: b.Lots, Applicants: applicants, Group: "乙组",
			SubscribedShares: subscribed, AllocatedShares: allocated,
			AllocatedLots: allocated / int64(lotSize),
		})
		applyBottomUpAllocation(&bWinRates[len(bWinRates)-1], lotSize)
	}
	// 强制甲乙组各自总分配不超过池子 50%，多则按比例压回
	scaleAndFix := func(list []WinRateInfo, totalAllocated int64, poolSupply int64) {
		if len(list) == 0 || totalAllocated <= 0 {
			return
		}
		scale := 1.0
		if totalAllocated > poolSupply && poolSupply > 0 {
			scale = float64(poolSupply) / float64(totalAllocated)
		}
		for j := range list {
			list[j].AllocatedShares = int64(float64(list[j].AllocatedShares) * scale)
			list[j].AllocatedLots = list[j].AllocatedShares / int64(lotSize)
			applyBottomUpAllocation(&list[j], lotSize)
		}
	}
	scaleAndFix(aWinRates, aAllocatedTotal, poolSharesI)
	scaleAndFix(bWinRates, bAllocatedTotal, poolSharesI)

	// 乙组保底：乙头档（金额最小的乙组档）中签股数不低于甲尾档
	if len(aWinRates) > 0 && len(bWinRates) > 0 {
		lastA := aWinRates[len(aWinRates)-1]
		bHeadIdx := 0
		for i := range bBuckets {
			if bBuckets[i].AmountHKD < bBuckets[bHeadIdx].AmountHKD {
				bHeadIdx = i
			}
		}
		bHead := &bWinRates[bHeadIdx]
		if bHead.AllocatedShares < lastA.AllocatedShares {
			delta := lastA.AllocatedShares - bHead.AllocatedShares
			bHead.AllocatedShares = lastA.AllocatedShares
			bHead.AllocatedLots = bHead.AllocatedShares / int64(lotSize)
			applyBottomUpAllocation(bHead, lotSize)
			// 从其余乙组档位按比例扣减，使乙组总分配不超池子
			bRestTotal := int64(0)
			for j := range bWinRates {
				if j == bHeadIdx {
					continue
				}
				bRestTotal += bWinRates[j].AllocatedShares
			}
			if bRestTotal > 0 && delta > 0 {
				trim := delta
				if trim > bRestTotal {
					trim = bRestTotal
				}
				for j := range bWinRates {
					if j == bHeadIdx {
						continue
					}
					part := int64(float64(trim) * float64(bWinRates[j].AllocatedShares) / float64(bRestTotal))
					if part > bWinRates[j].AllocatedShares {
						part = bWinRates[j].AllocatedShares
					}
					bWinRates[j].AllocatedShares -= part
					bWinRates[j].AllocatedLots = bWinRates[j].AllocatedShares / int64(lotSize)
					applyBottomUpAllocation(&bWinRates[j], lotSize)
				}
			}
		}
	}

	// 甲乙组分别补齐到各自池子，避免总分配明显低于公开发售股数。
	rebalanceAllocatedShares(aWinRates, poolSharesI, lotSize)
	rebalanceAllocatedShares(bWinRates, publicShares-poolSharesI, lotSize)
	enforceHouseholdWinRateNonDecreasing(bWinRates, lotSize)
	comparablePeerCount := 0
	if req.AutoEstimateContext != nil {
		comparablePeerCount = max(0, req.AutoEstimateContext.ComparablePeerCount)
	}

	return PredictResult{
		Tiers:    append(aTiers, bTiers...),
		WinRates: append(aWinRates, bWinRates...),
		Meta: PredictMeta{
			RawEstimatedApplicants:           rawEstimatedApplicants,
			AutoEstimatedApplicants:          autoEstimatedApplicants,
			FinalEstimatedApplicants:         estimatedApplicants,
			AutoBGroupApplicantRatio:         autoBGroupRatio,
			FinalBGroupApplicantRatio:        bGroupRatio,
			AutoAOneHandApplicantRatio:       autoAOneHandRatio,
			FinalAOneHandApplicantRatio:      aOneHandRatio,
			UsedEstimatedApplicantsOverride:  usedEstimatedApplicantsOverride,
			UsedBGroupApplicantRatioOverride: usedBGroupRatioOverride,
			UsedAOneHandApplicantOverride:    usedAOneHandRatioOverride,
			ComparablePeerCount:              comparablePeerCount,
		},
	}, nil
}

// rebalanceAllocatedShares 将单组分配股数回调到目标值（优先按比例，再按余量补齐/扣减），
// 并保证展示字段与实际分配一致：PerLotRate=AllocatedShares/SubscribedShares，WinRate=WinApplicants/Applicants。
func rebalanceAllocatedShares(list []WinRateInfo, targetShares int64, lotSize int) {
	if len(list) == 0 || targetShares < 0 {
		return
	}

	sumAllocated := int64(0)
	for _, w := range list {
		if w.AllocatedShares > 0 {
			sumAllocated += w.AllocatedShares
		}
	}
	if sumAllocated == targetShares {
		for i := range list {
			list[i].AllocatedLots = list[i].AllocatedShares / int64(lotSize)
			applyBottomUpAllocation(&list[i], lotSize)
		}
		return
	}

	if sumAllocated > targetShares {
		excess := sumAllocated - targetShares
		total := sumAllocated
		reduced := int64(0)
		for i := range list {
			if list[i].AllocatedShares <= 0 {
				continue
			}
			cut := excess * list[i].AllocatedShares / total
			if cut > list[i].AllocatedShares {
				cut = list[i].AllocatedShares
			}
			list[i].AllocatedShares -= cut
			reduced += cut
		}
		rem := excess - reduced
		if rem > 0 {
			idx := make([]int, len(list))
			for i := range idx {
				idx[i] = i
			}
			sort.Slice(idx, func(i, j int) bool {
				return list[idx[i]].AllocatedShares > list[idx[j]].AllocatedShares
			})
			for rem > 0 {
				progress := false
				for _, i := range idx {
					if rem == 0 {
						break
					}
					if list[i].AllocatedShares <= 0 {
						continue
					}
					list[i].AllocatedShares--
					rem--
					progress = true
				}
				if !progress {
					break
				}
			}
		}
	} else {
		deficit := targetShares - sumAllocated
		totalHeadroom := int64(0)
		headroom := make([]int64, len(list))
		for i := range list {
			h := list[i].SubscribedShares - list[i].AllocatedShares
			if h < 0 {
				h = 0
			}
			headroom[i] = h
			totalHeadroom += h
		}
		if totalHeadroom > 0 {
			added := int64(0)
			for i := range list {
				if headroom[i] <= 0 {
					continue
				}
				add := deficit * headroom[i] / totalHeadroom
				if add > headroom[i] {
					add = headroom[i]
				}
				list[i].AllocatedShares += add
				added += add
			}
			rem := deficit - added
			if rem > 0 {
				idx := make([]int, len(list))
				for i := range idx {
					idx[i] = i
				}
				sort.Slice(idx, func(i, j int) bool {
					hi := list[idx[i]].SubscribedShares - list[idx[i]].AllocatedShares
					hj := list[idx[j]].SubscribedShares - list[idx[j]].AllocatedShares
					return hi > hj
				})
				for rem > 0 {
					progress := false
					for _, i := range idx {
						if rem == 0 {
							break
						}
						h := list[i].SubscribedShares - list[i].AllocatedShares
						if h <= 0 {
							continue
						}
						list[i].AllocatedShares++
						rem--
						progress = true
					}
					if !progress {
						break
					}
				}
			}
		}
	}

	for i := range list {
		list[i].AllocatedLots = list[i].AllocatedShares / int64(lotSize)
		applyBottomUpAllocation(&list[i], lotSize)
	}
}

func enforceHouseholdWinRateNonDecreasing(list []WinRateInfo, lotSize int) {
	if len(list) < 2 || lotSize <= 0 {
		return
	}
	lotSizeI := int64(lotSize)
	for pass := 0; pass < len(list)*len(list)*4; pass++ {
		changed := false
		for i := 1; i < len(list); i++ {
			prev := &list[i-1]
			curr := &list[i]
			if prev.Applicants <= 0 || curr.Applicants <= 0 {
				continue
			}
			prevWinLots := min(prev.AllocatedLots, int64(prev.Applicants))
			currWinLots := min(curr.AllocatedLots, int64(curr.Applicants))
			if prevWinLots <= 0 {
				continue
			}
			left := prevWinLots*int64(curr.Applicants) - currWinLots*int64(prev.Applicants)
			if left <= 0 {
				continue
			}
			moveLots := (left + int64(prev.Applicants+curr.Applicants) - 1) / int64(prev.Applicants+curr.Applicants)
			if moveLots <= 0 {
				moveLots = 1
			}
			if moveLots > prevWinLots {
				moveLots = prevWinLots
			}
			currHeadroomLots := curr.SubscribedShares/lotSizeI - curr.AllocatedLots
			if currHeadroomLots <= 0 {
				continue
			}
			if moveLots > currHeadroomLots {
				moveLots = currHeadroomLots
			}
			prev.AllocatedLots -= moveLots
			prev.AllocatedShares = prev.AllocatedLots * lotSizeI
			curr.AllocatedLots += moveLots
			curr.AllocatedShares = curr.AllocatedLots * lotSizeI
			applyBottomUpAllocation(prev, lotSize)
			applyBottomUpAllocation(curr, lotSize)
			changed = true
		}
		if !changed {
			return
		}
	}
}
