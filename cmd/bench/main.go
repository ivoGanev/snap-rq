package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adrg/xdg"

	"snap-rq/backend/database"
	"snap-rq/backend/services"
)

// query mirrors the exact SQL used by RequestService.GetAllRequestSummaries,
// which is what the frontend "All requests" button calls.
const query = `SELECT id, collection_id, name, url, method, status_code, response_id FROM http_requests ORDER BY name`

// defaultIterations is the number of times the function is executed when no
// -iterations flag is supplied. Increase it on slower machines or small DBs.
const defaultIterations = 100

type SizeInfo struct {
	RowCount     int64 `json:"row_count"`
	PayloadBytes int64 `json:"payload_bytes"`
	FileBytes    int64 `json:"file_bytes"`
}

type HardwareInfo struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	CPU         string `json:"cpu"`
	CPUs        int    `json:"cpus"`
	MemoryBytes uint64 `json:"memory_bytes"`
}

type BenchmarkReport struct {
	Date            string       `json:"date"`
	Query           string       `json:"query"`
	Function        string       `json:"function"`
	Iterations      int          `json:"iterations"`
	TotalDuration   string       `json:"total_duration"`
	AverageDuration string       `json:"average_duration"`
	MinDuration     string       `json:"min_duration"`
	MaxDuration     string       `json:"max_duration"`
	Schema          string       `json:"schema"`
	Size            SizeInfo     `json:"size"`
	Hardware        HardwareInfo `json:"hardware"`
}

func main() {
	iterations := defaultIterations
	if len(os.Args) > 2 && os.Args[1] == "-iterations" {
		if n, err := fmt.Sscanf(os.Args[2], "%d", &iterations); n != 1 || err != nil {
			fmt.Fprintf(os.Stderr, "invalid -iterations value: %s\n", os.Args[2])
			os.Exit(1)
		}
	}

	db, err := database.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	svc := services.NewRequestService(db)
	dbPath := appDBPath()

	schema, err := getSchema(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading schema: %v\n", err)
		os.Exit(1)
	}

	size, err := getSize(db, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading size: %v\n", err)
		os.Exit(1)
	}

	hw, err := getHardware()
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading hardware: %v\n", err)
		os.Exit(1)
	}

	// Warmup: make sure caches, connections and the query plan are settled.
	if _, err := svc.GetAllRequestSummaries(); err != nil {
		fmt.Fprintf(os.Stderr, "warmup call failed: %v\n", err)
		os.Exit(1)
	}

	runs := make([]time.Duration, 0, iterations)
	overallStart := time.Now()
	for i := 0; i < iterations; i++ {
		start := time.Now()
		if _, err := svc.GetAllRequestSummaries(); err != nil {
			fmt.Fprintf(os.Stderr, "benchmark call failed: %v\n", err)
			os.Exit(1)
		}
		runs = append(runs, time.Since(start))
	}
	total := time.Since(overallStart)

	avg := total / time.Duration(iterations)
	min, max := runs[0], runs[0]
	for _, d := range runs[1:] {
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}

	report := BenchmarkReport{
		Date:            time.Now().UTC().Format(time.RFC3339),
		Query:           query,
		Function:        "RequestService.GetAllRequestSummaries",
		Iterations:      iterations,
		TotalDuration:   total.String(),
		AverageDuration: avg.String(),
		MinDuration:     min.String(),
		MaxDuration:     max.String(),
		Schema:          schema,
		Size:            size,
		Hardware:        hw,
	}

	outDir := "benchmarks"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "creating benchmarks directory: %v\n", err)
		os.Exit(1)
	}
	filename := fmt.Sprintf("benchmark-%s.json", time.Now().Format("20060102-150405"))
	outPath := filepath.Join(outDir, filename)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encoding report: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "writing report: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(outPath)
}

// appDBPath returns the same SQLite path that database.Open uses.
func appDBPath() string {
	return filepath.Join(xdg.DataHome, "snap-rq-wails-v3", "app.db")
}

// getSchema returns the CREATE TABLE and CREATE INDEX statements for
// http_requests so the benchmark report documents exactly what is being queried.
func getSchema(db *sql.DB) (string, error) {
	rows, err := db.Query(
		`SELECT sql FROM sqlite_master
		 WHERE tbl_name = 'http_requests' AND type IN ('table', 'index')
		 ORDER BY type, name`,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var stmts []string
	for rows.Next() {
		var sqlText sql.NullString
		if err := rows.Scan(&sqlText); err != nil {
			return "", err
		}
		if sqlText.Valid {
			stmts = append(stmts, sqlText.String)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if len(stmts) == 0 {
		return "", fmt.Errorf("no schema found for http_requests")
	}
	return strings.Join(stmts, ";\n"), nil
}

// getSize measures how many rows exist in http_requests, an approximation of
// the text payload size, and the on-disk size of the SQLite file.
func getSize(db *sql.DB, dbPath string) (SizeInfo, error) {
	var info SizeInfo
	row := db.QueryRow(
		`SELECT COUNT(*),
		        COALESCE(SUM(
					LENGTH(COALESCE(name, '')) +
					LENGTH(COALESCE(url, '')) +
					LENGTH(COALESCE(method, '')) +
					LENGTH(COALESCE(body, '')) +
					LENGTH(COALESCE(request_headers, ''))
				), 0)
		 FROM http_requests`,
	)
	if err := row.Scan(&info.RowCount, &info.PayloadBytes); err != nil {
		return info, err
	}

	fi, err := os.Stat(dbPath)
	if err != nil {
		return info, err
	}
	info.FileBytes = fi.Size()
	return info, nil
}

// getHardware collects basic environment details for the benchmark report.
func getHardware() (HardwareInfo, error) {
	cpu := os.Getenv("PROCESSOR_IDENTIFIER")
	if cpu == "" {
		cpu = runtime.GOARCH
	}

	mem, err := totalMemory()
	if err != nil {
		return HardwareInfo{}, err
	}

	return HardwareInfo{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		CPU:         cpu,
		CPUs:        runtime.NumCPU(),
		MemoryBytes: mem,
	}, nil
}
