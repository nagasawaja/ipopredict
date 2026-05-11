package gormstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hk_ipo/orm"
	"hk_ipo/pkg/collector"
	"hk_ipo/pkg/storage/gormmodel"
)

// UpsertStockDetail writes a normalized StockDetail into SQLite tables,
// using GORM upsert patterns.
func UpsertStockDetail(ctx context.Context, d collector.StockDetail) error {
	if orm.DB == nil {
		return fmt.Errorf("orm.DB is nil (call orm.Init first)")
	}
	if strings.TrimSpace(d.StockCode) == "" {
		return fmt.Errorf("stockCode is empty")
	}

	return orm.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stockID, err := upsertStock(tx, d)
		if err != nil {
			return err
		}

		if err := upsertOffering(tx, stockID, d); err != nil {
			return err
		}
		if err := upsertCompany(tx, stockID, d); err != nil {
			return err
		}
		if err := upsertGreenShoe(tx, stockID, d); err != nil {
			return err
		}
		if err := upsertGreyMarket(tx, stockID, d); err != nil {
			return err
		}
		if err := upsertPerformance(tx, stockID, d); err != nil {
			return err
		}
		if err := upsertRaiseMoney(tx, stockID, d); err != nil {
			return err
		}

		if err := upsertAllotmentSummary(tx, stockID, d); err != nil {
			return err
		}

		// Rebuild relationship tables (simple + deterministic)
		if err := rebuildIntermediaries(tx, stockID, d); err != nil {
			return err
		}
		if err := rebuildCompanySecretaries(tx, stockID, d.Company.CompanySecretary); err != nil {
			return err
		}
		if err := rebuildMajorShareholders(tx, stockID, d.Company.MajorShareholders); err != nil {
			return err
		}
		if err := rebuildUseOfProceeds(tx, stockID, d.UseOfProceeds); err != nil {
			return err
		}
		if err := rebuildManagement(tx, stockID, d.Management); err != nil {
			return err
		}
		if err := rebuildAllotmentTiers(tx, stockID, d.Allotment.Tiers); err != nil {
			return err
		}
		if err := rebuildRawSections(tx, stockID, d.RawSections); err != nil {
			return err
		}

		return nil
	})
}

func upsertStock(tx *gorm.DB, d collector.StockDetail) (uint64, error) {
	row := gormmodel.Stock{
		StockCode: d.StockCode,
		HkSymbol:  d.HkSymbol,
		StockName: d.StockName,

		ReferenceCompany: d.ReferenceCompany,

		SourceProvider:      d.Source.Provider,
		SourceRequestSymbol: d.Source.RequestSymbol,
		SourceUrl:           d.Source.Url,
		SourceFetchedAt:     d.Source.FetchedAt,
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stock_code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"hk_symbol", "stock_name", "reference_company",
			"source_provider", "source_request_symbol", "source_url", "source_fetched_at",
			"updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return 0, fmt.Errorf("upsert stocks: %w", err)
	}

	var out gormmodel.Stock
	if err := tx.Where("stock_code = ?", d.StockCode).First(&out).Error; err != nil {
		return 0, fmt.Errorf("select stocks id: %w", err)
	}
	return out.ID, nil
}

func upsertOffering(tx *gorm.DB, stockID uint64, d collector.StockDetail) error {
	o := d.Offering
	row := gormmodel.StockOffering{
		StockID: stockID,

		OfferPriceLow:  o.OfferPriceLow,
		OfferPriceHigh: o.OfferPriceHigh,
		OfferPrice:     o.OfferPrice,

		LotSize: o.LotSize,

		GlobalOfferShares:        o.GlobalOfferShares,
		PublicOfferShares:        o.PublicOfferShares,
		InternationalOfferShares: o.InternationalOfferShares,

		ApplyStartDate: parseDatePtr(o.ApplyStartDate),
		ApplyEndDate:   parseDatePtr(o.ApplyEndDate),
		ListDate:       parseDatePtr(o.ListDate),

		AdmissionFeeHkd: o.AdmissionFeeHkd,
		MarketCapHkd:    o.MarketCapHkd,
		Pe:              o.Pe,

		ProspectusUrl: o.ProspectusUrl,
	}
	row.AllocationMechanism = strings.TrimSpace(o.AllocationMechanism)
	row.AllocationMechanismConfidence = o.AllocationMechanismConfidence
	row.AllocationMechanismSource = strings.TrimSpace(o.AllocationMechanismSource)
	row.AllocationMechanismEvidence = strings.TrimSpace(o.AllocationMechanismEvidence)
	if row.AllocationMechanism == "" {
		row.AllocationMechanism, row.AllocationMechanismConfidence, row.AllocationMechanismSource, row.AllocationMechanismEvidence = inferAllocationMechanism(d)
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stock_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"offer_price_low", "offer_price_high", "offer_price",
			"lot_size",
			"global_offer_shares", "public_offer_shares", "international_offer_shares",
			"apply_start_date", "apply_end_date", "list_date",
			"admission_fee_hkd", "market_cap_hkd", "pe",
			"prospectus_url",
			"allocation_mechanism", "allocation_mechanism_confidence", "allocation_mechanism_source", "allocation_mechanism_evidence",
			"updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("upsert stock_offerings: %w", err)
	}
	return nil
}

func inferAllocationMechanism(d collector.StockDetail) (mechanism string, confidence float64, source string, evidence string) {
	stockName := strings.TrimSpace(d.StockName)
	stockCode := strings.TrimSpace(d.StockCode)
	lowerEvidence := strings.ToLower(strings.Join(rawSectionValues(d.RawSections), "\n"))
	switch {
	case containsAny(lowerEvidence, "mechanism b", "機制b", "机制b", "no clawback mechanism", "無回撥機制", "无回拨机制"):
		return "mechanism_b", 0.95, "raw_text", "raw text mentions Mechanism B/no clawback"
	case containsAny(lowerEvidence, "mechanism a", "機制a", "机制a"):
		return "mechanism_a", 0.95, "raw_text", "raw text mentions Mechanism A"
	case containsAny(lowerEvidence, "chapter 18c", "第18c章", "特專科技", "特专科技", "specialist technology"):
		return "chapter_18c", 0.95, "raw_text", "raw text mentions Chapter 18C/specialist technology"
	case strings.HasSuffix(strings.ToUpper(stockName), "-P"):
		return "chapter_18c_pre_commercial", 0.90, "stock_name_marker", stockName
	case isKnownChapter18CStock(stockCode):
		return "chapter_18c", 0.90, "known_18c_stock", stockCode
	}

	if d.Offering.GlobalOfferShares > 0 && d.Offering.PublicOfferShares > 0 {
		publicPct := 100 * float64(d.Offering.PublicOfferShares) / float64(d.Offering.GlobalOfferShares)
		switch {
		case publicPct >= 9.5 && publicPct <= 60:
			return "mechanism_b_likely", 0.60, "public_offer_ratio", fmt.Sprintf("public offer %.2f%% of global offer", publicPct)
		case publicPct >= 4.5 && publicPct < 9.5:
			return "mechanism_a_or_18c_likely", 0.45, "public_offer_ratio", fmt.Sprintf("public offer %.2f%% of global offer", publicPct)
		}
	}

	if strings.HasSuffix(strings.ToUpper(stockName), "-B") {
		return "unknown_biotech_marker", 0.30, "stock_name_marker", stockName
	}
	return "unknown", 0, "none", ""
}

func isKnownChapter18CStock(stockCode string) bool {
	switch strings.TrimSpace(stockCode) {
	case "06871":
		return true
	default:
		return false
	}
}

func rawSectionValues(sections []collector.RawSection) []string {
	out := make([]string, 0, len(sections)*4)
	for _, section := range sections {
		for _, item := range section.Items {
			out = append(out, item.Label, item.Value)
		}
	}
	return out
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func upsertCompany(tx *gorm.DB, stockID uint64, d collector.StockDetail) error {
	c := d.Company
	row := gormmodel.StockCompany{
		StockID: stockID,

		Address:        c.Address,
		Registrar:      c.Registrar,
		RegistrarPhone: c.RegistrarPhone,
		Chairman:       c.Chairman,
		Phone:          c.Phone,
		Business:       c.Business,
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stock_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"address", "registrar", "registrar_phone", "chairman", "phone", "business",
			"updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("upsert stock_companies: %w", err)
	}
	return nil
}

func upsertGreenShoe(tx *gorm.DB, stockID uint64, d collector.StockDetail) error {
	g := d.GreenShoe
	row := gormmodel.StockGreenShoe{
		StockID:      stockID,
		RatePct:      g.RatePct,
		AmountShares: g.AmountShares,
		AmountText:   g.AmountText,
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stock_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"rate_pct", "amount_shares", "amount_text", "updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("upsert stock_greenshoe: %w", err)
	}
	return nil
}

func upsertGreyMarket(tx *gorm.DB, stockID uint64, d collector.StockDetail) error {
	g := d.GreyMarket
	row := gormmodel.StockGreyMarket{
		StockID:      stockID,
		GreyDate:     parseDatePtr(g.Date),
		IncrRatePct:  g.IncrRatePct,
		IncrRatePct2: g.IncrRatePct2,
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stock_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"grey_date", "incr_rate_pct", "incr_rate_pct2", "updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("upsert stock_grey_market: %w", err)
	}
	return nil
}

func upsertPerformance(tx *gorm.DB, stockID uint64, d collector.StockDetail) error {
	p := d.Performance
	row := gormmodel.StockPerformance{
		StockID:             stockID,
		FirstDayIncrRatePct: p.FirstDayIncrRatePct,
		TotalIncrRatePct:    p.TotalIncrRatePct,
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stock_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"first_day_incr_rate_pct", "total_incr_rate_pct", "updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("upsert stock_performance: %w", err)
	}
	return nil
}

func upsertRaiseMoney(tx *gorm.DB, stockID uint64, d collector.StockDetail) error {
	r := d.RaiseMoney
	row := gormmodel.StockRaiseMoney{
		StockID:    stockID,
		AmountHkd:  r.AmountHkd,
		AmountText: r.AmountText,
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stock_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"amount_hkd", "amount_text", "updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("upsert stock_fundraising: %w", err)
	}
	return nil
}

func upsertAllotmentSummary(tx *gorm.DB, stockID uint64, d collector.StockDetail) error {
	s := d.Allotment.Summary
	row := gormmodel.StockAllotmentSummary{
		StockID:              stockID,
		Applicants:           s.Applicants,
		OneLotWinRatePct:     s.OneLotWinRatePct,
		MaxLots:              s.MaxLots,
		OfferPrice:           s.OfferPrice,
		OfferPriceLow:        s.OfferPriceLow,
		OfferPriceHigh:       s.OfferPriceHigh,
		SubscriptionMultiple: s.SubscriptionMultiple,
		ClawbackRatePct:      s.ClawbackRatePct,
		AnnouncementUrl:      s.AnnouncementUrl,
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stock_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"applicants", "one_lot_win_rate_pct", "max_lots",
			"offer_price", "offer_price_low", "offer_price_high",
			"subscription_multiple", "clawback_rate_pct", "announcement_url",
			"updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("upsert stock_allotment_summary: %w", err)
	}
	return nil
}

// ---- rebuild helpers (delete + insert) ----

func rebuildAllotmentTiers(tx *gorm.DB, stockID uint64, tiers []collector.AllotmentTier) error {
	if err := tx.Where("stock_id = ?", stockID).Delete(&gormmodel.StockAllotmentTier{}).Error; err != nil {
		return fmt.Errorf("delete stock_allotment_tiers: %w", err)
	}
	for i, t := range tiers {
		row := gormmodel.StockAllotmentTier{
			StockID:    stockID,
			Seq:        i + 1,
			GroupCode:  t.Group,
			Lots:       t.Lots,
			Applicants: t.Applicants,
			WinLots:    t.WinLots,
			WinRatePct: t.WinRatePct,
			Remark:     t.Remark,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert stock_allotment_tiers: %w", err)
		}
	}
	return nil
}

func rebuildUseOfProceeds(tx *gorm.DB, stockID uint64, list []string) error {
	if err := tx.Where("stock_id = ?", stockID).Delete(&gormmodel.StockUseOfProceeds{}).Error; err != nil {
		return fmt.Errorf("delete stock_use_of_proceeds: %w", err)
	}
	seq := 0
	for _, t := range list {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		seq++
		row := gormmodel.StockUseOfProceeds{StockID: stockID, Seq: seq, Text: t}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert stock_use_of_proceeds: %w", err)
		}
	}
	return nil
}

func rebuildManagement(tx *gorm.DB, stockID uint64, list []collector.ManagementMember) error {
	if err := tx.Where("stock_id = ?", stockID).Delete(&gormmodel.StockManagement{}).Error; err != nil {
		return fmt.Errorf("delete stock_management: %w", err)
	}
	seq := 0
	for _, m := range list {
		if strings.TrimSpace(m.Name) == "" {
			continue
		}
		seq++
		row := gormmodel.StockManagement{
			StockID: stockID,
			Seq:     seq,
			Name:    m.Name,
			Title:   m.Title,
			Bio:     m.Bio,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert stock_management: %w", err)
		}
	}
	return nil
}

func rebuildCompanySecretaries(tx *gorm.DB, stockID uint64, list []string) error {
	if err := tx.Where("stock_id = ?", stockID).Delete(&gormmodel.StockCompanySecretary{}).Error; err != nil {
		return fmt.Errorf("delete stock_secretaries: %w", err)
	}
	seq := 0
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		seq++
		row := gormmodel.StockCompanySecretary{StockID: stockID, Seq: seq, Name: s}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert stock_secretaries: %w", err)
		}
	}
	return nil
}

func rebuildMajorShareholders(tx *gorm.DB, stockID uint64, list []collector.MajorShareholderItem) error {
	if err := tx.Where("stock_id = ?", stockID).Delete(&gormmodel.StockMajorShareholder{}).Error; err != nil {
		return fmt.Errorf("delete stock_major_shareholders: %w", err)
	}
	seq := 0
	for _, item := range list {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		seq++

		var name *string
		if item.Name != "" {
			name = &item.Name
		}
		var pct *float64
		if item.SharePct != 0 {
			pct = &item.SharePct
		}
		rawText := item.Name
		if item.SharePct != 0 {
			rawText = fmt.Sprintf("%s(%.2f%%)", item.Name, item.SharePct)
		}

		row := gormmodel.StockMajorShareholder{
			StockID: stockID,
			Seq:     seq,
			Name:    name,
			Pct:     pct,
			RawText: rawText,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert stock_major_shareholders: %w", err)
		}
	}
	return nil
}

func rebuildIntermediaries(tx *gorm.DB, stockID uint64, d collector.StockDetail) error {
	roleLists := []struct {
		role  string
		names []string
	}{
		{"sponsor", d.Offering.Sponsors},
		{"underwriter", d.Offering.Underwriters},
		{"bookrunner", d.Offering.Bookrunners},
		{"global_coordinator", d.Offering.GlobalCoordinators},
	}

	for _, rl := range roleLists {
		if err := tx.Where("stock_id = ? AND role = ?", stockID, rl.role).Delete(&gormmodel.StockIntermediary{}).Error; err != nil {
			return fmt.Errorf("delete stock_intermediaries role=%s: %w", rl.role, err)
		}

		seq := 0
		for _, name := range rl.names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			seq++

			interID, err := ensureIntermediary(tx, name)
			if err != nil {
				return err
			}

			link := gormmodel.StockIntermediary{
				StockID:        stockID,
				IntermediaryID: interID,
				Role:           rl.role,
				Seq:            seq,
			}
			if err := tx.Create(&link).Error; err != nil {
				return fmt.Errorf("insert stock_intermediaries: %w", err)
			}
		}
	}
	return nil
}

func ensureIntermediary(tx *gorm.DB, name string) (uint64, error) {
	row := gormmodel.Intermediary{Name: name}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
	}).Create(&row).Error; err != nil {
		return 0, fmt.Errorf("upsert intermediaries: %w", err)
	}

	var out gormmodel.Intermediary
	if err := tx.Where("name = ?", name).First(&out).Error; err != nil {
		return 0, fmt.Errorf("select intermediaries id: %w", err)
	}
	return out.ID, nil
}

func rebuildRawSections(tx *gorm.DB, stockID uint64, sections []collector.RawSection) error {
	if len(sections) == 0 {
		return nil
	}

	for _, sec := range sections {
		secID := strings.TrimSpace(sec.SectionId)
		if secID == "" {
			continue
		}

		// Upsert raw section (unique by stock_id+section_id)
		rs := gormmodel.StockRawSection{
			StockID:   stockID,
			SectionID: secID,
			Seq:       nil,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "stock_id"}, {Name: "section_id"}},
			DoNothing: true,
		}).Create(&rs).Error; err != nil {
			return fmt.Errorf("upsert stock_raw_sections: %w", err)
		}

		var out gormmodel.StockRawSection
		if err := tx.Where("stock_id=? AND section_id=?", stockID, secID).First(&out).Error; err != nil {
			return fmt.Errorf("select stock_raw_sections id: %w", err)
		}

		if err := tx.Where("raw_section_id = ?", out.ID).Delete(&gormmodel.StockRawItem{}).Error; err != nil {
			return fmt.Errorf("delete stock_raw_items: %w", err)
		}

		seq := 0
		for _, it := range sec.Items {
			label := strings.TrimSpace(it.Label)
			value := strings.TrimSpace(it.Value)
			if label == "" && value == "" {
				continue
			}
			seq++
			row := gormmodel.StockRawItem{
				RawSectionID: out.ID,
				Seq:          seq,
				Label:        label,
				Value:        value,
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("insert stock_raw_items: %w", err)
			}
		}
	}
	return nil
}

func parseDatePtr(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}
