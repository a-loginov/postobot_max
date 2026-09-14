package bot

import (
	"context"
	"fmt"
	"log"
	"time"

	"first-max-bot/archive"
	"first-max-bot/db"
)

func (b *Bot) archiveQuery(ctx context.Context, userID int64, student *db.Student, text string) {
	hint := monthHint(text)
	if hint == "" {
		b.reply(ctx, userID, 0,
			"Не понял запрос. Напиши месяц, например: «покажи что я просил в августе 25».")
		return
	}

	month, year := archive.ParseMonth(text, time.Now())

	entries, err := archive.ListForUser(b.config.ArchiveDir, userID, year, month)
	if err != nil {
		log.Printf("archive list %s/%d: %v", month, year, err)
		b.reply(ctx, userID, 0,
			fmt.Sprintf("За %s %d записей в архиве нет.", monthName(month), year))
		return
	}

	if len(entries) == 0 {
		b.reply(ctx, userID, 0,
			fmt.Sprintf("В %s %d заявок от тебя не нашлось.", monthName(month), year))
		return
	}

	msg := fmt.Sprintf("Твои заявки за %s %d:\n\n", monthName(month), year)
	for _, e := range entries {
		msg += fmt.Sprintf("#%d • %s • %s\n", e.RequestID, e.CreatedAt[:10], e.Description)
	}
	b.reply(ctx, userID, 0, msg)
}

func monthHint(text string) string {
	return archive.MonthHint(text)
}

func monthName(m time.Month) string {
	names := []string{"", "январь", "февраль", "март", "апрель", "май", "июнь",
		"июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}
	if int(m) < 1 || int(m) > 12 {
		return ""
	}
	return names[int(m)]
}
