package bot

import (
	"context"
	"fmt"
	"log"

	"github.com/max-messenger/max-bot-api-client-go/schemes"

	"first-max-bot/config"
	"first-max-bot/db"
)

const betaGateText = "ПостоБот сейчас в стадии бета-тестирования 👀\n\n" +
	"Пока бот работает только для тех, кто подал заявку на бета-тест. " +
	"Нажми «Хочу в бета-тест», чтобы получить доступ."

// sendBetaGate shows the private beta offer to non-admitted users.
func (b *Bot) sendBetaGate(ctx context.Context, userID, chatID int64) {
	kb := b.client.NewKeyboard()
	kb.AddRow().AddCallback("🔑 Хочу в бета-тест", schemes.DEFAULT, "beta:apply")

	m := b.client.NewMessage().SetText(betaGateText).AddKeyboard(kb)
	if chatID != 0 {
		m.SetChat(chatID)
	} else {
		m.SetUser(userID)
	}
	if err := b.client.Send(ctx, m); err != nil {
		log.Printf("sendBetaGate to %d: %v", userID, err)
	}
}

// applyBeta admits a user to the private beta and opens the main menu.
func (b *Bot) applyBeta(ctx context.Context, userID int64) {
	// Make sure the student row exists for the tester list.
	if _, err := b.database.FindOrCreateStudent(ctx, userID); err != nil {
		log.Printf("applyBeta FindOrCreateStudent: %v", err)
	}
	if err := b.database.AddBetaTester(ctx, userID); err != nil {
		log.Printf("AddBetaTester: %v", err)
		b.reply(ctx, userID, 0, "Не получилось оформить заявку, попробуй ещё раз.")
		return
	}

	b.setSession(userID, &session{state: stateIdle})
	b.reply(ctx, userID, 0,
		"Ты в бета-тесте! 🎉 Твое имя появится в стенах тестировщиков школы, "+
			"а в профиль будет добавлен значок тестировщика.")
	b.sendMenu(ctx, userID)
}

func stageLabel(stage string) string {
	switch stage {
	case config.StageBeta:
		return "🔒 Бета-тестирование"
	case config.StagePublicBeta:
		return "🌍 Публичный бета-тест"
	case config.StageFinal:
		return "🚀 Финальный выход (24/7)"
	default:
		return stage
	}
}

func (b *Bot) changeStage(ctx context.Context, userID int64, stage string) {
	if !config.ValidStage(stage) {
		return
	}
	if err := b.database.SetSetting(ctx, db.SettingStage, stage); err != nil {
		log.Printf("SetSetting stage: %v", err)
		b.reply(ctx, userID, 0, "Не удалось сменить стадию.")
		return
	}
	b.reply(ctx, userID, 0, fmt.Sprintf("Стадия запуска: %s", stageLabel(stage)))
	b.sendAdminPanel(ctx, userID)
}
