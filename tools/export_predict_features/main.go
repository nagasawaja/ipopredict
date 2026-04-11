package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"

	_ "github.com/mattn/go-sqlite3"

	"hk_ipo/pkg/ipoprior"
)

func main() {
	dbPath := flag.String("db", "sql/hk_ipo.db", "sqlite db path")
	outPath := flag.String("out", "", "output csv path (default stdout)")
	flag.Parse()

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	stocks, err := ipoprior.LoadStockFeatures(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}
	rows := ipoprior.BuildExportRows(stocks)

	var w io.Writer = os.Stdout
	var file *os.File
	if *outPath != "" {
		file, err = os.Create(*outPath)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		w = file
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{
		"stock_id",
		"stock_code",
		"stock_name",
		"apply_start_date",
		"apply_end_date",
		"list_date",
		"actual_total_applicants",
		"actual_b_ratio",
		"actual_a_one_lot_ratio",
		"public_lots",
		"per_lot_money",
		"admission_fee_hkd",
		"subscription_multiple",
		"overlap_stock_count",
		"overlap_public_lots_sum",
		"overlap_entry_fee_sum",
		"overlap_low_entry_count",
		"overlap_small_public_lots_count",
		"public_lots_bucket",
		"per_lot_money_bucket",
		"subscription_bucket",
	}
	if err := cw.Write(header); err != nil {
		log.Fatal(err)
	}
	for _, row := range rows {
		record := []string{
			strconv.FormatInt(row.StockID, 10),
			row.StockCode,
			row.StockName,
			row.ApplyStartDate,
			row.ApplyEndDate,
			row.ListDate,
			strconv.Itoa(row.ActualTotalApplicants),
			fmt.Sprintf("%.6f", row.ActualBRatio),
			fmt.Sprintf("%.6f", row.ActualAOneLotRatio),
			fmt.Sprintf("%.2f", row.PublicLots),
			fmt.Sprintf("%.2f", row.PerLotMoney),
			fmt.Sprintf("%.2f", row.AdmissionFeeHKD),
			fmt.Sprintf("%.4f", row.SubscriptionMultiple),
			strconv.Itoa(row.OverlapStockCount),
			fmt.Sprintf("%.2f", row.OverlapPublicLotsSum),
			fmt.Sprintf("%.2f", row.OverlapEntryFeeSum),
			strconv.Itoa(row.OverlapLowEntryCount),
			strconv.Itoa(row.OverlapSmallPublicLotsCount),
			row.PublicLotsBucket,
			row.PerLotMoneyBucket,
			row.SubscriptionBucket,
		}
		if err := cw.Write(record); err != nil {
			log.Fatal(err)
		}
	}
	if err := cw.Error(); err != nil {
		log.Fatal(err)
	}
}
