package webapp

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"hk_ipo/orm"
	"hk_ipo/pkg/app/collectorapp"
	"hk_ipo/pkg/ipo_predict"
	"hk_ipo/pkg/ipoprior"
	"hk_ipo/pkg/pdfreader"
	"hk_ipo/pkg/storage/gormmodel"
)

//go:embed templates
var templateFS embed.FS

type server struct {
	db  *gorm.DB
	tpl *template.Template
}

func Run(addr string) error {
	if err := orm.Init(); err != nil {
		return fmt.Errorf("db init error: %w", err)
	}

	tpl := template.Must(template.New("").
		Funcs(template.FuncMap{
			"fmtTime":     fmtTime,
			"fmtNullTime": fmtNullTime,
			"roleLabel":   roleLabel,
			"fmtInt64Comma": func(v int64) string {
				return formatIntWithComma(strconv.FormatInt(v, 10))
			},
			"fmtFloatComma": func(v float64, digits int) string {
				if digits < 0 {
					digits = 0
				}
				s := strconv.FormatFloat(v, 'f', digits, 64)
				parts := strings.SplitN(s, ".", 2)
				intPart := formatIntWithComma(parts[0])
				if len(parts) == 2 {
					return intPart + "." + parts[1]
				}
				return intPart
			},
			"fmtDateRange": func(start, end sql.NullTime) string {
				if !start.Valid && !end.Valid {
					return "-"
				}
				if start.Valid && end.Valid {
					return start.Time.Format("2006-01-02") + "→" + end.Time.Format("2006-01-02")
				}
				if start.Valid {
					return start.Time.Format("2006-01-02")
				}
				return end.Time.Format("2006-01-02")
			},
			"fmtUnix": func(sec int64) string {
				if sec <= 0 {
					return "-"
				}
				return time.Unix(sec, 0).Local().Format("2006-01-02 15:04:05")
			},
			"fmtNullFloat": func(v sql.NullFloat64, digits int) string {
				if !v.Valid {
					return "-"
				}
				if digits <= 0 {
					return strconv.FormatFloat(v.Float64, 'f', -1, 64)
				}
				return strconv.FormatFloat(v.Float64, 'f', digits, 64)
			},
			"fmtNullInt64": func(v sql.NullInt64) string {
				if !v.Valid {
					return "-"
				}
				return strconv.FormatInt(v.Int64, 10)
			},
			"pct": func(f float64) string {
				return fmt.Sprintf("%.2f%%", f*100)
			},
			"fmtFloat": func(f float64) string {
				return strconv.FormatFloat(f, 'f', -1, 64)
			},
			"fmtHKDMoney": formatHKDMoney,
			"fmtPctTrunc2": func(v float64) string {
				return formatPctTrunc(v, 2)
			},
			"fmtRatePctTrunc2": func(v float64) string {
				return formatPctTrunc(v*100, 2)
			},
			"inc": func(i int) int {
				return i + 1
			},
			"fmtMinWinLots": func(applicants, winApplicants int, allocatedLots int64) string {
				return strconv.FormatInt(calcMinWinLots(applicants, winApplicants, allocatedLots), 10) + "手"
			},
			"calcAllotmentTierAmountHKD": calcAllotmentTierAmountHKD,
			"allocationLabel":            allocationMechanismLabel,
		}).
		ParseFS(templateFS, "templates/*.html"))

	s := &server{db: orm.DB, tpl: tpl}
	startAutoSync()

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleList)
	mux.HandleFunc("/stocks", s.handleList)
	mux.HandleFunc("/predict/", s.handlePredict)
	mux.HandleFunc("/stocks/", s.handleDetail)
	mux.HandleFunc("/api/stocks", s.handleAPIStocks)
	mux.HandleFunc("/api/intermediaries", s.handleAPIIntermediaries)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("web listening on %s", addr)
	log.Printf("db config via env: HK_IPO_DB_DSN or HK_IPO_DB_PATH")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen error: %w", err)
	}
	return nil
}

// ---- handlers ----

// Intermediary roles (must match stock_intermediaries.role in DB).
const (
	RoleSponsor           = "sponsor"
	RoleUnderwriter       = "underwriter"
	RoleBookrunner        = "bookrunner"
	RoleGlobalCoordinator = "global_coordinator"
)

func (s *server) handleList(w http.ResponseWriter, r *http.Request) {
	// 列表页改为接口加载：只输出壳子，数据由 /api/stocks、/api/intermediaries 在 H5 内请求
	data := struct{ Title string }{Title: "HK IPO"}
	var buf bytes.Buffer
	if err := s.tpl.ExecuteTemplate(&buf, "list_client.html", data); err != nil {
		http.Error(w, "render list: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (s *server) handleDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	code := strings.TrimPrefix(r.URL.Path, "/stocks/")
	code = strings.TrimSpace(code)
	if code == "" || strings.Contains(code, "/") {
		http.NotFound(w, r)
		return
	}

	d, err := getStockDetailByCode(ctx, s.db, code)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Title string
		D     stockDetailPage
	}{
		Title: fmt.Sprintf("%s %s", d.Stock.StockCode, d.Stock.StockName),
		D:     d,
	}

	var buf bytes.Buffer
	if err := s.tpl.ExecuteTemplate(&buf, "detail.html", data); err != nil {
		http.Error(w, "render detail: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// predictRequestDisplay 用于在预测结果页展示的入参（与 ipo_predict.MarginRequest 一致）
type predictRequestDisplay struct {
	PublicShares                int64
	OriginalPublicShares        int64
	AllocationMechanism         string
	PublicSharesAdjusted        bool
	PublicSharesAdjustmentNote  string
	LotSize                     int
	Price                       float64
	BrokerMarginSum             float64
	EstimatedApplicantsOverride int
	BGroupRatioOverride         float64
	AOneHandRatioOverride       float64
	AutoEstimateContext         *ipo_predict.AutoEstimateContext
	Buckets                     []ipo_predict.Tier
}

// predictPage 预测结果页数据
type predictPage struct {
	Stock                    gormmodel.Stock
	Request                  *predictRequestDisplay // 调用预测时的入参
	RequestJSON              string                 // 入参 JSON 化，用于页面展示
	LiveMargin               *vBrokerMarginData
	InputSub                 float64 // 页面输入：认购倍数
	InputMargin              float64 // 页面输入：孖展总额
	InputSubText             string  // 页面展示：认购倍数字符串
	InputMarginText          string  // 页面展示：孖展总额字符串
	InputEstimatedApplicants string  // 页面输入：总申请人数 override
	InputBGroupRatio         string  // 页面输入：乙组占比 override
	InputAOneHandRatio       string  // 页面输入：甲组一手占比 override
	Result                   *ipo_predict.PredictResult
	GroupACount              int // 甲组人数合计
	GroupBCount              int // 乙组人数合计
	GroupARatio              float64
	GroupBRatio              float64
	TotalApplicants          int    // 申请人数总和
	TotalAllocatedLots       int64  // 中签手数总和
	TotalWinApplicants       int    // 中签人数总和
	ErrMsg                   string // 数据不足或预测失败时提示
}

const defaultPredictSubscriptionMultiple = 1000.0

type predictInputPayload struct {
	Sub                 string
	Margin              string
	EstimatedApplicants string
	BRatio              string
	AOneLotRatio        string
}

func (s *server) handlePredict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	code := strings.TrimPrefix(r.URL.Path, "/predict/")
	code = strings.TrimSpace(code)
	if code == "" || strings.Contains(code, "/") {
		http.NotFound(w, r)
		return
	}

	var stock gormmodel.Stock
	if err := s.db.WithContext(ctx).Where("stock_code = ?", code).First(&stock).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	page := predictPage{Stock: stock}

	var offering gormmodel.StockOffering
	if err := s.db.WithContext(ctx).Where("stock_id = ?", stock.ID).First(&offering).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			page.ErrMsg = "缺少发行信息，无法预测"
		} else {
			page.ErrMsg = "查询发行信息失败: " + err.Error()
		}
		s.renderPredict(w, page)
		return
	}
	defaultSubMultiple := defaultPredictSubscriptionMultiple
	var summary gormmodel.StockAllotmentSummary
	hasAllotmentSummary := false
	if err := s.db.WithContext(ctx).Where("stock_id = ?", stock.ID).First(&summary).Error; err == nil {
		hasAllotmentSummary = true
		defaultSubMultiple = resolveDefaultPredictSubscriptionMultiple(summary.SubscriptionMultiple)
	}
	if !hasAllotmentSummary || summary.SubscriptionMultiple <= 0 {
		if liveMargin, err := newVBrokerMarginClient(nil).fetchByStockCode(ctx, stock.StockCode); err == nil && liveMargin != nil {
			page.LiveMargin = liveMargin
			if liveMargin.ApplyRate > 0 {
				defaultSubMultiple = liveMargin.ApplyRate
			}
		}
	}
	price := offering.OfferPrice
	if price <= 0 {
		price = offering.OfferPriceHigh
	}
	if offering.PublicOfferShares <= 0 || offering.LotSize <= 0 || price <= 0 {
		page.ErrMsg = "发行信息不完整（公开发售股数/每手股数/发行价需大于 0）"
		s.renderPredict(w, page)
		return
	}
	predictPublicShares, publicSharesAdjusted, publicSharesNote := effectivePredictPublicShares(offering)
	effectiveOffering := offering
	effectiveOffering.PublicOfferShares = predictPublicShares
	fundraisingAmt := float64(predictPublicShares) * price
	input, err := parsePredictInput(r)
	if err != nil {
		page.ErrMsg = "请求参数 JSON 格式错误: " + err.Error()
		s.renderPredict(w, page)
		return
	}
	subRaw := strings.TrimSpace(input.Sub)
	marginRaw := strings.TrimSpace(input.Margin)
	marginInput, err := parsePositiveFloatQuery(marginRaw)
	if err != nil {
		page.ErrMsg = "margin 参数格式错误（需为正数）"
		s.renderPredict(w, page)
		return
	}
	subInput, err := parsePositiveFloatQuery(subRaw)
	if err != nil {
		page.ErrMsg = "sub 参数格式错误（需为正数）"
		s.renderPredict(w, page)
		return
	}
	page.InputEstimatedApplicants = strings.TrimSpace(input.EstimatedApplicants)
	estimatedApplicantsOverride, err := parsePositiveIntQuery(page.InputEstimatedApplicants)
	if err != nil {
		page.ErrMsg = "estimatedApplicants 参数格式错误（需为正整数）"
		s.renderPredict(w, page)
		return
	}
	page.InputBGroupRatio = strings.TrimSpace(input.BRatio)
	bGroupRatioInput, err := parseRatioQuery(page.InputBGroupRatio)
	if err != nil {
		page.ErrMsg = "bRatio 参数格式错误（需为正数，可填 0~1 或 0~100）"
		s.renderPredict(w, page)
		return
	}
	page.InputAOneHandRatio = strings.TrimSpace(input.AOneLotRatio)
	aOneHandRatioInput, err := parseRatioQuery(page.InputAOneHandRatio)
	if err != nil {
		page.ErrMsg = "aOneLotRatio 参数格式错误（需为正数，可填 0~1 或 0~100）"
		s.renderPredict(w, page)
		return
	}
	effectiveSub := 0.0
	brokerMarginSum := 0.0
	switch {
	case subRaw != "":
		effectiveSub = subInput
		brokerMarginSum = inferBrokerMarginSumFromSubscriptionMultiple(fundraisingAmt, subInput)
		page.InputSubText = strconv.FormatFloat(subInput, 'f', -1, 64)
	case marginRaw != "":
		brokerMarginSum = marginInput
		effectiveSub = inferRealSubscriptionMultipleFromBrokerMarginSum(fundraisingAmt, marginInput)
		page.InputMarginText = strconv.FormatFloat(marginInput, 'f', -1, 64)
	default:
		effectiveSub = defaultSubMultiple
		brokerMarginSum = inferBrokerMarginSumFromSubscriptionMultiple(fundraisingAmt, effectiveSub)
		page.InputSubText = strconv.FormatFloat(effectiveSub, 'f', -1, 64)
	}
	page.InputSub = effectiveSub
	page.InputMargin = brokerMarginSum

	buckets, err := s.buildBucketsFromProspectus(ctx, stock.ID, effectiveOffering, price)
	if err != nil {
		page.ErrMsg = "无法从招股书提取真实申购档位: " + err.Error()
		s.renderPredict(w, page)
		return
	}
	currentSubMultiple := effectiveSub
	if currentSubMultiple <= 0 {
		currentSubMultiple = inferRealSubscriptionMultipleFromBrokerMarginSum(fundraisingAmt, brokerMarginSum)
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		page.ErrMsg = "获取数据库连接失败: " + err.Error()
		s.renderPredict(w, page)
		return
	}
	stockFeatures, err := ipoprior.LoadStockFeatures(ctx, sqlDB)
	if err != nil {
		page.ErrMsg = "加载自动估计特征失败: " + err.Error()
		s.renderPredict(w, page)
		return
	}
	autoCtx, _, err := ipoprior.BuildAutoEstimateContextByStockID(stockFeatures, int64(stock.ID), currentSubMultiple)
	if err != nil {
		page.ErrMsg = "构建自动估计上下文失败: " + err.Error()
		s.renderPredict(w, page)
		return
	}

	req := ipo_predict.MarginRequest{
		PublicShares:                predictPublicShares,
		LotSize:                     offering.LotSize,
		Price:                       price,
		BrokerMarginSum:             brokerMarginSum,
		EstimatedApplicantsOverride: estimatedApplicantsOverride,
		BGroupApplicantRatio:        bGroupRatioInput,
		AOneHandApplicantRatio:      aOneHandRatioInput,
		AutoEstimateContext:         &autoCtx,
		Buckets:                     buckets,
	}
	result, err := ipo_predict.Predict(req)
	if err != nil {
		page.ErrMsg = "预测失败: " + err.Error()
		s.renderPredict(w, page)
		return
	}
	page.Request = &predictRequestDisplay{
		PublicShares:                req.PublicShares,
		OriginalPublicShares:        offering.PublicOfferShares,
		AllocationMechanism:         strings.TrimSpace(offering.AllocationMechanism),
		PublicSharesAdjusted:        publicSharesAdjusted,
		PublicSharesAdjustmentNote:  publicSharesNote,
		LotSize:                     req.LotSize,
		Price:                       req.Price,
		BrokerMarginSum:             req.BrokerMarginSum,
		EstimatedApplicantsOverride: req.EstimatedApplicantsOverride,
		BGroupRatioOverride:         req.BGroupApplicantRatio,
		AOneHandRatioOverride:       req.AOneHandApplicantRatio,
		AutoEstimateContext:         req.AutoEstimateContext,
		Buckets:                     req.Buckets,
	}
	if b, _ := json.Marshal(page.Request); len(b) > 0 {
		page.RequestJSON = string(b)
	}
	page.Result = &result
	for _, wr := range result.WinRates {
		if wr.Group == "甲组" {
			page.GroupACount += wr.Applicants
		} else if wr.Group == "乙组" {
			page.GroupBCount += wr.Applicants
		}
		page.TotalApplicants += wr.Applicants
		page.TotalAllocatedLots += wr.AllocatedLots
		page.TotalWinApplicants += wr.WinApplicants
	}
	if page.TotalApplicants > 0 {
		page.GroupARatio = float64(page.GroupACount) / float64(page.TotalApplicants)
		page.GroupBRatio = float64(page.GroupBCount) / float64(page.TotalApplicants)
	}
	s.renderPredict(w, page)
}

func parsePredictInput(r *http.Request) (predictInputPayload, error) {
	if r.Method != http.MethodPost {
		return predictInputPayload{}, nil
	}
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return predictInputPayload{}, nil
		}
		return predictInputPayload{}, err
	}
	field := func(name string) (string, error) {
		return predictJSONFieldString(raw[name])
	}
	sub, err := field("sub")
	if err != nil {
		return predictInputPayload{}, fmt.Errorf("sub: %w", err)
	}
	margin, err := field("margin")
	if err != nil {
		return predictInputPayload{}, fmt.Errorf("margin: %w", err)
	}
	estimatedApplicants, err := field("estimatedApplicants")
	if err != nil {
		return predictInputPayload{}, fmt.Errorf("estimatedApplicants: %w", err)
	}
	bRatio, err := field("bRatio")
	if err != nil {
		return predictInputPayload{}, fmt.Errorf("bRatio: %w", err)
	}
	aOneLotRatio, err := field("aOneLotRatio")
	if err != nil {
		return predictInputPayload{}, fmt.Errorf("aOneLotRatio: %w", err)
	}
	return predictInputPayload{
		Sub:                 sub,
		Margin:              margin,
		EstimatedApplicants: estimatedApplicants,
		BRatio:              bRatio,
		AOneLotRatio:        aOneLotRatio,
	}, nil
}

func resolveDefaultPredictSubscriptionMultiple(publicSubscriptionMultiple float64) float64 {
	if publicSubscriptionMultiple > 0 {
		return publicSubscriptionMultiple
	}
	return defaultPredictSubscriptionMultiple
}

func effectivePredictPublicShares(offering gormmodel.StockOffering) (int64, bool, string) {
	original := offering.PublicOfferShares
	if !isChapter18CMechanism(offering.AllocationMechanism) {
		return original, false, ""
	}
	if offering.GlobalOfferShares <= 0 || offering.LotSize <= 0 {
		return original, false, "18C 机制已识别，但缺少全球发售股数或每手股数，沿用原公开发售股数"
	}
	target := roundSharesToLot(float64(offering.GlobalOfferShares)*0.20, offering.LotSize)
	if target <= 0 {
		return original, false, "18C 机制已识别，但 20% 公开发售股数计算无效，沿用原公开发售股数"
	}
	if target == original {
		return original, false, "18C 机制已识别，公开发售股数已为全球发售 20%"
	}
	note := fmt.Sprintf("18C 预测按全球发售 20%% 重置公开发售股数：%d -> %d", original, target)
	return target, true, note
}

func isChapter18CMechanism(mechanism string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mechanism)), "chapter_18c")
}

func allocationMechanismLabel(mechanism string) string {
	switch strings.ToLower(strings.TrimSpace(mechanism)) {
	case "mechanism_b":
		return "B"
	case "mechanism_b_likely":
		return "B?"
	case "mechanism_a":
		return "A"
	case "mechanism_a_or_18c_likely":
		return "A/18C?"
	case "chapter_18c":
		return "18C"
	case "chapter_18c_pre_commercial":
		return "18C-P"
	case "unknown_biotech_marker":
		return "Bio?"
	case "unknown", "":
		return "-"
	default:
		return mechanism
	}
}

func roundSharesToLot(shares float64, lotSize int) int64 {
	if shares <= 0 || lotSize <= 0 {
		return 0
	}
	lots := int64(math.Round(shares / float64(lotSize)))
	if lots <= 0 {
		lots = 1
	}
	return lots * int64(lotSize)
}

func predictJSONFieldString(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(x), nil
	case json.Number:
		return strings.TrimSpace(x.String()), nil
	default:
		return "", fmt.Errorf("must be string or number")
	}
}

// inferBrokerMarginSumFromSubscriptionMultiple 将“真实总超购倍数”反推为模型入参 BrokerMarginSum。
// Predict 内部会按 approxOversub=BrokerMarginSum/fundraising 估算覆盖率，再还原 realOversub。
// 这里做分段反推，避免把 realOversub 直接当 approxOversub 造成系统性高估。
func inferBrokerMarginSumFromSubscriptionMultiple(fundraisingAmt, realOversub float64) float64 {
	if fundraisingAmt <= 0 || realOversub <= 0 {
		return 0
	}
	approxOversub := realOversub
	switch {
	case realOversub > 200:
		approxOversub = realOversub * 0.98
	case realOversub > 30:
		approxOversub = realOversub * 0.50
	default:
		approxOversub = realOversub * 0.35
	}
	return fundraisingAmt * approxOversub
}

func inferRealSubscriptionMultipleFromBrokerMarginSum(fundraisingAmt, brokerMarginSum float64) float64 {
	if fundraisingAmt <= 0 || brokerMarginSum <= 0 {
		return 0
	}
	approxOversub := brokerMarginSum / fundraisingAmt
	switch {
	case approxOversub > 100:
		return approxOversub / 0.98
	case approxOversub > 15:
		return approxOversub / 0.50
	default:
		return approxOversub / 0.35
	}
}

func parsePositiveFloatQuery(v string) (float64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, err
	}
	if f <= 0 {
		return 0, fmt.Errorf("must be > 0")
	}
	return f, nil
}

func parsePositiveIntQuery(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be > 0")
	}
	return n, nil
}

func parseRatioQuery(v string) (float64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, err
	}
	if f <= 0 {
		return 0, fmt.Errorf("must be > 0")
	}
	// 允许两种输入：0~1 或 0~100（百分比）
	if f > 1 {
		if f > 100 {
			return 0, fmt.Errorf("must be in (0,1] or (0,100]")
		}
		f = f / 100
	}
	return f, nil
}

func (s *server) buildBucketsFromProspectus(ctx context.Context, stockID uint64, offering gormmodel.StockOffering, price float64) ([]ipo_predict.Tier, error) {
	if offering.LotSize <= 0 || offering.PublicOfferShares <= 0 || price <= 0 {
		return nil, fmt.Errorf("发行信息不完整（lot_size/public_offer_shares/price）")
	}

	// 优先使用数据库已存档位（稳定，不依赖外网）。
	if buckets, err := s.loadBucketsFromDBTiers(ctx, stockID, price); err == nil && len(buckets) > 0 {
		return buckets, nil
	}

	prospectusURL := strings.TrimSpace(offering.ProspectusUrl)
	entryFee := offering.AdmissionFeeHkd

	type rawRow struct {
		Label string
		Value string
	}
	var rows []rawRow
	if err := s.db.WithContext(ctx).
		Table("stock_raw_items i").
		Select("i.label,i.value").
		Joins("JOIN stock_raw_sections rs ON rs.id = i.raw_section_id").
		Where("rs.stock_id = ? AND i.label IN ?", stockID, []string{"入場費", "招股文件"}).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取 raw 档位锚点失败: %w", err)
	}
	for _, r := range rows {
		switch strings.TrimSpace(r.Label) {
		case "入場費":
			if entryFee <= 0 {
				entryFee = parseAmountFromText(r.Value)
			}
		case "招股文件":
			if prospectusURL == "" {
				prospectusURL = strings.TrimSpace(r.Value)
			}
		}
	}
	if prospectusURL == "" {
		// 无 URL 时退化到默认档位，保证预测页可用。
		buckets := buildDefaultBuckets(offering.LotSize, price, offering.PublicOfferShares)
		if len(buckets) > 0 {
			return buckets, nil
		}
		return nil, fmt.Errorf("缺少招股书 URL，且默认档位构建失败")
	}

	tmpPath, err := downloadToTempFile(prospectusURL)
	if err != nil {
		// 下载失败时也退化到默认档位，避免页面硬失败。
		buckets := buildDefaultBuckets(offering.LotSize, price, offering.PublicOfferShares)
		if len(buckets) > 0 {
			return buckets, nil
		}
		return nil, fmt.Errorf("下载招股书失败: %w", err)
	}
	defer os.Remove(tmpPath)

	anchorShares := strconv.Itoa(offering.LotSize)
	anchorPrices := make([]string, 0, 2)
	if entryFee > 0 {
		anchorPrices = append(anchorPrices, strconv.FormatFloat(entryFee, 'f', 2, 64))
	}
	lotMoney := float64(offering.LotSize) * price
	if lotMoney > 0 {
		anchorPrices = append(anchorPrices, strconv.FormatFloat(lotMoney, 'f', 2, 64))
	}

	var extractErr error
	for _, ap := range anchorPrices {
		res, err := pdfreader.ExtractTableFromAnchor(tmpPath, anchorShares, ap)
		if err != nil {
			extractErr = err
			continue
		}
		buckets := make([]ipo_predict.Tier, 0, len(res.Key1))
		seen := make(map[int64]struct{}, len(res.Key1))
		for _, c := range res.Key1 {
			lots := int64(c.Shares)
			if lots <= 0 {
				continue
			}
			if _, ok := seen[lots]; ok {
				continue
			}
			seen[lots] = struct{}{}
			buckets = append(buckets, ipo_predict.Tier{
				Lots:      lots,
				AmountHKD: c.TotalPrice,
			})
		}
		if len(buckets) == 0 {
			continue
		}
		sort.Slice(buckets, func(i, j int) bool { return buckets[i].Lots < buckets[j].Lots })
		return buckets, nil
	}
	if extractErr != nil {
		buckets := buildDefaultBuckets(offering.LotSize, price, offering.PublicOfferShares)
		if len(buckets) > 0 {
			return buckets, nil
		}
		return nil, extractErr
	}
	buckets := buildDefaultBuckets(offering.LotSize, price, offering.PublicOfferShares)
	if len(buckets) > 0 {
		return buckets, nil
	}
	return nil, fmt.Errorf("招股书未提取到档位表")
}

func (s *server) loadBucketsFromDBTiers(ctx context.Context, stockID uint64, price float64) ([]ipo_predict.Tier, error) {
	type lotsRow struct {
		Lots int64 `gorm:"column:lots"`
	}
	var rows []lotsRow
	if err := s.db.WithContext(ctx).
		Table("stock_allotment_tiers").
		Select("DISTINCT lots").
		Where("stock_id = ? AND lots > 0", stockID).
		Order("lots ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	buckets := make([]ipo_predict.Tier, 0, len(rows))
	for _, r := range rows {
		buckets = append(buckets, ipo_predict.Tier{
			Lots:      r.Lots,
			AmountHKD: float64(r.Lots) * price,
		})
	}
	return buckets, nil
}

func downloadToTempFile(fileURL string) (string, error) {
	resp, err := http.Get(fileURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http status=%d", resp.StatusCode)
	}

	f, err := os.CreateTemp("", "hkipo_prospectus_*.pdf")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func parseAmountFromText(s string) float64 {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(b.String(), 64)
	return v
}

func buildDefaultBuckets(lotSize int, price float64, publicShares int64) []ipo_predict.Tier {
	if lotSize <= 0 || price <= 0 || publicShares <= 0 {
		return nil
	}
	capShares := (publicShares / 2 / int64(lotSize)) * int64(lotSize)
	if capShares < int64(lotSize) {
		capShares = int64(lotSize)
	}
	mults := []int64{
		1, 2, 3, 4, 5,
		10, 20, 30, 40, 50,
		100, 200, 300, 400, 500,
		600, 700, 800, 900, 1000,
		2000, 3000, 4000, 5000, 6000,
		7000, 8000, 9000, 10000,
		20000, 30000,
		40000, 50000, 60000, 70000, 80000, 90000, 100000,
		200000, 300000, 400000, 500000,
	}
	buckets := make([]ipo_predict.Tier, 0, len(mults))
	for _, m := range mults {
		shares := int64(lotSize) * m
		if shares <= 0 || shares > capShares {
			continue
		}
		buckets = append(buckets, ipo_predict.Tier{
			Lots:      shares,
			AmountHKD: float64(shares) * price,
		})
	}
	if len(buckets) == 0 {
		buckets = append(buckets, ipo_predict.Tier{
			Lots:      int64(lotSize),
			AmountHKD: float64(lotSize) * price,
		})
		return buckets
	}
	last := buckets[len(buckets)-1].Lots
	if capShares > last {
		buckets = append(buckets, ipo_predict.Tier{
			Lots:      capShares,
			AmountHKD: float64(capShares) * price,
		})
	}
	return buckets
}

func (s *server) renderPredict(w http.ResponseWriter, page predictPage) {
	data := struct {
		Title string
		P     predictPage
	}{
		Title: fmt.Sprintf("预测 - %s %s", page.Stock.StockCode, page.Stock.StockName),
		P:     page,
	}
	var buf bytes.Buffer
	if err := s.tpl.ExecuteTemplate(&buf, "predict.html", data); err != nil {
		http.Error(w, "render predict: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// ---- API ----

// apiStocksRequest 列表接口 POST 请求体（JSON）
type apiStocksRequest struct {
	Q            string `json:"q"`
	Role         string `json:"role"`
	Intermediary string `json:"intermediary"`
	Page         int    `json:"page"`
	PageSize     int    `json:"pageSize"`
	OrderBy      string `json:"orderBy"`
	Order        string `json:"order"`
}

// apiStocksResponse 列表接口 JSON 响应
type apiStocksResponse struct {
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Rows     []apiStocksRow `json:"rows"`
}

type apiStocksRow struct {
	StockCode            string   `json:"stock_code"`
	StockName            string   `json:"stock_name"`
	HkSymbol             string   `json:"hk_symbol"`
	ListingMechanism     *string  `json:"listing_mechanism"`
	ListDate             *string  `json:"list_date"`
	ApplyStartDate       *string  `json:"apply_start_date"`
	ApplyEndDate         *string  `json:"apply_end_date"`
	OfferPriceLow        *float64 `json:"offer_price_low"`
	OfferPriceHigh       *float64 `json:"offer_price_high"`
	OfferPrice           *float64 `json:"offer_price"`
	LotSize              *int64   `json:"lot_size"`
	PublicOfferShares    *int64   `json:"public_offer_shares"`
	AdmissionFeeHkd      *float64 `json:"admission_fee_hkd"`
	MarketCapHkd         *float64 `json:"market_cap_hkd"`
	Pe                   *float64 `json:"pe"`
	ProspectusUrl        *string  `json:"prospectus_url"`
	AllocationMechanism  *string  `json:"allocation_mechanism"`
	AllocationConfidence *float64 `json:"allocation_mechanism_confidence"`
	AllocationSource     *string  `json:"allocation_mechanism_source"`
	AllocationEvidence   *string  `json:"allocation_mechanism_evidence"`
	RaiseMoneyHkd        *float64 `json:"amount_hkd"`
	RaiseMoneyText       *string  `json:"amount_text"`
	Applicants           *int64   `json:"applicants"`
	OneLotWinRatePct     *float64 `json:"one_lot_win_rate_pct"`
	SubscriptionMultiple *float64 `json:"subscription_multiple"`
	MaxLots              *int64   `json:"max_lots"`
	FirstDayIncrRatePct  *float64 `json:"first_day_incr_rate_pct"`
	TotalIncrRatePct     *float64 `json:"total_incr_rate_pct"`
	GreyDate             *string  `json:"grey_date"`
	GreyIncrRatePct      *float64 `json:"grey_incr_rate_pct"`
	GreyIncrRatePct2     *float64 `json:"grey_incr_rate_pct2"`
	UpdatedAt            string   `json:"updated_at"`
}

func stockListRowToAPI(r stockListRow) apiStocksRow {
	out := apiStocksRow{
		StockCode: r.StockCode,
		StockName: r.StockName,
		HkSymbol:  r.HkSymbol,
		UpdatedAt: r.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
	if r.ListDate.Valid {
		s := r.ListDate.Time.Format("2006-01-02")
		out.ListDate = &s
	}
	if r.ListingMechanism.Valid {
		s := strings.TrimSpace(r.ListingMechanism.String)
		if s != "" {
			out.ListingMechanism = &s
		}
	}
	if r.ApplyStartDate.Valid {
		s := r.ApplyStartDate.Time.Format("2006-01-02")
		out.ApplyStartDate = &s
	}
	if r.ApplyEndDate.Valid {
		s := r.ApplyEndDate.Time.Format("2006-01-02")
		out.ApplyEndDate = &s
	}
	if r.OfferPriceLow.Valid {
		out.OfferPriceLow = &r.OfferPriceLow.Float64
	}
	if r.OfferPriceHigh.Valid {
		out.OfferPriceHigh = &r.OfferPriceHigh.Float64
	}
	if r.OfferPrice.Valid {
		out.OfferPrice = &r.OfferPrice.Float64
	}
	if r.LotSize.Valid {
		out.LotSize = &r.LotSize.Int64
	}
	if r.AdmissionFeeHkd.Valid {
		out.AdmissionFeeHkd = &r.AdmissionFeeHkd.Float64
	}
	if r.MarketCapHkd.Valid {
		out.MarketCapHkd = &r.MarketCapHkd.Float64
	}
	if r.Pe.Valid {
		out.Pe = &r.Pe.Float64
	}
	if r.ProspectusUrl.Valid {
		out.ProspectusUrl = &r.ProspectusUrl.String
	}
	if r.AllocationMechanism.Valid {
		s := strings.TrimSpace(r.AllocationMechanism.String)
		if s != "" {
			out.AllocationMechanism = &s
		}
	}
	if r.AllocationConfidence.Valid {
		out.AllocationConfidence = &r.AllocationConfidence.Float64
	}
	if r.AllocationSource.Valid {
		s := strings.TrimSpace(r.AllocationSource.String)
		if s != "" {
			out.AllocationSource = &s
		}
	}
	if r.AllocationEvidence.Valid {
		s := strings.TrimSpace(r.AllocationEvidence.String)
		if s != "" {
			out.AllocationEvidence = &s
		}
	}
	if r.RaiseMoneyHkd.Valid {
		out.RaiseMoneyHkd = &r.RaiseMoneyHkd.Float64
	}
	if r.RaiseMoneyText.Valid {
		out.RaiseMoneyText = &r.RaiseMoneyText.String
	}
	if r.Applicants.Valid {
		out.Applicants = &r.Applicants.Int64
	}
	if r.OneLotWinRatePct.Valid {
		out.OneLotWinRatePct = &r.OneLotWinRatePct.Float64
	}
	if r.SubscriptionMultiple.Valid {
		out.SubscriptionMultiple = &r.SubscriptionMultiple.Float64
	}
	if r.MaxLots.Valid {
		out.MaxLots = &r.MaxLots.Int64
	}
	if r.FirstDayIncrRatePct.Valid {
		out.FirstDayIncrRatePct = &r.FirstDayIncrRatePct.Float64
	}
	if r.TotalIncrRatePct.Valid {
		out.TotalIncrRatePct = &r.TotalIncrRatePct.Float64
	}
	if r.PublicOfferShares.Valid {
		out.PublicOfferShares = &r.PublicOfferShares.Int64
	}
	if r.GreyDate.Valid {
		s := strings.TrimSpace(r.GreyDate.String)
		if s != "" {
			out.GreyDate = &s
		}
	}
	if r.GreyIncrRate.Valid {
		out.GreyIncrRatePct = &r.GreyIncrRate.Float64
	}
	if r.GreyIncrRate2.Valid {
		out.GreyIncrRatePct2 = &r.GreyIncrRate2.Float64
	}
	return out
}

func (s *server) handleAPIStocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	var req apiStocksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	queryQ := strings.TrimSpace(req.Q)
	queryRole := strings.TrimSpace(req.Role)
	queryIntermediary := strings.TrimSpace(req.Intermediary)
	queryOrderBy := strings.TrimSpace(req.OrderBy)
	queryOrder := strings.TrimSpace(req.Order)
	if queryOrder != "asc" && queryOrder != "desc" {
		queryOrder = "asc"
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	rows, total, err := listStocks(ctx, s.db, listFilter{
		q:            queryQ,
		role:         queryRole,
		intermediary: queryIntermediary,
		orderBy:      queryOrderBy,
		order:        queryOrder,
	}, pageSize, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	apiRows := make([]apiStocksRow, 0, len(rows))
	for _, row := range rows {
		apiRows = append(apiRows, stockListRowToAPI(row))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiStocksResponse{Total: total, Page: page, PageSize: pageSize, Rows: apiRows})
}

// apiIntermediariesResponse 中介独立接口：所有角色及其公司列表，打开 H5 请求一次即可
type apiIntermediariesResponse struct {
	Sponsor           []string `json:"sponsor"`
	Underwriter       []string `json:"underwriter"`
	Bookrunner        []string `json:"bookrunner"`
	GlobalCoordinator []string `json:"global_coordinator"`
}

func (s *server) handleAPIIntermediaries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	resp, err := listAllIntermediaries(ctx, s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// listAllIntermediaries 一次查出四类角色下所有机构名称，供 H5 首屏请求一次后缓存
func listAllIntermediaries(ctx context.Context, db *gorm.DB) (apiIntermediariesResponse, error) {
	type row struct {
		Role string `gorm:"column:role"`
		Name string `gorm:"column:name"`
	}
	var rows []row
	err := db.WithContext(ctx).
		Table("stock_intermediaries si").
		Select("si.role AS role, i.name AS name").
		Joins("INNER JOIN intermediaries i ON i.id = si.intermediary_id").
		Order("si.role ASC, i.name ASC").
		Scan(&rows).Error
	if err != nil {
		return apiIntermediariesResponse{}, err
	}
	resp := apiIntermediariesResponse{
		Sponsor:           []string{},
		Underwriter:       []string{},
		Bookrunner:        []string{},
		GlobalCoordinator: []string{},
	}
	seen := make(map[string]map[string]bool)
	for _, r := range rows {
		if seen[r.Role] == nil {
			seen[r.Role] = make(map[string]bool)
		}
		if seen[r.Role][r.Name] {
			continue
		}
		seen[r.Role][r.Name] = true
		switch r.Role {
		case RoleSponsor:
			resp.Sponsor = append(resp.Sponsor, r.Name)
		case RoleUnderwriter:
			resp.Underwriter = append(resp.Underwriter, r.Name)
		case RoleBookrunner:
			resp.Bookrunner = append(resp.Bookrunner, r.Name)
		case RoleGlobalCoordinator:
			resp.GlobalCoordinator = append(resp.GlobalCoordinator, r.Name)
		}
	}
	return resp, nil
}

// ---- queries ----

type stockListRow struct {
	StockCode        string         `gorm:"column:stock_code"`
	StockName        string         `gorm:"column:stock_name"`
	HkSymbol         string         `gorm:"column:hk_symbol"`
	ListingMechanism sql.NullString `gorm:"column:listing_mechanism"`

	ListDate       sql.NullTime `gorm:"column:list_date"` // 仅用于默认排序，不展示
	ApplyStartDate sql.NullTime `gorm:"column:apply_start_date"`
	ApplyEndDate   sql.NullTime `gorm:"column:apply_end_date"`

	OfferPriceLow        sql.NullFloat64 `gorm:"column:offer_price_low"`
	OfferPriceHigh       sql.NullFloat64 `gorm:"column:offer_price_high"`
	OfferPrice           sql.NullFloat64 `gorm:"column:offer_price"`
	LotSize              sql.NullInt64   `gorm:"column:lot_size"`
	PublicOfferShares    sql.NullInt64   `gorm:"column:public_offer_shares"`
	AdmissionFeeHkd      sql.NullFloat64 `gorm:"column:admission_fee_hkd"`
	MarketCapHkd         sql.NullFloat64 `gorm:"column:market_cap_hkd"`
	Pe                   sql.NullFloat64 `gorm:"column:pe"`
	ProspectusUrl        sql.NullString  `gorm:"column:prospectus_url"`
	AllocationMechanism  sql.NullString  `gorm:"column:allocation_mechanism"`
	AllocationConfidence sql.NullFloat64 `gorm:"column:allocation_mechanism_confidence"`
	AllocationSource     sql.NullString  `gorm:"column:allocation_mechanism_source"`
	AllocationEvidence   sql.NullString  `gorm:"column:allocation_mechanism_evidence"`

	RaiseMoneyHkd  sql.NullFloat64 `gorm:"column:amount_hkd"`
	RaiseMoneyText sql.NullString  `gorm:"column:amount_text"`

	Applicants           sql.NullInt64   `gorm:"column:applicants"`
	OneLotWinRatePct     sql.NullFloat64 `gorm:"column:one_lot_win_rate_pct"`
	SubscriptionMultiple sql.NullFloat64 `gorm:"column:subscription_multiple"`
	MaxLots              sql.NullInt64   `gorm:"column:max_lots"`

	FirstDayIncrRatePct sql.NullFloat64 `gorm:"column:first_day_incr_rate_pct"`
	TotalIncrRatePct    sql.NullFloat64 `gorm:"column:total_incr_rate_pct"`

	GreyDate      sql.NullString  `gorm:"column:grey_date"`
	GreyIncrRate  sql.NullFloat64 `gorm:"column:incr_rate_pct"`
	GreyIncrRate2 sql.NullFloat64 `gorm:"column:incr_rate_pct2"`

	UpdatedAt time.Time `gorm:"column:updated_at"`
}

type listFilter struct {
	q            string // 代码/名称 模糊
	role         string // sponsor（保薦人）/ underwriter（包銷商）/ bookrunner（賬簿管理人）/ global_coordinator（全球協調人）
	intermediary string // 机构名称，与 role 组合反查（二级选择时精确匹配）
	orderBy      string // 排序字段，如 one_lot_win_rate_pct
	order        string // asc / desc
}

func listStocks(ctx context.Context, db *gorm.DB, f listFilter, limit, offset int) ([]stockListRow, int64, error) {
	base := db.WithContext(ctx).Table("stocks s")

	if f.q != "" {
		like := "%" + f.q + "%"
		base = base.Where("s.stock_code LIKE ? OR s.stock_name LIKE ?", like, like)
	}
	if f.intermediary != "" {
		if f.role != "" {
			// 二级选择：精确匹配机构名称 + 角色
			base = base.Where("s.id IN (SELECT si.stock_id FROM stock_intermediaries si INNER JOIN intermediaries i ON i.id = si.intermediary_id WHERE i.name = ? AND si.role = ?)",
				f.intermediary, f.role)
		} else {
			subLike := "%" + f.intermediary + "%"
			base = base.Where("s.id IN (SELECT si.stock_id FROM stock_intermediaries si INNER JOIN intermediaries i ON i.id = si.intermediary_id WHERE i.name LIKE ?)",
				subLike)
		}
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count stocks: %w", err)
	}

	var rows []stockListRow
	query := base.
		Select(strings.Join([]string{
			"s.stock_code",
			"s.stock_name",
			"s.hk_symbol",
			"rl.listing_mechanism",
			"o.list_date",
			"o.apply_start_date",
			"o.apply_end_date",
			"o.offer_price_low",
			"o.offer_price_high",
			"o.offer_price",
			"o.lot_size",
			"o.public_offer_shares",
			"o.admission_fee_hkd",
			"o.market_cap_hkd",
			"o.pe",
			"o.prospectus_url",
			"o.allocation_mechanism",
			"o.allocation_mechanism_confidence",
			"o.allocation_mechanism_source",
			"o.allocation_mechanism_evidence",
			"r.amount_hkd",
			"r.amount_text",
			"a.applicants",
			"a.one_lot_win_rate_pct",
			"a.subscription_multiple",
			"a.max_lots",
			"p.first_day_incr_rate_pct",
			"p.total_incr_rate_pct",
			"COALESCE(NULLIF(TRIM(rl.grey_date_text), ''), CASE WHEN g.grey_date IS NOT NULL THEN strftime('%Y-%m-%d', g.grey_date) END, CASE WHEN o.list_date IS NOT NULL THEN strftime('%Y-%m-%d', date(o.list_date, '-1 day')) END) AS grey_date",
			"g.incr_rate_pct",
			"g.incr_rate_pct2",
			"s.updated_at",
		}, ",")).
		Joins(`LEFT JOIN (
			SELECT
				rs.stock_id AS stock_id,
				MAX(CASE WHEN LOWER(ri.label) IN ('market') OR ri.label IN ('上市机制','上市機制') THEN ri.value END) AS listing_mechanism,
				MAX(CASE WHEN LOWER(ri.label) IN ('gray_dt','grey_dt','gray_date','grey_date') OR ri.label IN ('暗盘日期','暗盤日期','暗盘','暗盤','灰度日期','灰度') THEN ri.value END) AS grey_date_text
			FROM stock_raw_sections rs
			INNER JOIN stock_raw_items ri ON ri.raw_section_id = rs.id
			WHERE rs.section_id = 'list'
			GROUP BY rs.stock_id
		) rl ON rl.stock_id = s.id`).
		Joins("LEFT JOIN stock_offerings o ON o.stock_id = s.id").
		Joins("LEFT JOIN stock_fundraising r ON r.stock_id = s.id").
		Joins("LEFT JOIN stock_allotment_summary a ON a.stock_id = s.id").
		Joins("LEFT JOIN stock_performance p ON p.stock_id = s.id").
		Joins("LEFT JOIN stock_grey_market g ON g.stock_id = s.id")
	switch f.orderBy {
	case "one_lot_win_rate_pct":
		if f.order == "desc" {
			query = query.Order("a.one_lot_win_rate_pct DESC, s.stock_code ASC")
		} else {
			query = query.Order("a.one_lot_win_rate_pct ASC, s.stock_code ASC")
		}
	default:
		query = query.Order("COALESCE(o.list_date, '1970-01-01') DESC, s.stock_code DESC")
	}
	if err := query.Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("select stock list: %w", err)
	}
	return rows, total, nil
}

// listIntermediaryNamesByRole 返回指定角色下所有机构名称（二级分类选项），按名称排序。
func listIntermediaryNamesByRole(ctx context.Context, db *gorm.DB, role string) ([]string, error) {
	if role == "" {
		return nil, nil
	}
	var names []string
	err := db.WithContext(ctx).
		Table("intermediaries i").
		Select("DISTINCT i.name").
		Joins("INNER JOIN stock_intermediaries si ON si.intermediary_id = i.id").
		Where("si.role = ?", role).
		Order("i.name ASC").
		Pluck("i.name", &names).Error
	if err != nil {
		return nil, err
	}
	return names, nil
}

type stockIntermediaryView struct {
	Role string
	Seq  int
	Name string
}

type stockDetailPage struct {
	Stock            gormmodel.Stock
	Offering         *gormmodel.StockOffering
	Company          *gormmodel.StockCompany
	GreenShoe        *gormmodel.StockGreenShoe
	GreyMarket       *gormmodel.StockGreyMarket
	Performance      *gormmodel.StockPerformance
	RaiseMoney       *gormmodel.StockRaiseMoney
	AllotmentSummary *gormmodel.StockAllotmentSummary
	LiveMargin       *vBrokerMarginData
	GroupACount      int
	GroupBCount      int

	CompanySecretaries []gormmodel.StockCompanySecretary
	MajorShareholders  []gormmodel.StockMajorShareholder
	UseOfProceeds      []gormmodel.StockUseOfProceeds
	Management         []gormmodel.StockManagement
	AllotmentTiers     []allotmentTierView

	Intermediaries []stockIntermediaryView
	RawSections    []rawSectionView
}

type allotmentTierView struct {
	Seq                 int
	GroupCode           string
	Lots                int64
	AmountHKD           float64
	Applicants          int
	WinLots             int64
	WinRatePct          float64
	HouseholdWinRatePct float64
	Remark              string
}

const (
	ipoBrokerageRate             = 0.0100000
	ipoSFCTransactionLevyRate    = 0.0000270
	ipoAFRCTransactionLevyRate   = 0.0000015
	ipoExchangeTradingFeeRate    = 0.0000565
	ipoSubscriptionFeeRate       = ipoBrokerageRate + ipoSFCTransactionLevyRate + ipoAFRCTransactionLevyRate + ipoExchangeTradingFeeRate
	ipoSubscriptionFeeMultiplier = 1 + ipoSubscriptionFeeRate
)

type rawItemView struct {
	Seq   int
	Label string
	Value string
}

type rawSectionView struct {
	SectionID string
	Items     []rawItemView
}

func getStockDetailByCode(ctx context.Context, db *gorm.DB, code string) (stockDetailPage, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return stockDetailPage{}, gorm.ErrRecordNotFound
	}

	tx := db.WithContext(ctx)

	var stock gormmodel.Stock
	if err := tx.Where("stock_code = ?", code).First(&stock).Error; err != nil {
		return stockDetailPage{}, err
	}

	out := stockDetailPage{Stock: stock}

	var err error
	out.Offering, err = takeOne[gormmodel.StockOffering](tx, stock.ID)
	if err != nil {
		return stockDetailPage{}, fmt.Errorf("select offering: %w", err)
	}
	out.Company, err = takeOne[gormmodel.StockCompany](tx, stock.ID)
	if err != nil {
		return stockDetailPage{}, fmt.Errorf("select company: %w", err)
	}
	out.GreenShoe, err = takeOne[gormmodel.StockGreenShoe](tx, stock.ID)
	if err != nil {
		return stockDetailPage{}, fmt.Errorf("select green shoe: %w", err)
	}
	out.GreyMarket, err = takeOne[gormmodel.StockGreyMarket](tx, stock.ID)
	if err != nil {
		return stockDetailPage{}, fmt.Errorf("select grey market: %w", err)
	}
	out.Performance, err = takeOne[gormmodel.StockPerformance](tx, stock.ID)
	if err != nil {
		return stockDetailPage{}, fmt.Errorf("select performance: %w", err)
	}
	out.RaiseMoney, err = takeOne[gormmodel.StockRaiseMoney](tx, stock.ID)
	if err != nil {
		return stockDetailPage{}, fmt.Errorf("select raise money: %w", err)
	}
	out.AllotmentSummary, err = takeOne[gormmodel.StockAllotmentSummary](tx, stock.ID)
	if err != nil {
		return stockDetailPage{}, fmt.Errorf("select allotment summary: %w", err)
	}
	if out.AllotmentSummary == nil || out.AllotmentSummary.SubscriptionMultiple <= 0 {
		if liveMargin, err := newVBrokerMarginClient(nil).fetchByStockCode(ctx, stock.StockCode); err == nil && liveMargin != nil {
			out.LiveMargin = liveMargin
		}
	}

	if err := tx.Where("stock_id = ?", stock.ID).Order("seq ASC").Find(&out.CompanySecretaries).Error; err != nil {
		return stockDetailPage{}, fmt.Errorf("select company secretaries: %w", err)
	}
	if err := tx.Where("stock_id = ?", stock.ID).Order("seq ASC").Find(&out.MajorShareholders).Error; err != nil {
		return stockDetailPage{}, fmt.Errorf("select major shareholders: %w", err)
	}
	if err := tx.Where("stock_id = ?", stock.ID).Order("seq ASC").Find(&out.UseOfProceeds).Error; err != nil {
		return stockDetailPage{}, fmt.Errorf("select use of proceeds: %w", err)
	}
	if err := tx.Where("stock_id = ?", stock.ID).Order("seq ASC").Find(&out.Management).Error; err != nil {
		return stockDetailPage{}, fmt.Errorf("select management: %w", err)
	}
	var tiers []gormmodel.StockAllotmentTier
	if err := tx.Where("stock_id = ?", stock.ID).Order("seq ASC").Find(&tiers).Error; err != nil {
		return stockDetailPage{}, fmt.Errorf("select allotment tiers: %w", err)
	}
	allotmentTierPrice := detailAllotmentTierPrice(out.AllotmentSummary, out.Offering)
	lotSize := 0
	if out.Offering != nil {
		lotSize = out.Offering.LotSize
	}
	out.AllotmentTiers = make([]allotmentTierView, 0, len(tiers))
	for _, t := range tiers {
		out.AllotmentTiers = append(out.AllotmentTiers, allotmentTierView{
			Seq:                 t.Seq,
			GroupCode:           t.GroupCode,
			Lots:                t.Lots,
			AmountHKD:           calcAllotmentTierAmountHKD(t.Lots, allotmentTierPrice),
			Applicants:          t.Applicants,
			WinLots:             t.WinLots,
			WinRatePct:          t.WinRatePct,
			HouseholdWinRatePct: calcTierHouseholdWinRatePct(t.Applicants, t.WinLots, t.Remark, t.Lots, lotSize, t.WinRatePct),
			Remark:              t.Remark,
		})
		switch strings.ToUpper(strings.TrimSpace(t.GroupCode)) {
		case "A":
			out.GroupACount += t.Applicants
		case "B":
			out.GroupBCount += t.Applicants
		}
	}

	if err := tx.Table("stock_intermediaries si").
		Select("si.role AS role, si.seq AS seq, i.name AS name").
		Joins("JOIN intermediaries i ON i.id = si.intermediary_id").
		Where("si.stock_id = ?", stock.ID).
		Order("si.role ASC, si.seq ASC").
		Scan(&out.Intermediaries).Error; err != nil {
		return stockDetailPage{}, fmt.Errorf("select intermediaries: %w", err)
	}

	// Raw sections + items
	var sections []gormmodel.StockRawSection
	if err := tx.Where("stock_id = ?", stock.ID).Order("section_id ASC").Find(&sections).Error; err != nil {
		return stockDetailPage{}, fmt.Errorf("select raw sections: %w", err)
	}
	if len(sections) > 0 {
		out.RawSections = make([]rawSectionView, 0, len(sections))
		for _, sec := range sections {
			var items []gormmodel.StockRawItem
			if err := tx.Where("raw_section_id = ?", sec.ID).Order("seq ASC").Find(&items).Error; err != nil {
				return stockDetailPage{}, fmt.Errorf("select raw items: %w", err)
			}
			v := rawSectionView{SectionID: sec.SectionID}
			for _, it := range items {
				v.Items = append(v.Items, rawItemView{Seq: it.Seq, Label: it.Label, Value: it.Value})
			}
			out.RawSections = append(out.RawSections, v)
		}
	}

	return out, nil
}

func detailAllotmentTierPrice(summary *gormmodel.StockAllotmentSummary, offering *gormmodel.StockOffering) float64 {
	if summary != nil {
		if summary.OfferPrice > 0 {
			return summary.OfferPrice
		}
		if summary.OfferPriceHigh > 0 {
			return summary.OfferPriceHigh
		}
	}
	if offering != nil {
		if offering.OfferPrice > 0 {
			return offering.OfferPrice
		}
		if offering.OfferPriceHigh > 0 {
			return offering.OfferPriceHigh
		}
	}
	return 0
}

func calcAllotmentTierAmountHKD(lots int64, price float64) float64 {
	if lots <= 0 || price <= 0 {
		return 0
	}
	return float64(lots) * price * ipoSubscriptionFeeMultiplier
}

func formatHKDMoney(v float64) string {
	abs := math.Abs(v)
	switch {
	case abs >= 1e8:
		return strconv.FormatFloat(v/1e8, 'f', 2, 64) + " 亿"
	case abs >= 1e4:
		return strconv.FormatFloat(v/1e4, 'f', 2, 64) + " 万"
	default:
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
}

func takeOne[T any](tx *gorm.DB, stockID uint64) (*T, error) {
	var out T
	if err := tx.Where("stock_id = ?", stockID).Take(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// ---- helpers ----

func fmtTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

func fmtNullTime(t sql.NullTime) string {
	if !t.Valid {
		return "-"
	}
	return t.Time.Format("2006-01-02")
}

// roleLabel 返回「英文（中文）」形式，用于列表/详情展示。
// bookrunner=賬簿管理人, global_coordinator=全球協調, sponsor=保薦人, underwriter=包銷商
func roleLabel(role string) string {
	switch role {
	case RoleSponsor:
		return "sponsor（保薦人）"
	case RoleUnderwriter:
		return "underwriter（包銷商）"
	case RoleBookrunner:
		return "bookrunner（賬簿管理人）"
	case RoleGlobalCoordinator:
		return "global_coordinator（全球協調人）"
	default:
		return role
	}
}

func parsePositiveInt(q url.Values, key string, defaultVal int) int {
	v := strings.TrimSpace(q.Get(key))
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultVal
	}
	return n
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func startAutoSync() {
	onStart := parseBoolEnv("HK_IPO_AUTO_SYNC_ON_START", false)
	interval := parseDurationEnv("HK_IPO_AUTO_SYNC_INTERVAL", 0)
	symbol := strings.TrimSpace(os.Getenv("HK_IPO_AUTO_SYNC_SYMBOL"))
	if !onStart && interval <= 0 {
		return
	}

	go func() {
		lastRun := time.Now()
		if onStart {
			runAutoSyncOnce(symbol)
			lastRun = time.Now()
		}
		if interval <= 0 {
			return
		}
		pollInterval := autoSyncPollInterval(interval)
		log.Printf("auto sync scheduled: interval=%s poll=%s", interval, pollInterval)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for range ticker.C {
			if !autoSyncDue(lastRun, time.Now(), interval) {
				continue
			}
			runAutoSyncOnce(symbol)
			lastRun = time.Now()
		}
	}()
}

func autoSyncPollInterval(interval time.Duration) time.Duration {
	if interval <= time.Minute {
		return interval
	}
	return time.Minute
}

func autoSyncDue(lastRun, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return false
	}
	return wallClockElapsed(lastRun, now) >= interval
}

func wallClockElapsed(start, end time.Time) time.Duration {
	return time.Duration(end.UnixNano() - start.UnixNano())
}

func runAutoSyncOnce(symbol string) {
	timeout := parseDurationEnv("HK_IPO_AUTO_SYNC_TIMEOUT", 30*time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if symbol == "" {
		log.Printf("auto sync started: full")
	} else {
		log.Printf("auto sync started: symbol=%s", symbol)
	}
	if err := collectorapp.SyncToDB(ctx, symbol); err != nil {
		log.Printf("auto sync failed: %v", err)
		return
	}
	log.Printf("auto sync finished")
}

func parseBoolEnv(key string, defaultVal bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return defaultVal
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return defaultVal
	}
}

func parseDurationEnv(key string, defaultVal time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if hours, err := strconv.ParseFloat(v, 64); err == nil && hours > 0 {
		return time.Duration(hours * float64(time.Hour))
	}
	return defaultVal
}

func formatPctTrunc(v float64, digits int) string {
	if digits < 0 {
		digits = 0
	}
	factor := math.Pow10(digits)
	truncated := math.Trunc(v*factor) / factor
	return strconv.FormatFloat(truncated, 'f', digits, 64) + "%"
}

func calcMinWinLots(applicants, winApplicants int, allocatedLots int64) int64 {
	if applicants <= 0 || winApplicants <= 0 || allocatedLots <= 0 {
		return 0
	}
	if winApplicants < applicants {
		return 1
	}
	minLots := allocatedLots / int64(winApplicants)
	if minLots < 1 {
		return 1
	}
	return minLots
}

var reHouseholdWinRemark = regexp.MustCompile(`(\d+)\s*(?:名(?:申請人|申请人|申請者|申请者)?|份)\s*(?:中|中的|中有)\s*(\d+)\s*(?:名|份)?`)
var reHouseholdWinAmongRemark = regexp.MustCompile(`其中\s*(\d+)\s*名`)

func calcHouseholdWinRatePct(applicants int, winLots int64, remark string) float64 {
	if applicants <= 0 {
		return 0
	}
	remark = strings.TrimSpace(remark)
	if strings.Contains(remark, "獲發額外") || strings.Contains(remark, "获发额外") ||
		strings.Contains(remark, "獲得額外") || strings.Contains(remark, "获得额外") ||
		strings.Contains(remark, "取得額外") || strings.Contains(remark, "取得额外") ||
		strings.Contains(remark, "加上") || strings.Contains(remark, "另加") {
		return 100
	}
	if m := reHouseholdWinRemark.FindStringSubmatch(remark); len(m) == 3 {
		total, err1 := strconv.Atoi(m[1])
		hit, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil && total > 0 && hit >= 0 {
			rate := float64(hit) * 100 / float64(total)
			if rate > 100 {
				return 100
			}
			return rate
		}
	}
	if m := reHouseholdWinAmongRemark.FindStringSubmatch(remark); len(m) == 2 {
		hit, err := strconv.Atoi(m[1])
		if err == nil && hit >= 0 {
			rate := float64(hit) * 100 / float64(applicants)
			if rate > 100 {
				return 100
			}
			return rate
		}
	}
	if winLots >= 2 {
		return 100
	}
	return 0
}

func calcTierHouseholdWinRatePct(applicants int, winLots int64, remark string, requestedShares int64, lotSize int, winRatePct float64) float64 {
	rate := calcHouseholdWinRatePct(applicants, winLots, remark)
	if rate > 0 || strings.TrimSpace(remark) != "" {
		return rate
	}
	if applicants <= 0 || winLots <= 0 || requestedShares <= 0 || lotSize <= 0 || winRatePct <= 0 {
		return rate
	}

	guaranteedWinRatePct := float64(winLots*int64(lotSize)) * 100 / float64(requestedShares)
	if math.Abs(guaranteedWinRatePct-winRatePct) < 0.01 {
		return 100
	}
	return rate
}

// buildSortURL 生成点击排序列的 URL：若当前已按该列排序则翻转 order，否则 order=asc；保留其他查询参数。
func buildSortURL(u *url.URL, column, currentOrderBy, currentOrder string) string {
	q := u.Query()
	q.Set("orderBy", column)
	q.Set("page", "1")
	if currentOrderBy == column {
		if currentOrder == "asc" {
			q.Set("order", "desc")
		} else {
			q.Set("order", "asc")
		}
	} else {
		q.Set("order", "asc")
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	return path + "?" + q.Encode()
}

func pageURL(u *url.URL, page int) string {
	if page <= 0 {
		return ""
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	nu := &url.URL{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Path:     u.Path,
		RawQuery: q.Encode(),
		Fragment: u.Fragment,
	}
	return nu.String()
}

func formatIntWithComma(s string) string {
	if s == "" {
		return s
	}
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign = "-"
		s = strings.TrimPrefix(s, "-")
	}
	if len(s) <= 3 {
		return sign + s
	}
	n := len(s)
	head := n % 3
	if head == 0 {
		head = 3
	}
	var b strings.Builder
	b.Grow(n + n/3 + 1)
	b.WriteString(sign)
	b.WriteString(s[:head])
	for i := head; i < n; i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
