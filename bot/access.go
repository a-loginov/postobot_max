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
	"Пока бот работает только для тех, кто прошёл отбор. " +
	"Нажми «Хочу в бета-тест», заполни анкету — и после проверки тебе придёт доступ."

// sendBetaGate shows the private beta offer to non-admitted users.
func (b *Bot) sendBetaGate(ctx context.Context, userID, chatID int64) {
	pending, err := b.database.HasPendingBetaApplication(ctx, userID)
	if err != nil {
		log.Printf("HasPendingBetaApplication: %v", err)
	}

	if pending {
		b.sendBetaPending(ctx, userID)
		return
	}

	kb := b.client.NewKeyboard()
	r := kb.AddRow()
	r.AddCallback("🔑 Хочу в бета-тест", schemes.DEFAULT, "beta:apply")

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

// sendBetaPending tells the user their application is under review.
func (b *Bot) sendBetaPending(ctx context.Context, userID int64) {
	b.setSession(userID, &session{state: stateIdle})
	b.reply(ctx, userID, 0,
		"📝 Твоя заявка на бета-тест уже на рассмотрении.\n"+
			"Как только проверим — пришлём уведомление.")
}

// applyBeta starts the beta application wizard.
func (b *Bot) applyBeta(ctx context.Context, userID int64) {
	pending, err := b.database.HasPendingBetaApplication(ctx, userID)
	if err != nil {
		log.Printf("HasPendingBetaApplication: %v", err)
	}
	if pending {
		b.sendBetaPending(ctx, userID)
		return
	}

	b.setSession(userID, &session{state: stateBetaName})
	b.reply(ctx, userID, 0,
		"🔑 Отлично! Заполни анкету бета-тестера.\n\nКак тебя зовут? (имя)")
}

func (b *Bot) inBetaWizard(userID int64) bool {
	switch b.getSession(userID).state {
	case stateBetaName, stateBetaSurname, stateBetaClass, stateBetaReason:
		return true
	}
	return false
}

// betaWizardMessage collects the beta application form fields.
func (b *Bot) betaWizardMessage(ctx context.Context, userID, chatID int64, text string) {
	if text == "Отмена" || text == "отмена" || text == "/cancel" {
		b.setSession(userID, &session{state: stateIdle})
		b.reply(ctx, userID, chatID, "Отменено.")
		return
	}

	if text == "" {
		b.reply(ctx, userID, chatID, "Напиши текстом, пожалуйста.")
		return
	}

	if b.moderation.Check(text).HasMat {
		b.reply(ctx, userID, chatID,
			"В сообщении нецензурные выражения. Перепиши, пожалуйста, без мата 🙏")
		return
	}

	s := b.getSession(userID)
	switch s.state {
	case stateBetaName:
		s.name = text
		s.state = stateBetaSurname
		b.reply(ctx, userID, chatID,
			fmt.Sprintf("Приятно познакомиться, %s! А какая у тебя фамилия?", s.name))

	case stateBetaSurname:
		s.surname = text
		s.state = stateBetaClass
		b.reply(ctx, userID, chatID, "Из какого ты класса? Например: 9А.")

	case stateBetaClass:
		s.class = text
		s.state = stateBetaReason
		b.reply(ctx, userID, chatID, "И последний вопрос: почему хочешь участвовать в бета-тесте?")

	case stateBetaReason:
		s.reason = text
		b.submitBetaApplication(ctx, userID, s)
	}
}

func (b *Bot) submitBetaApplication(ctx context.Context, userID int64, s *session) {
	if err := b.database.CreateBetaApplication(ctx, userID, s.name, s.surname, s.class, s.reason); err != nil {
		log.Printf("CreateBetaApplication: %v", err)
		b.reply(ctx, userID, 0, "Не удалось отправить заявку, попробуй ещё раз.")
		return
	}

	b.setSession(userID, &session{state: stateIdle})
	b.reply(ctx, userID, 0,
		"✅ Анкета отправлена!\n\n"+
			"Тебя заметят: твоё имя появится в анкетах на проверке. "+
			"Как только администратор одобрит заявку, тебе придёт уведомление и откроется меню.")
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
