package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"

	"adityanagar.com/ad-bot/internal/db"
)

type Scheduler struct {
	db            *db.DB
	bot           *tgbotapi.BotAPI
	timeLogChatID int64
}

func NewScheduler(database *db.DB, bot *tgbotapi.BotAPI, timeLogChatID int64) *Scheduler {
	return &Scheduler{db: database, bot: bot, timeLogChatID: timeLogChatID}
}

func (s *Scheduler) Start() {
	go s.runOneTimeTicker()
	go s.runCronScheduler()
	go s.runDailyFinanceAlerts()
	go s.runSummaryScheduler()
	go s.runTimeLogReminder()
}

func (s *Scheduler) runOneTimeTicker() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := s.checkDueReminders(); err != nil {
			log.Printf("reminder check error: %v", err)
		}
	}
}

func (s *Scheduler) checkDueReminders() error {
	reminders, err := s.db.GetDueReminders()
	if err != nil {
		return err
	}
	for _, r := range reminders {
		telegramID, err := s.db.GetUserByID(r.UserID)
		if err != nil {
			log.Printf("get user for reminder %d: %v", r.ID, err)
			continue
		}
		msg := tgbotapi.NewMessage(telegramID, fmt.Sprintf("Reminder: %s", r.Content))
		if _, err := s.bot.Send(msg); err != nil {
			log.Printf("send reminder %d: %v", r.ID, err)
			continue
		}
		if err := s.db.CompleteReminder(r.ID); err != nil {
			log.Printf("complete reminder %d: %v", r.ID, err)
		}
	}
	return nil
}

func (s *Scheduler) runCronScheduler() {
	c := cron.New()
	registered := map[int64]cron.EntryID{}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	reload := func() {
		rrs, err := s.db.GetActiveRecurringReminders()
		if err != nil {
			log.Printf("get recurring reminders: %v", err)
			return
		}
		for id, entryID := range registered {
			c.Remove(entryID)
			delete(registered, id)
		}
		for _, rr := range rrs {
			rr := rr
			entryID, err := c.AddFunc(rr.CronExpr, func() {
				telegramID, err := s.db.GetUserByID(rr.UserID)
				if err != nil {
					log.Printf("cron: get user %d: %v", rr.UserID, err)
					return
				}
				msg := tgbotapi.NewMessage(telegramID, fmt.Sprintf("Recurring reminder: %s", rr.Content))
				if _, err := s.bot.Send(msg); err != nil {
					log.Printf("cron: send reminder %d: %v", rr.ID, err)
					return
				}
				if err := s.db.UpdateRecurringLastTriggered(rr.ID); err != nil {
					log.Printf("cron: update last_triggered %d: %v", rr.ID, err)
				}
			})
			if err != nil {
				log.Printf("cron: invalid expr %q for recurring reminder %d: %v", rr.CronExpr, rr.ID, err)
				continue
			}
			registered[rr.ID] = entryID
		}
	}

	reload()
	c.Start()

	for range ticker.C {
		reload()
	}
}

func (s *Scheduler) runDailyFinanceAlerts() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	// Run once immediately on start, then every hour
	s.checkSIPAlerts()
	s.checkYearlyExpenseAlerts()
	for range ticker.C {
		s.checkSIPAlerts()
		s.checkYearlyExpenseAlerts()
	}
}

func (s *Scheduler) checkSIPAlerts() {
	sips, err := s.db.GetSIPsDueForAlert()
	if err != nil {
		log.Printf("sip alert check error: %v", err)
		return
	}
	for _, sip := range sips {
		telegramID, err := s.db.GetUserByID(sip.UserID)
		if err != nil {
			log.Printf("get user for sip %d: %v", sip.ID, err)
			continue
		}
		accountInfo := ""
		if sip.AccountName != "" {
			accountInfo = fmt.Sprintf(" from %s", sip.AccountName)
		}
		text := fmt.Sprintf("⏰ SIP Reminder: %s of ₹%.2f debits tomorrow%s.", sip.Name, sip.Amount, accountInfo)
		if _, err := s.bot.Send(tgbotapi.NewMessage(telegramID, text)); err != nil {
			log.Printf("send sip alert %d: %v", sip.ID, err)
			continue
		}
		if err := s.db.MarkSIPAlerted(sip.ID); err != nil {
			log.Printf("mark sip alerted %d: %v", sip.ID, err)
		}
	}
}

func (s *Scheduler) runSummaryScheduler() {
	c := cron.New()

	// Weekly summary: every Sunday at 9 PM
	c.AddFunc("0 21 * * 0", func() {
		s.sendWeeklySummaries()
	})

	// Monthly summary: every day at 9 PM — handler checks if today is the last day
	c.AddFunc("0 21 * * *", func() {
		now := time.Now()
		// tomorrow being the 1st means today is the last day of the month
		if now.AddDate(0, 0, 1).Day() == 1 {
			s.sendMonthlySummaries()
		}
	})

	c.Start()
	select {} // block forever
}

func (s *Scheduler) sendWeeklySummaries() {
	userIDs, err := s.db.GetAllUserIDs()
	if err != nil {
		log.Printf("weekly summary: get users: %v", err)
		return
	}
	now := time.Now()
	// Calculate Monday of current week for display
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := now.AddDate(0, 0, -(weekday - 1))

	for _, userID := range userIDs {
		total, byCategory, err := s.db.GetWeekSummary(userID)
		if err != nil {
			log.Printf("weekly summary: get summary for user %d: %v", userID, err)
			continue
		}
		if total == 0 {
			continue
		}
		telegramID, err := s.db.GetUserByID(userID)
		if err != nil {
			log.Printf("weekly summary: get telegram id for user %d: %v", userID, err)
			continue
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📊 Weekly Summary (%s – %s)\n", monday.Format("Jan 2"), now.Format("Jan 2")))
		sb.WriteString(fmt.Sprintf("Total spent: ₹%.2f\n\n", total))
		for cat, amt := range byCategory {
			sb.WriteString(fmt.Sprintf("  %s: ₹%.2f\n", cat, amt))
		}
		s.bot.Send(tgbotapi.NewMessage(telegramID, strings.TrimSpace(sb.String())))
	}
}

func (s *Scheduler) sendMonthlySummaries() {
	userIDs, err := s.db.GetAllUserIDs()
	if err != nil {
		log.Printf("monthly summary: get users: %v", err)
		return
	}
	now := time.Now()

	for _, userID := range userIDs {
		total, byCategory, err := s.db.GetMonthSummary(userID, now.Year(), int(now.Month()))
		if err != nil {
			log.Printf("monthly summary: get summary for user %d: %v", userID, err)
			continue
		}
		if total == 0 {
			continue
		}
		telegramID, err := s.db.GetUserByID(userID)
		if err != nil {
			log.Printf("monthly summary: get telegram id for user %d: %v", userID, err)
			continue
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📅 Monthly Summary — %s %d\n", now.Month().String(), now.Year()))
		sb.WriteString(fmt.Sprintf("Total spent: ₹%.2f\n\n", total))
		for cat, amt := range byCategory {
			sb.WriteString(fmt.Sprintf("  %s: ₹%.2f\n", cat, amt))
		}
		s.bot.Send(tgbotapi.NewMessage(telegramID, strings.TrimSpace(sb.String())))
	}
}

func (s *Scheduler) runTimeLogReminder() {
	if s.timeLogChatID == 0 {
		return
	}
	c := cron.New()
	// Hourly prompt: X:50 from 9 AM through 8 PM (9:50, 10:50, ..., 20:50)
	c.AddFunc("50 9-20 * * *", func() {
		now := time.Now()
		text := fmt.Sprintf("🕐 %d:%02d — What did you work on in the last hour?", now.Hour(), now.Minute())
		s.bot.Send(tgbotapi.NewMessage(s.timeLogChatID, text))
	})
	// EOD summary at 9:00 PM
	c.AddFunc("0 21 * * *", func() {
		s.sendTimeLogEODSummary()
	})
	c.Start()
	select {}
}

func (s *Scheduler) sendTimeLogEODSummary() {
	userID, err := s.db.GetUserIDByTelegramID(s.timeLogChatID)
	if err != nil {
		log.Printf("time log EOD: get user for chat %d: %v", s.timeLogChatID, err)
		return
	}
	logs, err := s.db.ListTimeLogsForDay(userID, time.Now())
	if err != nil {
		log.Printf("time log EOD: list logs: %v", err)
		return
	}
	if len(logs) == 0 {
		s.bot.Send(tgbotapi.NewMessage(s.timeLogChatID, "📋 No time logs recorded today."))
		return
	}
	var sb strings.Builder
	sb.WriteString("📋 EOD Summary — here's what you logged today:\n")
	for i, tl := range logs {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, tl.LoggedAt.Format("15:04"), tl.Content))
	}
	s.bot.Send(tgbotapi.NewMessage(s.timeLogChatID, strings.TrimSpace(sb.String())))
}

func (s *Scheduler) checkYearlyExpenseAlerts() {
	yes, err := s.db.GetYearlyExpensesDueForAlert()
	if err != nil {
		log.Printf("yearly expense alert check error: %v", err)
		return
	}
	now := time.Now()
	for _, ye := range yes {
		telegramID, err := s.db.GetUserByID(ye.UserID)
		if err != nil {
			log.Printf("get user for yearly expense %d: %v", ye.ID, err)
			continue
		}
		dueDate := time.Date(now.Year(), time.Month(ye.DueMonth), ye.DueDay, 0, 0, 0, 0, now.Location())
		daysUntil := int(dueDate.Sub(now).Hours() / 24)
		text := fmt.Sprintf("📅 Upcoming: %s of ₹%.2f is due in %d day(s).", ye.Name, ye.Amount, daysUntil)
		if _, err := s.bot.Send(tgbotapi.NewMessage(telegramID, text)); err != nil {
			log.Printf("send yearly expense alert %d: %v", ye.ID, err)
			continue
		}
		if err := s.db.MarkYearlyExpenseReminded(ye.ID); err != nil {
			log.Printf("mark yearly expense reminded %d: %v", ye.ID, err)
		}
	}
}
