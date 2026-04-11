package backtestutil

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"hk_ipo/pkg/ipo_predict"
	"hk_ipo/pkg/pdfreader"
)

const (
	ReasonOK                    = "ok"
	ReasonMissingCoreFields     = "missing_core_fields"
	ReasonMissingProspectusURL  = "missing_prospectus_url"
	ReasonMissingAnnouncement   = "missing_announcement_tiers"
	ReasonMissingOneLotTier     = "missing_one_lot_tier"
	ReasonInvalidAnnouncement   = "invalid_announcement_tiers"
	ReasonProspectusDownload    = "prospectus_download_failed"
	ReasonProspectusExtract     = "prospectus_extract_failed"
	ReasonTierCountMismatch     = "tier_count_mismatch"
	ReasonTierLotsMismatch      = "tier_lots_mismatch"
)

type Candidate struct {
	ID                   int64
	Code                 string
	Name                 string
	ListDate             time.Time
	PublicShares         int64
	LotSize              int
	Price                float64
	AdmissionFeeHKD      float64
	ProspectusURL        string
	SubscriptionMultiple float64
	OneLotWinRatePct     float64
}

type TierRow struct {
	Seq       int
	GroupCode string
	Lots      int64
	ActualPct float64
}

type ValidationResult struct {
	Reason            string
	ProspectusBuckets []ipo_predict.Tier
	ProspectusLots    []int64
	AnnouncementLots  []int64
	CountMatch        bool
	ExactMatch        bool
}

type rawCandidateRow struct {
	ID                   int64
	Code                 string
	Name                 string
	ListDate             sql.NullTime
	PublicShares         int64
	LotSize              int
	Price                float64
	AdmissionFeeHKD      float64
	ProspectusURL        sql.NullString
	RawProspectusURL     sql.NullString
	SubscriptionMultiple float64
	OneLotWinRatePct     float64
}

func LoadCandidates(db *sql.DB, limit int) ([]Candidate, error) {
	query := `
SELECT
	s.id,
	s.stock_code,
	s.stock_name,
	o.list_date,
	o.public_offer_shares,
	o.lot_size,
	COALESCE(NULLIF(o.offer_price, 0), o.offer_price_high) AS price,
	o.admission_fee_hkd,
	NULLIF(o.prospectus_url, '') AS prospectus_url,
	(
		SELECT NULLIF(MAX(i.value), '')
		FROM stock_raw_sections rs
		JOIN stock_raw_items i ON i.raw_section_id = rs.id
		WHERE rs.stock_id = s.id
		  AND i.label IN ('招股文件', '招股书', '招股書')
	) AS raw_prospectus_url,
	a.subscription_multiple,
	a.one_lot_win_rate_pct
FROM stocks s
JOIN stock_offerings o ON o.stock_id = s.id
JOIN stock_allotment_summary a ON a.stock_id = s.id
WHERE a.subscription_multiple > 0
  AND a.one_lot_win_rate_pct > 0
ORDER BY datetime(o.list_date) DESC, s.stock_code DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rs, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	out := make([]Candidate, 0, 128)
	for rs.Next() {
		var row rawCandidateRow
		if err := rs.Scan(
			&row.ID,
			&row.Code,
			&row.Name,
			&row.ListDate,
			&row.PublicShares,
			&row.LotSize,
			&row.Price,
			&row.AdmissionFeeHKD,
			&row.ProspectusURL,
			&row.RawProspectusURL,
			&row.SubscriptionMultiple,
			&row.OneLotWinRatePct,
		); err != nil {
			return nil, err
		}
		c := Candidate{
			ID:                   row.ID,
			Code:                 row.Code,
			Name:                 row.Name,
			PublicShares:         row.PublicShares,
			LotSize:              row.LotSize,
			Price:                row.Price,
			AdmissionFeeHKD:      row.AdmissionFeeHKD,
			SubscriptionMultiple: row.SubscriptionMultiple,
			OneLotWinRatePct:     row.OneLotWinRatePct,
		}
		if row.ListDate.Valid {
			c.ListDate = row.ListDate.Time
		}
		if row.ProspectusURL.Valid && strings.TrimSpace(row.ProspectusURL.String) != "" {
			c.ProspectusURL = strings.TrimSpace(row.ProspectusURL.String)
		} else if row.RawProspectusURL.Valid {
			c.ProspectusURL = strings.TrimSpace(row.RawProspectusURL.String)
		}
		out = append(out, c)
	}
	return out, rs.Err()
}

func LoadAnnouncementTiers(db *sql.DB, stockID int64) ([]TierRow, error) {
	const query = `
SELECT seq, group_code, lots, win_rate_pct
FROM stock_allotment_tiers
WHERE stock_id = ?
ORDER BY seq`
	rs, err := db.Query(query, stockID)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	out := make([]TierRow, 0, 64)
	for rs.Next() {
		var row TierRow
		if err := rs.Scan(&row.Seq, &row.GroupCode, &row.Lots, &row.ActualPct); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rs.Err()
}

func ValidateCandidate(ctx context.Context, client *http.Client, c Candidate, tiers []TierRow, cacheDir string) ValidationResult {
	if reason := checkCompleteness(c, tiers); reason != ReasonOK {
		return ValidationResult{Reason: reason}
	}

	buckets, err := ExtractProspectusBuckets(ctx, client, c, cacheDir)
	if err != nil {
		reason := ReasonProspectusExtract
		if strings.Contains(err.Error(), "download") || strings.Contains(err.Error(), "http status") {
			reason = ReasonProspectusDownload
		}
		return ValidationResult{Reason: reason}
	}

	prospectusLots := bucketsToLots(buckets)
	announcementLots := announcementLots(tiers)
	countMatch := len(prospectusLots) == len(announcementLots)
	exactMatch := countMatch && equalLots(prospectusLots, announcementLots)
	reason := ReasonOK
	switch {
	case !countMatch:
		reason = ReasonTierCountMismatch
	case !exactMatch:
		reason = ReasonTierLotsMismatch
	}

	return ValidationResult{
		Reason:            reason,
		ProspectusBuckets: buckets,
		ProspectusLots:    prospectusLots,
		AnnouncementLots:  announcementLots,
		CountMatch:        countMatch,
		ExactMatch:        exactMatch,
	}
}

func ExtractProspectusBuckets(ctx context.Context, client *http.Client, c Candidate, cacheDir string) ([]ipo_predict.Tier, error) {
	if strings.TrimSpace(c.ProspectusURL) == "" {
		return nil, fmt.Errorf("missing prospectus url")
	}
	if c.LotSize <= 0 || c.PublicShares <= 0 || c.Price <= 0 {
		return nil, fmt.Errorf("candidate fields incomplete")
	}

	pdfPath, err := ensureProspectusFile(ctx, client, c, cacheDir)
	if err != nil {
		return nil, err
	}

	anchorShares := strconv.Itoa(c.LotSize)
	anchorPrices := make([]string, 0, 2)
	if c.AdmissionFeeHKD > 0 {
		anchorPrices = append(anchorPrices, strconv.FormatFloat(c.AdmissionFeeHKD, 'f', 2, 64))
	}
	lotMoney := float64(c.LotSize) * c.Price
	if lotMoney > 0 {
		anchorPrices = append(anchorPrices, strconv.FormatFloat(lotMoney, 'f', 2, 64))
	}

	var lastErr error
	for _, anchorPrice := range anchorPrices {
		res, err := pdfreader.ExtractTableFromAnchor(pdfPath, anchorShares, anchorPrice)
		if err != nil {
			lastErr = err
			continue
		}
		buckets := make([]ipo_predict.Tier, 0, len(res.Key1))
		seen := make(map[int64]struct{}, len(res.Key1))
		for _, cell := range res.Key1 {
			lots := int64(cell.Shares)
			if lots <= 0 {
				continue
			}
			if _, ok := seen[lots]; ok {
				continue
			}
			seen[lots] = struct{}{}
			buckets = append(buckets, ipo_predict.Tier{
				Lots:      lots,
				AmountHKD: cell.TotalPrice,
			})
		}
		if len(buckets) == 0 {
			continue
		}
		sort.Slice(buckets, func(i, j int) bool { return buckets[i].Lots < buckets[j].Lots })
		return buckets, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("prospectus buckets not found")
}

func InferBrokerMarginSum(fundraisingAmt, realOversub float64) float64 {
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

func BuildDefaultBuckets(lotSize int, price float64, publicShares int64) []ipo_predict.Tier {
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
		return []ipo_predict.Tier{{
			Lots:      int64(lotSize),
			AmountHKD: float64(lotSize) * price,
		}}
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

func FindOneLotPerLotRate(list []ipo_predict.WinRateInfo, oneLotShares int64) float64 {
	best := 0.0
	for _, w := range list {
		if w.Lots == oneLotShares && w.PerLotRate > best {
			best = w.PerLotRate
		}
	}
	return best
}

func checkCompleteness(c Candidate, tiers []TierRow) string {
	if c.PublicShares <= 0 || c.LotSize <= 0 || c.Price <= 0 || c.SubscriptionMultiple <= 0 || c.OneLotWinRatePct <= 0 {
		return ReasonMissingCoreFields
	}
	if strings.TrimSpace(c.ProspectusURL) == "" {
		return ReasonMissingProspectusURL
	}
	if len(tiers) == 0 {
		return ReasonMissingAnnouncement
	}
	foundOneLot := false
	prevLots := int64(0)
	for _, t := range tiers {
		if t.Lots <= 0 {
			return ReasonInvalidAnnouncement
		}
		if prevLots > 0 && t.Lots < prevLots {
			return ReasonInvalidAnnouncement
		}
		if t.Lots == int64(c.LotSize) {
			foundOneLot = true
		}
		prevLots = t.Lots
	}
	if !foundOneLot {
		return ReasonMissingOneLotTier
	}
	return ReasonOK
}

func ensureProspectusFile(ctx context.Context, client *http.Client, c Candidate, cacheDir string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "hkipo_prospectus_cache")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	target := filepath.Join(cacheDir, c.Code+".pdf")
	if fi, err := os.Stat(target); err == nil && fi.Size() > 0 {
		return target, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ProspectusURL, nil)
	if err != nil {
		return "", fmt.Errorf("download request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; hk_ipo-backtest/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download pdf: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http status=%d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(cacheDir, c.Code+"_*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("write pdf: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close pdf: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return "", fmt.Errorf("cache pdf: %w", err)
	}
	return target, nil
}

func bucketsToLots(buckets []ipo_predict.Tier) []int64 {
	out := make([]int64, 0, len(buckets))
	for _, b := range buckets {
		if b.Lots <= 0 {
			continue
		}
		out = append(out, b.Lots)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func announcementLots(tiers []TierRow) []int64 {
	out := make([]int64, 0, len(tiers))
	for _, t := range tiers {
		if t.Lots <= 0 {
			continue
		}
		out = append(out, t.Lots)
	}
	return out
}

func equalLots(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
