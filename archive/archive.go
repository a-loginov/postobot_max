package archive

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Columns written by the export script (export_month.py) — keep in sync.
const (
	colRequestID = "A" // request_id
	colUserID    = "B" // max_user_id
	colName      = "C" // name
	colSurname   = "D" // surname
	colClass     = "E" // class
	colDesc      = "F" // description
	colStatus    = "G" // status
	colCreated   = "H" // created_at
)

type Entry struct {
	RequestID   uint
	UserID      int64
	Name        string
	Surname     string
	Class       string
	Description string
	Status      string
	CreatedAt   string
}

// FileName returns the Excel archive file for a month, e.g. "2025-08.xlsx".
func FileName(year int, month time.Month) string {
	return fmt.Sprintf("%04d-%02d.xlsx", year, int(month))
}

// ListForUser loads the archive file for a month and returns entries of a user.
func ListForUser(dir string, userID int64, year int, month time.Month) ([]Entry, error) {
	path := filepath.Join(dir, FileName(year, month))

	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open archive %s: %w", path, err)
	}
	defer f.Close()

	rows, err := f.GetRows("Sheet1")
	if err != nil {
		return nil, fmt.Errorf("read archive rows: %w", err)
	}

	var entries []Entry
	for i, row := range rows {
		if i == 0 {
			continue // header
		}
		if len(row) < len(colCreated) {
			continue
		}
		uid, err := strconv.ParseInt(strings.TrimSpace(row[1]), 10, 64)
		if err != nil || uid != userID {
			continue
		}

		reqID, _ := strconv.ParseUint(strings.TrimSpace(row[0]), 10, 64)
		entries = append(entries, Entry{
			RequestID:   uint(reqID),
			UserID:      uid,
			Name:        row[2],
			Surname:     row[3],
			Class:       row[4],
			Description: row[5],
			Status:      row[6],
			CreatedAt:   row[7],
		})
	}

	return entries, nil
}

// monthStems maps common Russian month stems (covers январЯ, январе, февралЯ,
// феврале, марте, апреле, мае, июле, октябре, сентябре, ноябре, декабре etc.)
// to the corresponding month.
var monthStems = map[string]time.Month{
	"январ":   time.January,
	"феврал":  time.February,
	"март":    time.March,
	"апрел":   time.April,
	"май":     time.May,
	"июн":     time.June,
	"июл":     time.July,
	"август":  time.August,
	"сентябр": time.September,
	"октябр":  time.October,
	"ноябр":   time.November,
	"декабр":  time.December,
}

// MonthHint returns the Russian month stem found in the text (longest match),
// or "" if the text does not mention any month.
func MonthHint(text string) string {
	lower := strings.ToLower(text)
	longest := ""
	for stem := range monthStems {
		if strings.Contains(lower, stem) && len(stem) > len(longest) {
			longest = stem
		}
	}
	return longest
}

// ParseMonth extracts (month, year) from free text like "покажи что я просил в августе 25".
// Missing year defaults to the current year.
func ParseMonth(text string, now time.Time) (time.Month, int) {
	lower := strings.ToLower(text)
	month := time.December
	found := false
	for stem, m := range monthStems {
		if strings.Contains(lower, stem) {
			month = m
			found = true
			break
		}
	}
	if !found {
		return month, 0
	}

	year := now.Year()
	re := regexp.MustCompile(`(19|20)?(\d{2})`)
	match := re.FindStringSubmatch(text)
	if len(match) == 3 && match[2] != "" {
		yy, _ := strconv.Atoi(match[2])
		if yy < 100 {
			year = 2000 + yy
		} else {
			year = yy
		}
	}
	return month, year
}
