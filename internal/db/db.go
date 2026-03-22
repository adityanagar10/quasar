package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func New(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	conn.SetMaxOpenConns(1)
	d := &DB{conn: conn}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id INTEGER UNIQUE NOT NULL,
			username TEXT,
			active_session_id INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_session_id ON conversations(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_created_at ON conversations(created_at)`,
		`CREATE TABLE IF NOT EXISTS reminders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			remind_at TIMESTAMP NOT NULL,
			completed BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reminders_user_id ON reminders(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_reminders_remind_at ON reminders(remind_at)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			completed BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id)`,
		`CREATE TABLE IF NOT EXISTS notes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notes_user_id ON notes(user_id)`,
		`CREATE TABLE IF NOT EXISTS goals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			category TEXT NOT NULL,
			name TEXT NOT NULL,
			target TEXT,
			context TEXT,
			active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_goals_user_id ON goals(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_goals_category ON goals(category)`,
		`CREATE TABLE IF NOT EXISTS activities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			goal_id INTEGER,
			category TEXT NOT NULL,
			content TEXT NOT NULL,
			mood TEXT,
			duration INTEGER,
			notes TEXT,
			completed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (goal_id) REFERENCES goals(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_activities_user_id ON activities(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_activities_goal_id ON activities(goal_id)`,
		`CREATE TABLE IF NOT EXISTS recurring_reminders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			cron_expr TEXT NOT NULL,
			content TEXT NOT NULL,
			active BOOLEAN DEFAULT TRUE,
			last_triggered TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recurring_reminders_user_id ON recurring_reminders(user_id)`,

		// --- Finance tables ---
		`CREATE TABLE IF NOT EXISTS accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			starting_balance REAL NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id),
			UNIQUE(user_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			account_id INTEGER,
			amount REAL NOT NULL,
			merchant TEXT NOT NULL,
			category TEXT DEFAULT 'Expenses:General',
			transacted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (account_id) REFERENCES accounts(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON transactions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_transacted_at ON transactions(transacted_at)`,
		`CREATE TABLE IF NOT EXISTS sips (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			account_id INTEGER,
			name TEXT NOT NULL,
			amount REAL NOT NULL,
			debit_day INTEGER NOT NULL,
			active BOOLEAN DEFAULT TRUE,
			last_alerted DATE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (account_id) REFERENCES accounts(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sips_user_id ON sips(user_id)`,
		`CREATE TABLE IF NOT EXISTS yearly_expenses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			account_id INTEGER,
			name TEXT NOT NULL,
			amount REAL NOT NULL,
			due_month INTEGER NOT NULL,
			due_day INTEGER NOT NULL,
			active BOOLEAN DEFAULT TRUE,
			last_reminded DATE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (account_id) REFERENCES accounts(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_yearly_expenses_user_id ON yearly_expenses(user_id)`,

		// --- Time log tables ---
		`CREATE TABLE IF NOT EXISTS time_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			logged_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_time_logs_user_id ON time_logs(user_id)`,
	}
	for _, s := range stmts {
		if _, err := d.conn.Exec(s); err != nil {
			n := len(s)
			if n > 40 {
				n = 40
			}
			return fmt.Errorf("stmt %q: %w", s[:n], err)
		}
	}
	return nil
}

// --- User ---

type User struct {
	ID              int64
	TelegramID      int64
	Username        string
	ActiveSessionID sql.NullInt64
}

func (d *DB) GetOrCreateUser(telegramID int64, username string) (*User, error) {
	u := &User{}
	err := d.conn.QueryRow(
		`SELECT id, telegram_id, username, active_session_id FROM users WHERE telegram_id = ?`,
		telegramID,
	).Scan(&u.ID, &u.TelegramID, &u.Username, &u.ActiveSessionID)
	if err == sql.ErrNoRows {
		res, err := d.conn.Exec(
			`INSERT INTO users (telegram_id, username) VALUES (?, ?)`,
			telegramID, username,
		)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		u = &User{ID: id, TelegramID: telegramID, Username: username}
	} else if err != nil {
		return nil, err
	}
	return u, nil
}

func (d *DB) SetActiveSession(userID, sessionID int64) error {
	_, err := d.conn.Exec(`UPDATE users SET active_session_id = ? WHERE id = ?`, sessionID, userID)
	return err
}

func (d *DB) GetUserByID(userID int64) (int64, error) {
	var telegramID int64
	err := d.conn.QueryRow(`SELECT telegram_id FROM users WHERE id = ?`, userID).Scan(&telegramID)
	return telegramID, err
}

func (d *DB) GetUserIDByTelegramID(telegramID int64) (int64, error) {
	var userID int64
	err := d.conn.QueryRow(`SELECT id FROM users WHERE telegram_id = ?`, telegramID).Scan(&userID)
	return userID, err
}

// --- Session ---

type Session struct {
	ID     int64
	UserID int64
	Name   string
}

func (d *DB) CreateSession(userID int64, name string) (*Session, error) {
	res, err := d.conn.Exec(
		`INSERT INTO sessions (user_id, name) VALUES (?, ?)`,
		userID, name,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Session{ID: id, UserID: userID, Name: name}, nil
}

func (d *DB) GetActiveSession(user *User) (*Session, error) {
	if !user.ActiveSessionID.Valid {
		return d.newSessionForUser(user)
	}
	s := &Session{}
	err := d.conn.QueryRow(
		`SELECT id, user_id, name FROM sessions WHERE id = ?`,
		user.ActiveSessionID.Int64,
	).Scan(&s.ID, &s.UserID, &s.Name)
	if err == sql.ErrNoRows {
		return d.newSessionForUser(user)
	}
	return s, err
}

func (d *DB) newSessionForUser(user *User) (*Session, error) {
	s, err := d.CreateSession(user.ID, fmt.Sprintf("Session %d", time.Now().Unix()))
	if err != nil {
		return nil, err
	}
	if err := d.SetActiveSession(user.ID, s.ID); err != nil {
		return nil, err
	}
	user.ActiveSessionID = sql.NullInt64{Int64: s.ID, Valid: true}
	return s, nil
}

// --- Conversation ---

type Message struct {
	Role    string
	Content string
}

func (d *DB) AddMessage(sessionID int64, role, content string) error {
	_, err := d.conn.Exec(
		`INSERT INTO conversations (session_id, role, content) VALUES (?, ?, ?)`,
		sessionID, role, content,
	)
	return err
}

func (d *DB) GetRecentMessages(sessionID int64, limit int) ([]Message, error) {
	rows, err := d.conn.Query(
		`SELECT role, content FROM (
			SELECT role, content, created_at FROM conversations WHERE session_id = ?
			ORDER BY created_at DESC LIMIT ?
		) ORDER BY created_at ASC`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// --- Task ---

type Task struct {
	ID        int64
	Content   string
	Completed bool
	CreatedAt time.Time
}

func (d *DB) AddTask(userID int64, content string) (int64, error) {
	res, err := d.conn.Exec(
		`INSERT INTO tasks (user_id, content) VALUES (?, ?)`,
		userID, content,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListTasks(userID int64) ([]Task, error) {
	rows, err := d.conn.Query(
		`SELECT id, content, completed, created_at FROM tasks WHERE user_id = ? AND completed = FALSE ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		var t Task
		var ts string
		if err := rows.Scan(&t.ID, &t.Content, &t.Completed, &ts); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (d *DB) CompleteTask(userID, taskID int64) error {
	res, err := d.conn.Exec(
		`UPDATE tasks SET completed = TRUE WHERE id = ? AND user_id = ?`,
		taskID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %d not found", taskID)
	}
	return nil
}

func (d *DB) DeleteTask(userID, taskID int64) error {
	res, err := d.conn.Exec(`DELETE FROM tasks WHERE id = ? AND user_id = ?`, taskID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %d not found", taskID)
	}
	return nil
}

func (d *DB) CompleteAllTasks(userID int64) (int64, error) {
	res, err := d.conn.Exec(
		`UPDATE tasks SET completed = TRUE WHERE user_id = ? AND completed = FALSE`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// --- Reminder ---

type Reminder struct {
	ID        int64
	UserID    int64
	Content   string
	RemindAt  time.Time
	Completed bool
}

func (d *DB) AddReminder(userID int64, content string, remindAt time.Time) (int64, error) {
	res, err := d.conn.Exec(
		`INSERT INTO reminders (user_id, content, remind_at) VALUES (?, ?, ?)`,
		userID, content, remindAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListReminders(userID int64) ([]Reminder, error) {
	rows, err := d.conn.Query(
		`SELECT id, user_id, content, remind_at FROM reminders WHERE user_id = ? AND completed = FALSE ORDER BY remind_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReminders(rows)
}

func (d *DB) GetDueReminders() ([]Reminder, error) {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	rows, err := d.conn.Query(
		`SELECT id, user_id, content, remind_at FROM reminders WHERE completed = FALSE AND remind_at <= ?`,
		now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReminders(rows)
}

func scanReminders(rows *sql.Rows) ([]Reminder, error) {
	var reminders []Reminder
	for rows.Next() {
		var r Reminder
		var ts string
		if err := rows.Scan(&r.ID, &r.UserID, &r.Content, &ts); err != nil {
			return nil, err
		}
		r.RemindAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		reminders = append(reminders, r)
	}
	return reminders, rows.Err()
}

func (d *DB) CompleteReminder(id int64) error {
	_, err := d.conn.Exec(`UPDATE reminders SET completed = TRUE WHERE id = ?`, id)
	return err
}

// --- Recurring Reminder ---

type RecurringReminder struct {
	ID            int64
	UserID        int64
	CronExpr      string
	Content       string
	Active        bool
	LastTriggered sql.NullString
}

func (d *DB) AddRecurringReminder(userID int64, cronExpr, content string) (int64, error) {
	res, err := d.conn.Exec(
		`INSERT INTO recurring_reminders (user_id, cron_expr, content) VALUES (?, ?, ?)`,
		userID, cronExpr, content,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetActiveRecurringReminders() ([]RecurringReminder, error) {
	rows, err := d.conn.Query(
		`SELECT id, user_id, cron_expr, content, active, last_triggered FROM recurring_reminders WHERE active = TRUE`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rr []RecurringReminder
	for rows.Next() {
		var r RecurringReminder
		if err := rows.Scan(&r.ID, &r.UserID, &r.CronExpr, &r.Content, &r.Active, &r.LastTriggered); err != nil {
			return nil, err
		}
		rr = append(rr, r)
	}
	return rr, rows.Err()
}

func (d *DB) UpdateRecurringLastTriggered(id int64) error {
	_, err := d.conn.Exec(
		`UPDATE recurring_reminders SET last_triggered = ? WHERE id = ?`,
		time.Now().UTC().Format("2006-01-02 15:04:05"), id,
	)
	return err
}

// --- Goal ---

type Goal struct {
	ID       int64
	Category string
	Name     string
	Target   sql.NullString
	Context  sql.NullString
	Active   bool
}

func (d *DB) AddGoal(userID int64, category, name string) (int64, error) {
	res, err := d.conn.Exec(
		`INSERT INTO goals (user_id, category, name) VALUES (?, ?, ?)`,
		userID, category, name,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListGoals(userID int64) ([]Goal, error) {
	rows, err := d.conn.Query(
		`SELECT id, category, name, target, context, active FROM goals WHERE user_id = ? AND active = TRUE ORDER BY category, name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var goals []Goal
	for rows.Next() {
		var g Goal
		if err := rows.Scan(&g.ID, &g.Category, &g.Name, &g.Target, &g.Context, &g.Active); err != nil {
			return nil, err
		}
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

// --- Activity ---

type Activity struct {
	ID          int64
	Category    string
	Content     string
	Mood        sql.NullString
	Duration    sql.NullInt64
	CompletedAt time.Time
}

func (d *DB) AddActivity(userID int64, category, content string) (int64, error) {
	res, err := d.conn.Exec(
		`INSERT INTO activities (user_id, category, content) VALUES (?, ?, ?)`,
		userID, category, content,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListActivities(userID int64, limit int) ([]Activity, error) {
	rows, err := d.conn.Query(
		`SELECT id, category, content, mood, duration, completed_at FROM activities WHERE user_id = ? ORDER BY completed_at DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var acts []Activity
	for rows.Next() {
		var a Activity
		var ts string
		if err := rows.Scan(&a.ID, &a.Category, &a.Content, &a.Mood, &a.Duration, &ts); err != nil {
			return nil, err
		}
		a.CompletedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		acts = append(acts, a)
	}
	return acts, rows.Err()
}

// --- Note ---

type Note struct {
	ID        int64
	Title     sql.NullString
	Content   string
	CreatedAt time.Time
}

func (d *DB) AddNote(userID int64, title, content string) (int64, error) {
	var titleVal interface{}
	if title != "" {
		titleVal = title
	}
	res, err := d.conn.Exec(
		`INSERT INTO notes (user_id, title, content) VALUES (?, ?, ?)`,
		userID, titleVal, content,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListNotes(userID int64, limit int) ([]Note, error) {
	rows, err := d.conn.Query(
		`SELECT id, title, content, created_at FROM notes WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotes(rows)
}

func (d *DB) DeleteNote(userID, noteID int64) error {
	res, err := d.conn.Exec(`DELETE FROM notes WHERE id = ? AND user_id = ?`, noteID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("note %d not found", noteID)
	}
	return nil
}

func (d *DB) SearchNotes(userID int64, query string) ([]Note, error) {
	rows, err := d.conn.Query(
		`SELECT id, title, content, created_at FROM notes WHERE user_id = ? AND (title LIKE ? OR content LIKE ?) ORDER BY created_at DESC LIMIT 20`,
		userID, "%"+query+"%", "%"+query+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotes(rows)
}

func scanNotes(rows *sql.Rows) ([]Note, error) {
	var notes []Note
	for rows.Next() {
		var n Note
		var ts string
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &ts); err != nil {
			return nil, err
		}
		n.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// --- Account ---

type Account struct {
	ID              int64
	UserID          int64
	Name            string
	StartingBalance float64
	CreatedAt       time.Time
}

func (d *DB) UpsertAccount(userID int64, name string, balance float64) (*Account, error) {
	_, err := d.conn.Exec(
		`INSERT INTO accounts (user_id, name, starting_balance) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, name) DO UPDATE SET starting_balance = excluded.starting_balance`,
		userID, name, balance,
	)
	if err != nil {
		return nil, err
	}
	return d.GetAccountByName(userID, name)
}

func (d *DB) ListAccounts(userID int64) ([]Account, error) {
	rows, err := d.conn.Query(
		`SELECT id, user_id, name, starting_balance, created_at FROM accounts WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		var a Account
		var ts string
		if err := rows.Scan(&a.ID, &a.UserID, &a.Name, &a.StartingBalance, &ts); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (d *DB) GetAccountByName(userID int64, name string) (*Account, error) {
	a := &Account{}
	var ts string
	err := d.conn.QueryRow(
		`SELECT id, user_id, name, starting_balance, created_at FROM accounts WHERE user_id = ? AND name = ?`,
		userID, name,
	).Scan(&a.ID, &a.UserID, &a.Name, &a.StartingBalance, &ts)
	if err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
	return a, nil
}

// --- Transaction ---

type Transaction struct {
	ID           int64
	UserID       int64
	AccountID    sql.NullInt64
	Amount       float64
	Merchant     string
	Category     string
	TransactedAt time.Time
}

func (d *DB) AddTransaction(userID, accountID int64, amount float64, merchant, category string) (int64, error) {
	var accID interface{}
	if accountID != 0 {
		accID = accountID
	}
	if category == "" {
		category = "Expenses:General"
	}
	res, err := d.conn.Exec(
		`INSERT INTO transactions (user_id, account_id, amount, merchant, category) VALUES (?, ?, ?, ?, ?)`,
		userID, accID, amount, merchant, category,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetMonthSummary(userID int64, year, month int) (total float64, byCategory map[string]float64, err error) {
	start := fmt.Sprintf("%04d-%02d-01 00:00:00", year, month)
	end := fmt.Sprintf("%04d-%02d-01 00:00:00", year, month+1)
	if month == 12 {
		end = fmt.Sprintf("%04d-01-01 00:00:00", year+1)
	}

	rows, err := d.conn.Query(
		`SELECT category, SUM(amount) FROM transactions
		 WHERE user_id = ? AND transacted_at >= ? AND transacted_at < ?
		 GROUP BY category`,
		userID, start, end,
	)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	byCategory = make(map[string]float64)
	for rows.Next() {
		var cat string
		var amt float64
		if err := rows.Scan(&cat, &amt); err != nil {
			return 0, nil, err
		}
		byCategory[cat] = amt
		total += amt
	}
	return total, byCategory, rows.Err()
}

func (d *DB) GetAllUserIDs() ([]int64, error) {
	rows, err := d.conn.Query(`SELECT id FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (d *DB) GetWeekSummary(userID int64) (total float64, byCategory map[string]float64, err error) {
	now := time.Now()
	// Start of current week: Monday
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7 in ISO
	}
	monday := now.AddDate(0, 0, -(weekday - 1))
	start := monday.Format("2006-01-02") + " 00:00:00"
	end := now.Format("2006-01-02") + " 23:59:59"

	rows, err := d.conn.Query(
		`SELECT category, SUM(amount) FROM transactions
		 WHERE user_id = ? AND transacted_at >= ? AND transacted_at <= ?
		 GROUP BY category`,
		userID, start, end,
	)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	byCategory = make(map[string]float64)
	for rows.Next() {
		var cat string
		var amt float64
		if err := rows.Scan(&cat, &amt); err != nil {
			return 0, nil, err
		}
		byCategory[cat] = amt
		total += amt
	}
	return total, byCategory, rows.Err()
}

func (d *DB) GetAccountBalance(userID int64, accountName string) (startBalance, spentThisMonth float64, err error) {
	acc, err := d.GetAccountByName(userID, accountName)
	if err != nil {
		return 0, 0, fmt.Errorf("account %q not found", accountName)
	}
	startBalance = acc.StartingBalance

	now := time.Now()
	start := fmt.Sprintf("%04d-%02d-01 00:00:00", now.Year(), int(now.Month()))
	end := fmt.Sprintf("%04d-%02d-01 00:00:00", now.Year(), int(now.Month())+1)
	if now.Month() == 12 {
		end = fmt.Sprintf("%04d-01-01 00:00:00", now.Year()+1)
	}

	err = d.conn.QueryRow(
		`SELECT COALESCE(SUM(t.amount), 0) FROM transactions t
		 WHERE t.user_id = ? AND t.account_id = ? AND t.transacted_at >= ? AND t.transacted_at < ?`,
		userID, acc.ID, start, end,
	).Scan(&spentThisMonth)
	return startBalance, spentThisMonth, err
}

// --- SIP ---

type SIP struct {
	ID          int64
	UserID      int64
	AccountID   sql.NullInt64
	AccountName string
	Name        string
	Amount      float64
	DebitDay    int
	Active      bool
	LastAlerted sql.NullString
}

func (d *DB) AddSIP(userID, accountID int64, name string, amount float64, debitDay int) (int64, error) {
	var accID interface{}
	if accountID != 0 {
		accID = accountID
	}
	res, err := d.conn.Exec(
		`INSERT INTO sips (user_id, account_id, name, amount, debit_day) VALUES (?, ?, ?, ?, ?)`,
		userID, accID, name, amount, debitDay,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListSIPs(userID int64) ([]SIP, error) {
	rows, err := d.conn.Query(
		`SELECT s.id, s.user_id, s.account_id, COALESCE(a.name,''), s.name, s.amount, s.debit_day, s.active, s.last_alerted
		 FROM sips s LEFT JOIN accounts a ON s.account_id = a.id
		 WHERE s.user_id = ? AND s.active = TRUE ORDER BY s.debit_day`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSIPs(rows)
}

func (d *DB) GetSIPsDueForAlert() ([]SIP, error) {
	tomorrow := time.Now().AddDate(0, 0, 1).Day()
	today := time.Now().Format("2006-01-02")
	rows, err := d.conn.Query(
		`SELECT s.id, s.user_id, s.account_id, COALESCE(a.name,''), s.name, s.amount, s.debit_day, s.active, s.last_alerted
		 FROM sips s LEFT JOIN accounts a ON s.account_id = a.id
		 WHERE s.active = TRUE AND s.debit_day = ? AND (s.last_alerted IS NULL OR s.last_alerted != ?)`,
		tomorrow, today,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSIPs(rows)
}

func scanSIPs(rows *sql.Rows) ([]SIP, error) {
	var sips []SIP
	for rows.Next() {
		var s SIP
		if err := rows.Scan(&s.ID, &s.UserID, &s.AccountID, &s.AccountName, &s.Name, &s.Amount, &s.DebitDay, &s.Active, &s.LastAlerted); err != nil {
			return nil, err
		}
		sips = append(sips, s)
	}
	return sips, rows.Err()
}

func (d *DB) MarkSIPAlerted(id int64) error {
	_, err := d.conn.Exec(
		`UPDATE sips SET last_alerted = ? WHERE id = ?`,
		time.Now().Format("2006-01-02"), id,
	)
	return err
}

// --- Yearly Expense ---

type YearlyExpense struct {
	ID           int64
	UserID       int64
	AccountID    sql.NullInt64
	AccountName  string
	Name         string
	Amount       float64
	DueMonth     int
	DueDay       int
	Active       bool
	LastReminded sql.NullString
}

func (d *DB) AddYearlyExpense(userID, accountID int64, name string, amount float64, dueMonth, dueDay int) (int64, error) {
	var accID interface{}
	if accountID != 0 {
		accID = accountID
	}
	res, err := d.conn.Exec(
		`INSERT INTO yearly_expenses (user_id, account_id, name, amount, due_month, due_day) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, accID, name, amount, dueMonth, dueDay,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListYearlyExpenses(userID int64) ([]YearlyExpense, error) {
	rows, err := d.conn.Query(
		`SELECT ye.id, ye.user_id, ye.account_id, COALESCE(a.name,''), ye.name, ye.amount, ye.due_month, ye.due_day, ye.active, ye.last_reminded
		 FROM yearly_expenses ye LEFT JOIN accounts a ON ye.account_id = a.id
		 WHERE ye.user_id = ? AND ye.active = TRUE ORDER BY ye.due_month, ye.due_day`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanYearlyExpenses(rows)
}

func (d *DB) GetYearlyExpensesDueForAlert() ([]YearlyExpense, error) {
	now := time.Now()
	thisYear := now.Format("2006")
	// return yearly expenses due within 7 days that haven't been reminded this year
	rows, err := d.conn.Query(
		`SELECT ye.id, ye.user_id, ye.account_id, COALESCE(a.name,''), ye.name, ye.amount, ye.due_month, ye.due_day, ye.active, ye.last_reminded
		 FROM yearly_expenses ye LEFT JOIN accounts a ON ye.account_id = a.id
		 WHERE ye.active = TRUE
		   AND (ye.last_reminded IS NULL OR ye.last_reminded < ?)`,
		thisYear+"-01-01",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanYearlyExpenses(rows)
	if err != nil {
		return nil, err
	}

	// Filter: due within next 7 days
	var due []YearlyExpense
	for _, ye := range all {
		dueDate := time.Date(now.Year(), time.Month(ye.DueMonth), ye.DueDay, 0, 0, 0, 0, now.Location())
		daysUntil := int(dueDate.Sub(now).Hours() / 24)
		if daysUntil >= 0 && daysUntil <= 7 {
			due = append(due, ye)
		}
	}
	return due, nil
}

func scanYearlyExpenses(rows *sql.Rows) ([]YearlyExpense, error) {
	var yes []YearlyExpense
	for rows.Next() {
		var ye YearlyExpense
		if err := rows.Scan(&ye.ID, &ye.UserID, &ye.AccountID, &ye.AccountName, &ye.Name, &ye.Amount, &ye.DueMonth, &ye.DueDay, &ye.Active, &ye.LastReminded); err != nil {
			return nil, err
		}
		yes = append(yes, ye)
	}
	return yes, rows.Err()
}

func (d *DB) MarkYearlyExpenseReminded(id int64) error {
	_, err := d.conn.Exec(
		`UPDATE yearly_expenses SET last_reminded = ? WHERE id = ?`,
		time.Now().Format("2006-01-02"), id,
	)
	return err
}

// --- Time Log ---

type TimeLog struct {
	ID       int64
	UserID   int64
	Content  string
	LoggedAt time.Time
}

func (d *DB) AddTimeLog(userID int64, content string) (int64, error) {
	res, err := d.conn.Exec(
		`INSERT INTO time_logs (user_id, content) VALUES (?, ?)`,
		userID, content,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListTimeLogs(userID int64, limit int) ([]TimeLog, error) {
	rows, err := d.conn.Query(
		`SELECT id, user_id, content, logged_at FROM time_logs WHERE user_id = ? ORDER BY logged_at DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTimeLogs(rows)
}

func (d *DB) ListTimeLogsForDay(userID int64, date time.Time) ([]TimeLog, error) {
	start := date.Format("2006-01-02") + " 00:00:00"
	end := date.Format("2006-01-02") + " 23:59:59"
	rows, err := d.conn.Query(
		`SELECT id, user_id, content, logged_at FROM time_logs WHERE user_id = ? AND logged_at >= ? AND logged_at <= ? ORDER BY logged_at ASC`,
		userID, start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTimeLogs(rows)
}

func scanTimeLogs(rows *sql.Rows) ([]TimeLog, error) {
	var logs []TimeLog
	for rows.Next() {
		var tl TimeLog
		var ts string
		if err := rows.Scan(&tl.ID, &tl.UserID, &tl.Content, &ts); err != nil {
			return nil, err
		}
		tl.LoggedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		logs = append(logs, tl)
	}
	return logs, rows.Err()
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
