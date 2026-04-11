# 港股 IPO 预测算法模型技术规范 (IPO Prediction Model Spec)

## 1. 核心逻辑概述
本模型旨在通过实时追踪的**券商孖展数据 (Broker Margin)**，反推总冻资规模与真实超额倍数，进而模拟申购人数分布（散户 vs 大户），最终计算出甲乙组的中签率曲线。

**核心假设：**
1. **无回拨机制 (No Clawback)：** 甲乙组分配比例固定为 50% : 50%。
2. **孖展驱动 (Margin Driven)：** 公开的券商孖展数据是推算总热度的唯一动态变量。
3. **红鞋偏好 (Red Shoe)：** 小资金（一手党）享有一定的分配倾斜。

---

## 2. 输入参数 (Input Parameters)

| 参数名 | 类型 | 说明 | 示例 |
| :--- | :--- | :--- | :--- |
| `public_shares` | Int | 公开发售总股数 | 25,000,000 |
| `lot_size` | Int | 每手股数 | 100 |
| `price` | Float | 发行价（通常取上限） | 50.00 |
| `broker_margin_sum` | Float | **[核心]** 各大券商统计到的孖展总额 | 30,000,000,000 |
| `price_buckets` | List | 申购阶梯表 (手数, 金额) | `[(1, 5000), (10, 50000)...]` |

---

## 3. 算法模型步骤

### 步骤 I：宏观热度推演 (Macro Estimation)

通过已知的孖展数据，反推全市场冻资额。

* **变量定义：**
    * `Margin_Coverage_Ratio` (孖展覆盖率系数)：估算当前统计到的孖展占全市场总冻资的比例。
    * *逻辑规则：* 热度越高，系数越大（全员孖展）；热度越低，系数越小（现金白嫖多）。
        * Hot (>100x): `0.65`
        * Warm (15-100x): `0.50`
        * Cold (<15x): `0.35`

* **计算公式：**
    $$\text{Total Frozen} = \frac{\text{Broker Margin Sum}}{\text{Margin Coverage Ratio}}$$
    $$\text{Real Oversubscription} = \frac{\text{Total Frozen}}{\text{Public Shares} \times \text{Price}}$$

### 步骤 II：申购人数画像 (Applicant Profiling)

总钱数确定后，需要拆解成“人数”。核心在于估算**人均申购规模 (Avg Ticket Size)**。

* **变量定义：**
    * `Base_Lots`：基础人均手数（通常为 1-3 手）。
    * `Heat_Factor`：热度驱动的加码系数。

* **计算逻辑：**
    $$\text{Avg Lots Per Person} = \text{Base Lots} + \left( \frac{\text{Real Oversubscription}}{50} \right)$$
    *(上限可设为 50 手，防止数据失真)*
    
    $$\text{Estimated Applicants} = \frac{\text{Total Frozen}}{\text{Avg Lots Per Person} \times \text{Lot Size} \times \text{Price}}$$

### 步骤 III：甲乙组分配率计算 (Allocation Rates)

基于**无回拨 (50/50)** 假设进行计算。

#### A. 乙组中签率 (Pool B Rate) - 线性分配
假设乙组资金占总冻资的 50%（简化模型，实际可波动）。

$$\text{Pool B Supply} = \frac{\text{Public Shares}}{2}$$
$$\text{Pool B Demand Shares} = \frac{\text{Total Frozen} \times 0.5}{\text{Price}}$$
$$\text{Pool B Rate} = \frac{\text{Pool B Supply}}{\text{Pool B Demand Shares}}$$

#### B. 甲组一手中签率 (One Lot Rate) - 红鞋机制
$$\text{Pool A Supply} = \frac{\text{Public Shares}}{2}$$
$$\text{Avg Shares Per Applicant} = \frac{\text{Pool A Supply}}{\text{Estimated Applicants}}$$

$$\text{One Lot Rate} = \frac{\text{Avg Shares Per Applicant}}{\text{Lot Size}} \times \text{Red Shoe Factor}$$

* `Red Shoe Factor` (红鞋系数)：通常取 `1.2` 到 `1.5`。热度越高，为了普惠，系数越高。
* *约束：* 结果需限制在 `[Min_Rate, 100%]` 之间。

---

## 4. 阶梯分布算法 (Bucket Distribution Logic)

我们需要填充 `price_buckets` 中每一档的中签率。
通常中签率曲线是一条**衰减曲线**：从 `One Lot Rate` 逐渐衰减至 `Pool B Rate`。

**推荐插值算法 (Weighted Decay)：**

对于甲组内的任意档位 $N$ (手)：
1.  **线性期望率**：$R_{linear} = \text{Pool B Rate}$ (随着资金量增大，趋向于平均分配)
2.  **红鞋溢价率**：$R_{premium} = \frac{\text{One Lot Rate}}{N}$ (一手率除以手数)
3.  **合成公式**：
    $$\text{Rate}(N) = \alpha \cdot R_{premium} + (1 - \alpha) \cdot R_{linear}$$
    * $\alpha$ 是衰减权重，随着 $N$ 增大而减小（例如 $\alpha = 1 / \sqrt{N}$），意味着手数越多，越不享受红鞋照顾，越接近乙组分配率。

---

## 5. Python 实现代码片段

```python
import math

def calculate_ipo_prediction(public_shares, lot_size, price, margin_data, buckets):
    # --- 配置参数 (Tunable Constants) ---
    # 根据历史回测调整这些参数
    MARGIN_COVERAGE_RATIO = 0.60  # 假设当前孖展覆盖了60%的市场资金
    RED_SHOE_FACTOR = 1.3         # 红鞋照顾系数
    BASE_AVG_LOTS = 3             # 基础人均申购手数
    
    # --- 1. 宏观推演 ---
    total_frozen = margin_data / MARGIN_COVERAGE_RATIO
    fundraising_amt = public_shares * price
    oversubscription = total_frozen / fundraising_amt
    
    # --- 2. 人数推演 ---
    # 热度越高，人均手数越多
    avg_lots = BASE_AVG_LOTS + (oversubscription / 40)
    avg_ticket_money = avg_lots * lot_size * price
    applicants = int(total_frozen / avg_ticket_money)
    
    # --- 3. 基础分配率 ---
    # 无回拨模式：甲乙各50%
    pool_shares = public_shares / 2
    
    # 乙组率 (Pool B) = 供应 / 需求
    # 假设乙组资金占总冻资一半
    pool_b_demand_shares = (total_frozen * 0.5) / price
    pool_b_rate = pool_shares / pool_b_demand_shares if pool_b_demand_shares > 0 else 1.0
    
    # 甲组一手率 (Pool A)
    # 平均每人能分到的股数
    avg_shares_per_head = pool_shares / applicants if applicants > 0 else pool_shares
    # 红鞋机制修正
    one_lot_rate = (avg_shares_per_head / lot_size) * RED_SHOE_FACTOR
    one_lot_rate = min(1.0, max(0.01, one_lot_rate)) # 限制在 1%-100%
    
    # --- 4. 阶梯计算 (Buckets) ---
    results = []
    for lots, amount in buckets:
        if lots * lot_size * price > 5000000:
            # 乙组逻辑：直接应用乙组率
            rate = pool_b_rate
        else:
            # 甲组逻辑：权重衰减插值
            # 随着手数增加，权重 alpha 降低，中签率向 pool_b_rate 收敛
            alpha = 1 / math.sqrt(lots) 
            rate_premium = one_lot_rate / lots # 纯红鞋逻辑下的单手率
            
            # 混合算法
            rate = (alpha * one_lot_rate) + ((1-alpha) * pool_b_rate * lots)
            # 归一化为"该档位的中签概率" (不是中签手数)
            # 注意：这里计算的是该档位"中一手"的概率近似值
            # 如果要算"中签手数期望"，公式需微调
            
            # 简化输出：中签率 (Probability of winning at least 1 lot)
            # 对于多手申购，中签率通常指 "中签股数 / 申购股数" 或者 "获配市值 / 申购本金"
            # 这里返回：获配率 (Allocation Percentage)
            rate = (alpha * one_lot_rate/1.0) + ((1-alpha) * pool_b_rate)
            
        results.append({
            "lots": lots,
            "amount": amount,
            "alloc_rate_pct": round(rate * 100, 2),
            "estimated_shares": round(lots * lot_size * rate)
        })

    return {
        "oversubscription": round(oversubscription, 2),
        "applicants": applicants,
        "one_lot_rate_pct": round(one_lot_rate * 100, 2),
        "pool_b_rate_pct": round(pool_b_rate * 100, 4),
        "buckets_detail": results
    }
```

---

## 6. 档位输出与中签率定义 (Go 实现)

`pkg/ipo_predict` 中每个档位返回 `WinRateInfo`，包含两类中签率字段，含义不同：

| 字段 | 含义 | 计算公式 | 随档位变化 |
| :--- | :--- | :--- | :--- |
| **PerLotRate** | **每手中签率** | `AllocatedShares / SubscribedShares` | 同组内随档位（申购手数）上升而**阶梯式降低**：一手档最高，大户档更低。 |
| **WinRate** | **每户中签率** | `WinApplicants / Applicants` | 随档位上升而**上升**：申购越多，该档“至少中 1 手”的人数占比越高。 |

* **每手中签率**：该档「分配到的股数 ÷ 申购的股数」，即每一手申购被满足的概率；红鞋下低档位更有利，故随档位升高而降低。
* **每户中签率**：该档「获至少 1 手的人数 ÷ 该档申请人总数」；大户因分配到的货相对更多，至少中 1 手的比例更高。
* **WinApplicants**：该档获至少 1 手的人数；**LotDistribution** 文案据此生成（如「X名申请人中的Y名获发1手」）。