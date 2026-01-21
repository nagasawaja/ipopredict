package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// SubscriptionTier 申购阶梯信息
type SubscriptionTier struct {
	Lots   int     // 申购手数
	Amount float64 // 应付金额（HK$）
}

// IPOInfo 存储从PDF中提取的IPO基本信息
type IPOInfo struct {
	StockName             string  // 股票名称（含代码）
	PriceMax              float64 // 最高发行价（HKD）
	LotSize               int     // 每手股数
	GlobalOffering        int64   // 全球发售总股数
	HKPublicOffering      int64   // 香港公开发售股数
	InternationalOffering int64   // 国际发售股数
	Fees                  struct {
		Brokerage   float64 // 经纪费 (%)
		SFCLevy     float64 // SFC交易征费 (%)
		ExchangeFee float64 // 港交所交易费 (%)
		AFRCLevy    float64 // AFRC交易征费 (%)
	}
	SubscriptionTiers []SubscriptionTier // 申购阶梯表
}

// extractIPOInfo 从PDF文本中提取IPO信息
func extractIPOInfo(text string) IPOInfo {
	info := IPOInfo{}

	// 1. 提取股票名称和代码
	// 匹配模式：公司名称 + 股票代码（通常在 "Stock code:" 或 "股票代码:" 后面）
	stockCodePattern := regexp.MustCompile(`(?i)(?:Stock\s+code|股票代码|股份代号)[:\s]+(\d{4})`)
	stockCodeMatch := stockCodePattern.FindStringSubmatch(text)

	// 匹配公司名称（通常在PDF开头或"Global Offering"附近）
	// 优先匹配包含"GROUP"、"CO."、"LTD"等关键词的公司名
	companyPatterns := []*regexp.Regexp{
		regexp.MustCompile(`([A-Z][A-Z\s&,\.\-]+(?:GROUP|CO\.|LTD\.|LIMITED|COMPANY|INC\.))`),
		regexp.MustCompile(`([A-Z][A-Z\s&,\.\-]+(?:CO\.|LTD\.|LIMITED))`),
	}

	var companyName string
	for _, pattern := range companyPatterns {
		matches := pattern.FindAllString(text, 10)
		if len(matches) > 0 {
			// 选择最长的匹配（通常是完整的公司名）
			for _, match := range matches {
				trimmed := strings.TrimSpace(match)
				if len(trimmed) > len(companyName) && len(trimmed) < 100 {
					companyName = trimmed
				}
			}
			if companyName != "" {
				break
			}
		}
	}

	if companyName != "" {
		if len(stockCodeMatch) > 1 {
			info.StockName = fmt.Sprintf("%s (%s.HK)", companyName, stockCodeMatch[1])
		} else {
			// 如果没有找到股票代码，尝试从文本中查找4位数字
			codePattern := regexp.MustCompile(`\b(\d{4})\b`)
			codeMatch := codePattern.FindString(text)
			if codeMatch != "" {
				info.StockName = fmt.Sprintf("%s (%s.HK)", companyName, codeMatch)
			} else {
				info.StockName = companyName
			}
		}
	}

	// 2. 提取最高发行价
	// 匹配模式：HK$236.60 或 Maximum Offer Price: HK$236.60
	pricePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:Maximum\s+Offer\s+Price|最高发行价|最高发售价)[:\s]+HK\$\s*([\d,]+\.?\d*)`),
		regexp.MustCompile(`(?i)HK\$\s*([\d,]+\.?\d*)\s+per\s+(?:H\s+)?Share`),
		regexp.MustCompile(`HK\$\s*([\d,]+\.?\d*)`),
	}

	var maxPrice float64
	for _, pattern := range pricePatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) > 1 && match[1] != "" {
				priceStr := strings.ReplaceAll(match[1], ",", "")
				if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
					// 只接受合理的价格范围（港股通常在0.01-10000之间）
					if price > 0.01 && price < 10000 && price > maxPrice {
						maxPrice = price
					}
				}
			}
		}
		if maxPrice > 0 {
			break // 如果第一个模式找到了，就停止
		}
	}
	info.PriceMax = maxPrice

	// 3. 提取每手股数
	// 匹配模式：100 H Shares 或 board lots of 100 或 每手100股
	lotSizePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:board\s+lots?\s+of|每手)\s+(\d+)\s+(?:H\s+)?(?:Shares?|股)`),
		regexp.MustCompile(`(?i)(\d+)\s+(?:H\s+)?Shares?\s+each`),
		regexp.MustCompile(`(?i)minimum\s+of\s+(\d+)\s+(?:H\s+)?(?:Shares?|股)`),
		regexp.MustCompile(`(?i)(\d+)\s+(?:H\s+)?Shares?`), // 匹配数字+Shares格式
	}

	for _, pattern := range lotSizePatterns {
		match := pattern.FindStringSubmatch(text)
		if len(match) > 1 {
			if lotSize, err := strconv.Atoi(match[1]); err == nil {
				info.LotSize = lotSize
				break
			}
		}
	}

	// 如果没找到，默认港股通常是100
	if info.LotSize == 0 {
		info.LotSize = 100
	}

	// 4. 提取发售股数信息
	extractOfferingInfo(text, &info)

	// 5. 提取费用信息
	extractFeesInfo(text, &info)

	// 6. 提取申购阶梯表
	info.SubscriptionTiers = extractSubscriptionTiers(text)

	return info
}

// extractOfferingInfo 提取发售股数信息
func extractOfferingInfo(text string, info *IPOInfo) {
	// 匹配全球发售总股数
	globalPattern := regexp.MustCompile(`(?i)(?:Number\s+of\s+Offer\s+Shares\s+under\s+the\s+Global\s+Offering|全球发售总股数)[:\s]+([\d,]+)\s+(?:H\s+)?Shares?`)
	if match := globalPattern.FindStringSubmatch(text); len(match) > 1 {
		globalStr := strings.ReplaceAll(match[1], ",", "")
		if shares, err := strconv.ParseInt(globalStr, 10, 64); err == nil {
			info.GlobalOffering = shares
		}
	}

	// 匹配香港公开发售股数
	hkPattern := regexp.MustCompile(`(?i)(?:Number\s+of\s+Hong\s+Kong\s+Offer\s+Shares|香港公开发售股数)[:\s]+([\d,]+)\s+(?:H\s+)?Shares?`)
	if match := hkPattern.FindStringSubmatch(text); len(match) > 1 {
		hkStr := strings.ReplaceAll(match[1], ",", "")
		if shares, err := strconv.ParseInt(hkStr, 10, 64); err == nil {
			info.HKPublicOffering = shares
		}
	}

	// 匹配国际发售股数
	intlPattern := regexp.MustCompile(`(?i)(?:Number\s+of\s+International\s+Offer\s+Shares|国际发售股数)[:\s]+([\d,]+)\s+(?:H\s+)?Shares?`)
	if match := intlPattern.FindStringSubmatch(text); len(match) > 1 {
		intlStr := strings.ReplaceAll(match[1], ",", "")
		if shares, err := strconv.ParseInt(intlStr, 10, 64); err == nil {
			info.InternationalOffering = shares
		}
	}
}

// extractFeesInfo 提取费用信息
func extractFeesInfo(text string, info *IPOInfo) {
	// 匹配经纪费
	brokeragePattern := regexp.MustCompile(`(?i)(?:brokerage|经纪费)[\s:]+of\s+([\d.]+)%`)
	if match := brokeragePattern.FindStringSubmatch(text); len(match) > 1 {
		if fee, err := strconv.ParseFloat(match[1], 64); err == nil {
			info.Fees.Brokerage = fee
		}
	}

	// 匹配SFC交易征费
	sfcPattern := regexp.MustCompile(`(?i)(?:SFC\s+transaction\s+levy|SFC交易征费)[\s:]+of\s+([\d.]+)%`)
	if match := sfcPattern.FindStringSubmatch(text); len(match) > 1 {
		if fee, err := strconv.ParseFloat(match[1], 64); err == nil {
			info.Fees.SFCLevy = fee
		}
	}

	// 匹配港交所交易费
	exchangePattern := regexp.MustCompile(`(?i)(?:Hong\s+Kong\s+Stock\s+Exchange\s+trading\s+fee|港交所交易费)[\s:]+of\s+([\d.]+)%`)
	if match := exchangePattern.FindStringSubmatch(text); len(match) > 1 {
		if fee, err := strconv.ParseFloat(match[1], 64); err == nil {
			info.Fees.ExchangeFee = fee
		}
	}

	// 匹配AFRC交易征费
	afrcPattern := regexp.MustCompile(`(?i)(?:AFRC\s+transaction\s+levy|AFRC交易征费)[\s:]+of\s+([\d.]+)%`)
	if match := afrcPattern.FindStringSubmatch(text); len(match) > 1 {
		if fee, err := strconv.ParseFloat(match[1], 64); err == nil {
			info.Fees.AFRCLevy = fee
		}
	}
}

// extractSubscriptionTiers 从PDF文本中提取申购阶梯表
func extractSubscriptionTiers(text string) []SubscriptionTier {
	var tiers []SubscriptionTier

	// 匹配表格数据：手数和金额对
	// 模式1: 匹配 "100 23,898.62" 这样的格式（手数在前，金额在后，可能有逗号分隔）
	// 需要匹配多列格式，每行可能有多个手数-金额对

	// 先找到表格区域（通常在"NUMBER OF HONG KONG OFFER SHARES"附近）
	tableStartPattern := regexp.MustCompile(`(?i)(?:NUMBER\s+OF\s+HONG\s+KONG\s+OFFER\s+SHARES|申购数量|申购手数)`)
	tableEndPattern := regexp.MustCompile(`(?i)(?:APPLICATION\s+FOR\s+LISTING|申请上市|配售结果)`)

	startIdx := -1
	endIdx := len(text)

	if match := tableStartPattern.FindStringIndex(text); match != nil {
		startIdx = match[0]
	}
	if match := tableEndPattern.FindStringIndex(text); match != nil && match[0] > startIdx {
		endIdx = match[0]
	}

	var tableText string
	if startIdx >= 0 {
		tableText = text[startIdx:endIdx]
	} else {
		tableText = text
	}

	// 匹配手数和金额对
	// 模式：数字（手数） + 空格/换行 + HK$或数字（金额，可能带逗号）
	// 需要匹配多种格式：
	// 1. "100 23,898.62"
	// 2. "1,000 238,986.11"
	// 3. "705,100(1) 168,509,106.87"

	// PDF提取时表格可能没有空格，需要匹配连续的数字对
	// 格式可能是：10023,898.62 (实际上是 100 23,898.62)
	// 或者：1,000238,986.11 (实际上是 1,000 238,986.11)

	// 策略：找到所有可能的数字对，然后验证它们是否合理
	// 手数模式：100, 200, 300, 1,000, 10,000, 100,000 等
	// 金额模式：23,898.62, 238,986.11 等（通常有小数点后2位）

	// 先尝试匹配有空格的情况
	pairPattern1 := regexp.MustCompile(`(\d{1,3}(?:,\d{3})*)(?:\(1\))?\s+(\d{1,3}(?:,\d{3})*\.\d{2})`)
	matches1 := pairPattern1.FindAllStringSubmatch(tableText, -1)

	// 再尝试匹配无空格的情况（手数+金额连在一起）
	// 手数可能是：100, 200, 1,000, 10,000, 100,000, 705,100
	// 金额总是以小数点后2位结尾
	pairPattern2 := regexp.MustCompile(`(\d{1,3}(?:,\d{3})*)(?:\(1\))?(\d{1,3}(?:,\d{3})*\.\d{2})`)
	matches2 := pairPattern2.FindAllStringSubmatch(tableText, -1)

	seen := make(map[int]bool) // 用于去重

	// 处理有空格的情况
	for _, match := range matches1 {
		if len(match) < 3 {
			continue
		}
		processMatch(match, &tiers, seen)
	}

	// 处理无空格的情况（需要更严格的验证）
	for _, match := range matches2 {
		if len(match) < 3 {
			continue
		}
		// 验证：手数应该较小（通常<1000000），金额应该较大（通常>10000）
		lotStr := strings.ReplaceAll(match[1], ",", "")
		amountStr := strings.ReplaceAll(match[2], ",", "")

		lots, err1 := strconv.Atoi(lotStr)
		amount, err2 := strconv.ParseFloat(amountStr, 64)

		if err1 == nil && err2 == nil {
			// 验证合理性：手数<1000000，金额>10000，且金额/手数在合理范围内（100-500）
			if lots >= 100 && lots < 1000000 && amount >= 10000 {
				ratio := amount / float64(lots)
				if ratio >= 100 && ratio <= 500 && !seen[lots] {
					tiers = append(tiers, SubscriptionTier{
						Lots:   lots,
						Amount: amount,
					})
					seen[lots] = true
				}
			}
		}
	}

	return tiers
}

// formatNumber 格式化数字，添加千位分隔符
func formatNumber(n int64) string {
	str := strconv.FormatInt(n, 10)
	if len(str) <= 3 {
		return str
	}

	var result strings.Builder
	for i, char := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result.WriteString(",")
		}
		result.WriteRune(char)
	}
	return result.String()
}

// processMatch 处理匹配结果并添加到tiers中
func processMatch(match []string, tiers *[]SubscriptionTier, seen map[int]bool) {
	if len(match) < 3 {
		return
	}

	// 解析手数
	lotStr := strings.ReplaceAll(match[1], ",", "")
	lots, err := strconv.Atoi(lotStr)
	if err != nil {
		return
	}

	// 解析金额
	amountStr := strings.ReplaceAll(match[2], ",", "")
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return
	}

	// 验证合理性：手数应该在100-1000000之间，金额应该在10000-100000000之间
	if lots >= 100 && lots <= 1000000 && amount >= 10000 && amount <= 100000000 {
		// 去重：如果手数已存在，跳过
		if !seen[lots] {
			*tiers = append(*tiers, SubscriptionTier{
				Lots:   lots,
				Amount: amount,
			})
			seen[lots] = true
		}
	}
}

func main() {
	// 支持命令行参数指定PDF文件，默认读取a.pdf
	pdfPath := "b.pdf"
	if len(os.Args) > 1 {
		pdfPath = os.Args[1]
	}

	// 检查文件是否存在
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		fmt.Printf("错误: 文件 %s 不存在\n", pdfPath)
		fmt.Println("用法: go run main.go [PDF文件路径]")
		os.Exit(1)
	}

	// 打开PDF文件
	file, reader, err := pdf.Open(pdfPath)
	if err != nil {
		fmt.Printf("错误: 无法打开PDF文件: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// 获取PDF信息
	totalPages := reader.NumPage()
	fmt.Println("==================================================")
	fmt.Printf("📄 PDF文件: %s\n", pdfPath)
	fmt.Printf("📊 总页数: %d 页\n", totalPages)
	fmt.Println("==================================================")

	// 读取前几页（通常关键信息在前5页，申购阶梯表也在前几页）
	var allText strings.Builder
	pagesToRead := totalPages
	if pagesToRead > 10 {
		pagesToRead = 10 // 读取前10页以确保包含申购阶梯表
	}

	for i := 1; i <= pagesToRead; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}

		// 提取文本内容
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}

		text = strings.TrimSpace(text)
		if text != "" {
			allText.WriteString(text + "\n")
		}
	}

	// 提取IPO信息
	fullText := allText.String()
	ipoInfo := extractIPOInfo(fullText)

	// 打印提取的信息
	fmt.Println("\n🎯 提取的IPO信息:")
	fmt.Println("==================================================")
	fmt.Printf("股票名称: %s\n", ipoInfo.StockName)
	fmt.Printf("最高发行价: HK$%.2f\n", ipoInfo.PriceMax)
	fmt.Printf("每手股数: %d\n", ipoInfo.LotSize)

	if ipoInfo.GlobalOffering > 0 {
		fmt.Printf("全球发售总股数: %s H Shares\n", formatNumber(ipoInfo.GlobalOffering))
	}
	if ipoInfo.HKPublicOffering > 0 {
		fmt.Printf("香港公开发售股数: %s H Shares\n", formatNumber(ipoInfo.HKPublicOffering))
	}
	if ipoInfo.InternationalOffering > 0 {
		fmt.Printf("国际发售股数: %s H Shares\n", formatNumber(ipoInfo.InternationalOffering))
	}

	if ipoInfo.Fees.Brokerage > 0 || ipoInfo.Fees.SFCLevy > 0 || ipoInfo.Fees.ExchangeFee > 0 || ipoInfo.Fees.AFRCLevy > 0 {
		fmt.Println("\n费用信息:")
		if ipoInfo.Fees.Brokerage > 0 {
			fmt.Printf("  经纪费: %.4f%%\n", ipoInfo.Fees.Brokerage)
		}
		if ipoInfo.Fees.SFCLevy > 0 {
			fmt.Printf("  SFC交易征费: %.4f%%\n", ipoInfo.Fees.SFCLevy)
		}
		if ipoInfo.Fees.ExchangeFee > 0 {
			fmt.Printf("  港交所交易费: %.4f%%\n", ipoInfo.Fees.ExchangeFee)
		}
		if ipoInfo.Fees.AFRCLevy > 0 {
			fmt.Printf("  AFRC交易征费: %.4f%%\n", ipoInfo.Fees.AFRCLevy)
		}
	}

	fmt.Printf("\n申购阶梯表: %d 条记录\n", len(ipoInfo.SubscriptionTiers))
	fmt.Println("==================================================")

	// 打印申购阶梯表（按手数排序）
	if len(ipoInfo.SubscriptionTiers) > 0 {
		// 按手数排序
		sort.Slice(ipoInfo.SubscriptionTiers, func(i, j int) bool {
			return ipoInfo.SubscriptionTiers[i].Lots < ipoInfo.SubscriptionTiers[j].Lots
		})

		fmt.Println("\n📊 申购阶梯表:")
		fmt.Println("==================================================")
		fmt.Printf("%-12s %-25s\n", "申购手数", "应付金额(HK$)")
		fmt.Println("--------------------------------------------------")
		for _, tier := range ipoInfo.SubscriptionTiers {
			fmt.Printf("%-12d %-25.2f\n", tier.Lots, tier.Amount)
		}
		fmt.Println("==================================================")
	}

	// 打印结构体（便于程序使用）
	fmt.Println("\n📋 结构体数据:")
	fmt.Println("==================================================")
	fmt.Printf("var currentIPO = struct {\n")
	fmt.Printf("\tStockName         string  // 股票名称（含代码）\n")
	fmt.Printf("\tPriceMax          float64 // 最高发行价（HKD）\n")
	fmt.Printf("\tLotSize           int     // 每手股数\n")
	if ipoInfo.GlobalOffering > 0 || ipoInfo.HKPublicOffering > 0 || ipoInfo.InternationalOffering > 0 {
		fmt.Printf("\tGlobalOffering    int64   // 全球发售总股数\n")
		fmt.Printf("\tHKPublicOffering  int64   // 香港公开发售股数\n")
		fmt.Printf("\tInternationalOffering int64 // 国际发售股数\n")
	}
	if ipoInfo.Fees.Brokerage > 0 || ipoInfo.Fees.SFCLevy > 0 || ipoInfo.Fees.ExchangeFee > 0 || ipoInfo.Fees.AFRCLevy > 0 {
		fmt.Printf("\tFees              struct {\n")
		fmt.Printf("\t\tBrokerage   float64 // 经纪费 (%%)\n")
		fmt.Printf("\t\tSFCLevy     float64 // SFC交易征费 (%%)\n")
		fmt.Printf("\t\tExchangeFee float64 // 港交所交易费 (%%)\n")
		fmt.Printf("\t\tAFRCLevy    float64 // AFRC交易征费 (%%)\n")
		fmt.Printf("\t}\n")
	}
	fmt.Printf("}{\n")
	fmt.Printf("\tStockName: \"%s\",\n", ipoInfo.StockName)
	fmt.Printf("\tPriceMax:  %.2f,\n", ipoInfo.PriceMax)
	fmt.Printf("\tLotSize:   %d,\n", ipoInfo.LotSize)
	if ipoInfo.GlobalOffering > 0 {
		fmt.Printf("\tGlobalOffering:   %d,\n", ipoInfo.GlobalOffering)
	}
	if ipoInfo.HKPublicOffering > 0 {
		fmt.Printf("\tHKPublicOffering: %d,\n", ipoInfo.HKPublicOffering)
	}
	if ipoInfo.InternationalOffering > 0 {
		fmt.Printf("\tInternationalOffering: %d,\n", ipoInfo.InternationalOffering)
	}
	if ipoInfo.Fees.Brokerage > 0 || ipoInfo.Fees.SFCLevy > 0 || ipoInfo.Fees.ExchangeFee > 0 || ipoInfo.Fees.AFRCLevy > 0 {
		fmt.Printf("\tFees: struct {\n")
		fmt.Printf("\t\tBrokerage   float64\n")
		fmt.Printf("\t\tSFCLevy     float64\n")
		fmt.Printf("\t\tExchangeFee float64\n")
		fmt.Printf("\t\tAFRCLevy    float64\n")
		fmt.Printf("\t}{\n")
		if ipoInfo.Fees.Brokerage > 0 {
			fmt.Printf("\t\tBrokerage:   %.4f,\n", ipoInfo.Fees.Brokerage)
		}
		if ipoInfo.Fees.SFCLevy > 0 {
			fmt.Printf("\t\tSFCLevy:     %.4f,\n", ipoInfo.Fees.SFCLevy)
		}
		if ipoInfo.Fees.ExchangeFee > 0 {
			fmt.Printf("\t\tExchangeFee: %.4f,\n", ipoInfo.Fees.ExchangeFee)
		}
		if ipoInfo.Fees.AFRCLevy > 0 {
			fmt.Printf("\t\tAFRCLevy:    %.4f,\n", ipoInfo.Fees.AFRCLevy)
		}
		fmt.Printf("\t},\n")
	}
	fmt.Printf("}\n")
	fmt.Println("--------------------------------------------------")

	// 打印申购阶梯表结构体（已排序）
	if len(ipoInfo.SubscriptionTiers) > 0 {
		fmt.Println("\n📋 申购阶梯表结构体:")
		fmt.Println("--------------------------------------------------")
		fmt.Printf("type SubscriptionTier struct {\n")
		fmt.Printf("\tLots   int     // 申购手数\n")
		fmt.Printf("\tAmount float64 // 应付金额（HK$）\n")
		fmt.Printf("}\n\n")
		fmt.Printf("var subscriptionTiers = []SubscriptionTier{\n")
		for _, tier := range ipoInfo.SubscriptionTiers {
			fmt.Printf("\t{Lots: %d, Amount: %.2f},\n", tier.Lots, tier.Amount)
		}
		fmt.Printf("}\n")
		fmt.Println("==================================================")
	}
}
