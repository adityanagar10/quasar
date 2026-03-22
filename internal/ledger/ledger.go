package ledger

import (
	"fmt"
	"os"
	"time"
)

// Writer appends hledger-format transactions to a plain-text ledger file.
type Writer struct {
	path string
}

// NewWriter returns a Writer that appends to path (created if absent).
func NewWriter(path string) *Writer {
	return &Writer{path: path}
}

// WriteExpense appends an expense transaction. Account name is intentionally
// omitted for privacy — only the category and amount are recorded.
//
// Example output:
//
//	2024/01/15 * XYZ Food
//	    Expenses:Food        ₹500.00
//	    Assets:Bank
func (w *Writer) WriteExpense(date time.Time, payee, category string, amount float64) error {
	if category == "" {
		category = "Expenses:General"
	}
	if len(category) < 9 || category[:9] != "Expenses:" {
		category = "Expenses:" + category
	}

	entry := fmt.Sprintf(
		"%s * %s\n    %-30s ₹%.2f\n    Assets:Bank\n\n",
		date.Format("2006/01/02"),
		payee,
		category,
		amount,
	)
	return w.append(entry)
}

func (w *Writer) append(text string) error {
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("ledger open %s: %w", w.path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		return fmt.Errorf("ledger write: %w", err)
	}
	return nil
}
