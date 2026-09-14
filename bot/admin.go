package bot

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/max-messenger/max-bot-api-client-go/schemes"

	"first-max-bot/config"
	"first-max-bot/db"
)

// handleAdminMessage processes text messages from an admin (password prompt).
// Returns true when the message was fully handled as an admin action.
func (b *Bot) handleAdminMessage(ctx context.Context, userID, chatID int64, text string) bool {
	if b.isAdminAuth(userID) {
		if text == "/admin" || text == "/start" {
			b.sendAdminPanel(ctx, userID)
		}
		return true
	}

	if b.hasAdminPending(userID) {
		b.clearAdminPending(userID)
		if text == b.config.AdminPassword {
			b.setAdminAuth(userID)
			b.sendAdminPanel(ctx, userID)
		} else {
			b.reply(ctx, userID, chatID, "Неверный пароль. Для повторной попытки отправь /admin")
		}
		return true
	}

	if text == "/admin" {
		b.setAdminPending(userID)
		b.reply(ctx, userID, chatID, "Введи пароль администратора:")
		return true
	}

	return false
}

// handleAdminCallback routes callback presses inside the admin panel.
// Returns true when the callback was consumed by the panel.
func (b *Bot) handleAdminCallback(ctx context.Context, userID int64, callbackID, payload string) bool {
	action, arg, _ := parsePayload(payload)
	if action != "adm" || !b.isAdminAuth(userID) {
		return false
	}

	switch arg {
	case "panel":
		b.sendAdminPanel(ctx, userID)

	case "stage":
		b.sendStagePicker(ctx, userID)

	case "stage:beta", "stage:public_beta", "stage:final":
		b.answerCallback(ctx, callbackID, "Стадия обновлена ✔")
		b.changeStage(ctx, userID, strings.TrimPrefix(arg, "stage:"))

	case "testers":
		b.answerCallback(ctx, callbackID, "Список тестировщиков")
		b.listTesters(ctx, userID)

	case "stats":
		b.answerCallback(ctx, callbackID, "Статистика")
		b.sendStats(ctx, userID)

	case "close":
		b.clearAdminAuth(userID)
		b.answerCallback(ctx, callbackID, "Выход из панели администратора.")

	default:
		return false
	}

	return true
}

func (b *Bot) sendAdminPanel(ctx context.Context, userID int64) {
	stage, err := b.database.GetSetting(ctx, db.SettingStage, b.config.DefaultStage)
	if err != nil {
		log.Printf("GetSetting stage: %v", err)
		stage = b.config.DefaultStage
	}

	kb := b.client.NewKeyboard()
	r1 := kb.AddRow()
	r1.AddCallback(fmt.Sprintf("🚀 Стадия: %s", stageLabel(stage)), schemes.DEFAULT, "adm:stage")

	r2 := kb.AddRow()
	r2.AddCallback("🧪 Тестировщики", schemes.DEFAULT, "adm:testers")
	r2.AddCallback("📊 Статистика", schemes.DEFAULT, "adm:stats")

	r3 := kb.AddRow()
	r3.AddCallback("🔚 Выйти", schemes.DEFAULT, "adm:close")

	b.replyKeyboard(ctx, userID, 0, "👨‍💻 Панель администратора", kb)
}

func (b *Bot) sendStagePicker(ctx context.Context, userID int64) {
	kb := b.client.NewKeyboard()
	r1 := kb.AddRow()
	r1.AddCallback(stageLabel(config.StageBeta), schemes.DEFAULT, "adm:stage:beta")
	r2 := kb.AddRow()
	r2.AddCallback(stageLabel(config.StagePublicBeta), schemes.DEFAULT, "adm:stage:public_beta")
	r3 := kb.AddRow()
	r3.AddCallback(stageLabel(config.StageFinal), schemes.DEFAULT, "adm:stage:final")
	r4 := kb.AddRow()
	r4.AddCallback("🔙 Назад", schemes.DEFAULT, "adm:panel")

	b.replyKeyboard(ctx, userID, 0, "Текущая стадия: "+stageLabel(b.currentStage(ctx))+"\nВыбери новую:", kb)
}

func (b *Bot) listTesters(ctx context.Context, userID int64) {
	testers, err := b.database.BetaTesters(ctx)
	if err != nil {
		log.Printf("BetaTesters: %v", err)
		return
	}

	if len(testers) == 0 {
		b.reply(ctx, userID, 0, "Тестировщиков пока нет.")
		return
	}

	msg := fmt.Sprintf("🧪 Тестировщики (%d):\n", len(testers))
	for _, t := range testers {
		msg += fmt.Sprintf("• id=%d (с %s)\n", t.MaxUserID, t.AddedAt.Format("02.01.2006"))
	}
	b.reply(ctx, userID, 0, msg)
}

func (b *Bot) sendStats(ctx context.Context, userID int64) {
	s, err := b.database.Stats(ctx)
	if err != nil {
		log.Printf("Stats: %v", err)
		return
	}

	b.reply(ctx, userID, 0, fmt.Sprintf(
		"📊 Статистика:\n"+
			"Ученики: %d\n"+
			"Заявки (всего): %d\n"+
			"— в работе: %d\n"+
			"— выполнено: %d\n"+
			"— отклонено: %d\n"+
			"Тестировщики: %d",
		s.Students, s.Requests, s.ActiveRequests, s.DoneRequests, s.RejectedRequests, s.BetaTesters))
}

// --- admin auth state ---

func (b *Bot) setAdminPending(userID int64) {
	b.adminMu.Lock()
	b.adminPending[userID] = true
	b.adminMu.Unlock()
}

func (b *Bot) hasAdminPending(userID int64) bool {
	b.adminMu.Lock()
	defer b.adminMu.Unlock()
	return b.adminPending[userID]
}

func (b *Bot) clearAdminPending(userID int64) {
	b.adminMu.Lock()
	delete(b.adminPending, userID)
	b.adminMu.Unlock()
}

func (b *Bot) setAdminAuth(userID int64) {
	b.adminMu.Lock()
	b.adminAuth[userID] = true
	b.adminMu.Unlock()
}

func (b *Bot) isAdminAuth(userID int64) bool {
	b.adminMu.Lock()
	defer b.adminMu.Unlock()
	return b.adminAuth[userID]
}

func (b *Bot) clearAdminAuth(userID int64) {
	b.adminMu.Lock()
	delete(b.adminAuth, userID)
	b.adminMu.Unlock()
}
