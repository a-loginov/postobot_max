package bot

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/max-messenger/max-bot-api-client-go/schemes"

	"first-max-bot/config"
	"first-max-bot/db"
	"first-max-bot/services/maxbot"
	"first-max-bot/services/moderation"
)

type sessionState string

const (
	stateIdle                sessionState = "idle"
	stateAwaitingDescription sessionState = "awaiting_description"
	stateAwaitingName        sessionState = "awaiting_name"
	stateAwaitingSurname     sessionState = "awaiting_surname"
	stateAwaitingClass       sessionState = "awaiting_class"
	stateAwaitingPhoto       sessionState = "awaiting_photo"
	stateAwaitingArchive     sessionState = "awaiting_archive"

	stateBetaName    sessionState = "beta_name"
	stateBetaSurname sessionState = "beta_surname"
	stateBetaClass   sessionState = "beta_class"
	stateBetaReason  sessionState = "beta_reason"
)

type session struct {
	state       sessionState
	description string
	name        string
	surname     string
	class       string
	reason      string
}

type Bot struct {
	client     *maxbot.Client
	database   *db.DB
	config     *config.Config
	moderation *moderation.Moderation

	mu       sync.Mutex
	sessions map[int64]*session

	adminMu      sync.Mutex
	adminPending map[int64]bool
	adminAuth    map[int64]bool
}

func New(client *maxbot.Client, database *db.DB, cfg *config.Config) *Bot {
	return &Bot{
		client:       client,
		database:     database,
		config:       cfg,
		moderation:   moderation.New(cfg.MatWords),
		sessions:     make(map[int64]*session),
		adminPending: make(map[int64]bool),
		adminAuth:    make(map[int64]bool),
	}
}

const menuText = "ПостоБот — заявки на замену или ремонт оборудования в школе 1409.\n\n" +
	"Сообщи, что нужно заменить: бутылка воды, стул, монитор и всё остальное. " +
	"Мы передадим заявку ответственному и уведомим тебя, когда будет выполнено."

func (b *Bot) Run(ctx context.Context) {
	go func() {
		for err := range b.client.GetErrors() {
			log.Printf("[maxbot] error: %v", err)
		}
	}()

	for upd := range b.client.GetUpdates(ctx) {
		b.safe("update", func() {
			switch upd := upd.(type) {
			case *schemes.MessageCreatedUpdate:
				b.handleMessage(ctx, upd)
			case *schemes.MessageCallbackUpdate:
				b.handleCallback(ctx, upd)
			}
		})
	}
}

// safe runs fn and recovers panics so a single bad update never kills the bot.
func (b *Bot) safe(op string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic] %s: %v\n%s", op, r, debug.Stack())
		}
	}()
	fn()
}

// currentStage returns the active launch stage from DB settings.
func (b *Bot) currentStage(ctx context.Context) string {
	stage, err := b.database.GetSetting(ctx, db.SettingStage, b.config.DefaultStage)
	if err != nil {
		log.Printf("GetSetting stage: %v", err)
		return b.config.DefaultStage
	}
	if !config.ValidStage(stage) {
		return b.config.DefaultStage
	}
	return stage
}

func (b *Bot) isPrivileged(userID int64) bool {
	return b.isResponsible(userID) || b.isAdmin(userID)
}

func (b *Bot) isAdmin(userID int64) bool {
	for _, id := range b.config.AdminIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// checkAccess applies the launch-stage gate for regular students.
// It returns true when the user may use the bot.
func (b *Bot) checkAccess(ctx context.Context, userID int64) bool {
	if b.isPrivileged(userID) {
		return true
	}

	switch b.currentStage(ctx) {
	case config.StageBeta:
		ok, err := b.database.IsBetaTester(ctx, userID)
		if err != nil {
			log.Printf("IsBetaTester: %v", err)
		}
		return ok
	case config.StagePublicBeta, config.StageFinal:
		return true
	}
	return true
}

// handleMessage processes a plain incoming message (text or with photo).
func (b *Bot) handleMessage(ctx context.Context, u *schemes.MessageCreatedUpdate) {
	userID := u.GetUserID()
	chatID := u.GetChatID()
	text := u.Message.Body.Text
	photos := b.photoAttachments(u)

	if b.isAdmin(userID) {
		if b.handleAdminMessage(ctx, userID, chatID, text) {
			return
		}
	}

	if b.isResponsible(userID) {
		if text != "" && text[0] == '/' {
			b.adminCommand(ctx, userID, chatID, text)
		}
		return
	}

	if !b.checkAccess(ctx, userID) {
		if b.inBetaWizard(userID) {
			b.betaWizardMessage(ctx, userID, chatID, text)
			return
		}
		b.sendBetaGate(ctx, userID, chatID)
		return
	}

	b.studentMessage(ctx, userID, chatID, text, photos)
}

func (b *Bot) isResponsible(userID int64) bool {
	for _, id := range b.config.ResponsibleIDs {
		if id == userID {
			return true
		}
	}
	return false
}

func (b *Bot) handleCallback(ctx context.Context, u *schemes.MessageCallbackUpdate) {
	userID := u.GetUserID()
	payload := u.Callback.Payload
	callbackID := u.Callback.CallbackID

	action, arg, err := parsePayload(payload)
	if err != nil {
		return
	}

	// Admin panel callbacks first.
	if b.isAdmin(userID) && b.handleAdminCallback(ctx, userID, callbackID, payload) {
		return
	}

	// Callback for responsible person (done / reorder).
	if b.isResponsible(userID) {
		switch action {
		case "done", "up", "down":
			b.responsibleAction(ctx, userID, callbackID, action, arg)
		}
		return
	}

	// Beta application from the gate screen.
	if action == "beta" && arg == "apply" {
		b.applyBeta(ctx, userID)
		return
	}

	if !b.checkAccess(ctx, userID) {
		b.sendBetaGate(ctx, userID, 0)
		return
	}

	// Callbacks for students.
	switch action {
	case "new":
		b.setSession(userID, &session{state: stateAwaitingDescription})
		b.replyKeyboard(ctx, userID, 0,
			"Что нужно заменить или починить? Напиши текстом или выбери кнопку ниже 👇",
			b.quickButtons(
				"💧 Бутылка воды", "🪑 Стул", "🖥 Монитор",
				"💡 Лампочка", "🚰 Кран/раковина", "🚪 Дверь",
				"🪟 Окно", "🛋 Диван", "✏️ Другое…",
			))
	case "mylist":
		b.myRequests(ctx, userID)
	case "archive":
		b.setSession(userID, &session{state: stateAwaitingArchive})
		b.reply(ctx, userID, 0, "Напиши запрос, например: «покажи что я просил в августе 25».")
	}
}

// studentMessage drives the request wizard.
func (b *Bot) studentMessage(ctx context.Context, userID int64, chatID int64, text string, photos []photoInfo) {
	if text == "/start" {
		b.setSession(userID, &session{state: stateIdle})
		b.sendMenu(ctx, userID)
		return
	}

	if text == "Отмена" || text == "отмена" || text == "/cancel" {
		b.setSession(userID, &session{state: stateIdle})
		b.reply(ctx, userID, chatID, "Отменено.")
		return
	}

	// Unknown slash-commands must not be stored as a complaint.
	if text != "" && text[0] == '/' && text != "/start" {
		b.reply(ctx, userID, chatID, "Неизвестная команда или нет доступа. Напиши текстом, что нужно починить.")
		return
	}

	student, err := b.database.FindOrCreateStudent(ctx, userID)
	if err != nil {
		log.Printf("FindOrCreateStudent: %v", err)
		return
	}

	s := b.getSession(userID)

	// Profanity guard on every free-text input, no matter the wizard step.
	if text != "" && b.moderation.Check(text).HasMat {
		b.reply(ctx, userID, chatID,
			"В сообщении нецензурные выражения. Перепиши, пожалуйста, без мата 🙏")
		return
	}

	// Any free text from an idle student that mentions a month is treated as an archive query.
	if s.state == stateIdle && text != "" && monthHint(text) != "" {
		b.archiveQuery(ctx, userID, student, text)
		return
	}

	switch s.state {
	case stateIdle, stateAwaitingDescription:
		if text == "" {
			b.reply(ctx, userID, chatID,
				"Что нужно заменить или починить? Напиши текстом или выбери кнопку ниже 👇")
			return
		}
		s.description = text
		s.state = stateAwaitingName
		b.reply(ctx, userID, chatID, "Как тебя зовут? Напиши, пожалуйста, имя.")

	case stateAwaitingName:
		if text == "" {
			return
		}
		s.name = text
		s.state = stateAwaitingSurname
		b.reply(ctx, userID, chatID,
			fmt.Sprintf("Приятно познакомиться, %s! А какая у тебя фамилия?", s.name))

	case stateAwaitingSurname:
		if text == "" {
			return
		}
		s.surname = text
		s.state = stateAwaitingClass
		b.reply(ctx, userID, chatID, "Теперь класс. Например: 9А.")

	case stateAwaitingClass:
		if text == "" {
			return
		}
		s.class = text
		s.state = stateAwaitingPhoto
		b.reply(ctx, userID, chatID,
			"Отлично! Почти готово. Пришли фото того, что нужно заменить. Без фото заявку не примем 📸")

	case stateAwaitingPhoto:
		if len(photos) == 0 {
			b.reply(ctx, userID, chatID, "Фото обязательно. Пришли фотографию проблемного предмета.")
			return
		}
		if photos[0].Token == "" && photos[0].Url == "" {
			b.reply(ctx, userID, chatID, "Не удалось получить фото. Пришли фотографию ещё раз.")
			return
		}
		b.submitRequest(ctx, student, s, photos[0])

	case stateAwaitingArchive:
		b.archiveQuery(ctx, userID, student, text)
	}
}

func (b *Bot) submitRequest(ctx context.Context, student *db.Student, s *session, photo photoInfo) {
	if err := b.database.UpdateStudentProfile(ctx, student.ID, s.name, s.surname, s.class); err != nil {
		log.Printf("UpdateStudentProfile: %v", err)
	}

	req := &db.Request{
		StudentID:   student.ID,
		Description: s.description,
		Normalized:  moderation.Normalize(s.description),
		PhotoToken:  photo.Token,
		PhotoURL:    photo.Url,
	}

	result := b.moderation.Check(s.description)

	// Auto-moderation: profanity and near-duplicates.
	if result.HasMat {
		req.Status = db.StatusRejected
		req.RejectReason = "мат"
		if err := b.database.CreateRequest(ctx, req); err != nil {
			log.Printf("CreateRequest rejected: %v", err)
		}
		b.reply(ctx, student.MaxUserID, 0, "Заявка отклонена: сообщение содержит недопустимые выражения. Перепиши по-нормальному.")
		return
	}

	dup, err := b.findDuplicate(ctx, student, req)
	if err != nil {
		log.Printf("findDuplicate: %v", err)
	}
	if dup != nil {
		req.Status = db.StatusRejected
		req.RejectReason = "дубликат"
		req.DuplicateOfID = &dup.ID
		if err := b.database.CreateRequest(ctx, req); err != nil {
			log.Printf("CreateRequest duplicate: %v", err)
		}
		b.reply(ctx, student.MaxUserID, 0, "Похожая заявка уже отправлена и принята в работу. Дубликат не нужен.")
		return
	}

	req.Status = db.StatusActive
	if err := b.database.CreateRequest(ctx, req); err != nil {
		log.Printf("CreateRequest: %v", err)
		b.reply(ctx, student.MaxUserID, 0, "Не удалось сохранить заявку, попробуй ещё раз.")
		return
	}

	b.reply(ctx, student.MaxUserID, 0,
		fmt.Sprintf("✅ Заявка №%d принята!\n👤 Кто запросил: %s %s (%s класс)\n🔧 Проблема: %s",
			req.ID, s.name, s.surname, s.class, s.description))

	b.notifyResponsible(ctx, req)

	b.setSession(student.MaxUserID, &session{state: stateIdle})
}

func (b *Bot) findDuplicate(ctx context.Context, student *db.Student, req *db.Request) (*db.Request, error) {
	requests, err := b.database.StudentRequests(ctx, student.ID)
	if err != nil {
		return nil, err
	}

	since := time.Now().AddDate(0, 0, -7)
	for i := range requests {
		r := &requests[i]
		if r.Status == db.StatusRejected || r.CreatedAt.Before(since) {
			continue
		}
		if b.moderation.IsSimilar(req.Normalized, r.Normalized) {
			return r, nil
		}
	}
	return nil, nil
}

// notifyResponsible sends the approved request to the school staff.
func (b *Bot) notifyResponsible(ctx context.Context, req *db.Request) {
	for _, id := range b.config.ResponsibleIDs {
		m := b.client.NewMessage()
		m.SetText(formatRequest(req))
		if req.PhotoToken != "" {
			m.AddPhotoByToken(req.PhotoToken)
		}
		m.AddKeyboard(b.requestButtons(req.ID))
		m.SetUser(id)
		if err := b.client.Send(ctx, m); err != nil {
			log.Printf("notify responsible %d: %v", id, err)
		}
	}
}

func (b *Bot) requestButtons(reqID uint) *maxbot.Keyboard {
	kb := b.client.NewKeyboard()
	row := kb.AddRow()
	row.AddCallback("✅ Выполнено", schemes.DEFAULT, fmt.Sprintf("done:%d", reqID))
	row.AddCallback("⬆️ Важнее", schemes.DEFAULT, fmt.Sprintf("up:%d", reqID))
	row.AddCallback("⬇️ Ниже", schemes.DEFAULT, fmt.Sprintf("down:%d", reqID))
	return kb
}

func formatRequest(r *db.Request) string {
	status := "в работе"
	if r.Status == db.StatusDone {
		status = "✅ выполнено"
	}
	photo := "нет"
	if r.PhotoURL != "" {
		photo = "приложено"
	}
	return fmt.Sprintf(
		"🧾 Заявка №%d\n👤 Кто запросил: %s %s (%s класс)\n🔧 Проблема: %s\n📸 Фото: %s\nСтатус: %s",
		r.ID, r.Student.Name, r.Student.Surname, r.Student.Class, r.Description, photo, status)
}

// adminCommand handles responsible person commands.
func (b *Bot) adminCommand(ctx context.Context, userID, chatID int64, text string) {
	switch text {
	case "/start", "/admin", "/list":
		b.sendResponsibleList(ctx, userID, chatID)
	}
}

func (b *Bot) sendResponsibleList(ctx context.Context, userID, chatID int64) {
	requests, err := b.database.ResponsibleList(ctx)
	if err != nil {
		log.Printf("ResponsibleList: %v", err)
		return
	}

	if len(requests) == 0 {
		b.reply(ctx, userID, chatID, "Заявок нет. Все свободны.")
		return
	}

	msg := "Единый список заявок (по важности):\n\n"
	for _, r := range requests {
		msg += fmt.Sprintf("#%d • приоритет %d • %s %s • %s\n",
			r.ID, r.Priority, r.Student.Name, r.Student.Surname, r.Description)
	}
	b.reply(ctx, userID, chatID, msg)
}

func (b *Bot) responsibleAction(ctx context.Context, _ int64, callbackID, action, arg string) {
	reqID := parseID(arg)
	if reqID == 0 {
		return
	}

	req, err := b.database.GetRequest(ctx, reqID)
	if err != nil {
		log.Printf("GetRequest(%d): %v", reqID, err)
		return
	}

	switch action {
	case "done":
		if req.Status == db.StatusActive {
			if err := b.database.UpdateStatus(ctx, req.ID, db.StatusDone, ""); err != nil {
				log.Printf("UpdateStatus done: %v", err)
				return
			}

			note := fmt.Sprintf("Заявка #%d выполнена ✅", req.ID)
			b.answerCallback(ctx, callbackID, note)

			// Notify the student.
			b.reply(ctx, req.Student.MaxUserID, 0,
				fmt.Sprintf("✅ Твоя заявка #%d выполнена: %s", req.ID, req.Description))
		}
		return

	case "up", "down":
		delta := -1
		if action == "up" {
			delta = 1
		}
		if err := b.database.BumpPriority(ctx, req.ID, delta); err != nil {
			log.Printf("BumpPriority: %v", err)
			return
		}
		b.answerCallback(ctx, callbackID,
			fmt.Sprintf("Приоритет заявки #%d: %d", req.ID, req.Priority+delta))
		return
	}
}

func (b *Bot) answerCallback(ctx context.Context, callbackID, notification string) {
	answer := &schemes.CallbackAnswer{Notification: notification}
	if err := b.client.AnswerOnCallback(ctx, callbackID, answer); err != nil {
		log.Printf("AnswerOnCallback: %v", err)
	}
}

func (b *Bot) myRequests(ctx context.Context, userID int64) {
	student, err := b.database.FindOrCreateStudent(ctx, userID)
	if err != nil {
		log.Printf("FindOrCreateStudent: %v", err)
		return
	}

	requests, err := b.database.StudentRequests(ctx, student.ID)
	if err != nil {
		log.Printf("StudentRequests: %v", err)
		return
	}

	if len(requests) == 0 {
		b.reply(ctx, userID, 0, "Заявок пока нет. Напиши, что нужно заменить.")
		return
	}

	msg := "Твои заявки:\n\n"
	for _, r := range requests {
		msg += fmt.Sprintf("#%d • %s • %s\n", r.ID, statusLabel(r.Status), r.Description)
	}
	b.reply(ctx, userID, 0, msg)
}

func statusLabel(s db.RequestStatus) string {
	switch s {
	case db.StatusActive:
		return "в работе"
	case db.StatusDone:
		return "✅ выполнено"
	case db.StatusRejected:
		return "⛔ отклонена"
	default:
		return string(s)
	}
}

func (b *Bot) sendMenu(ctx context.Context, userID int64) {
	kb := b.client.NewKeyboard()
	row := kb.AddRow()
	row.AddCallback("📝 Новая заявка", schemes.DEFAULT, "new")
	row.AddCallback("📋 Мои заявки", schemes.DEFAULT, "mylist")

	row2 := kb.AddRow()
	row2.AddCallback("🗄 Архив", schemes.DEFAULT, "archive")

	m := b.client.NewMessage().SetText(menuText).AddKeyboard(kb)
	m.SetUser(userID)
	if err := b.client.Send(ctx, m); err != nil {
		log.Printf("sendMenu: %v", err)
	}
}

// --- helpers ---

type photoInfo struct {
	Token string
	Url   string
}

func (b *Bot) photoAttachments(u *schemes.MessageCreatedUpdate) []photoInfo {
	var photos []photoInfo
	for _, att := range u.Message.Body.Attachments {
		if p, ok := att.(*schemes.PhotoAttachment); ok {
			photos = append(photos, photoInfo{Token: p.Payload.Token, Url: p.Payload.Url})
		}
	}
	return photos
}

func (b *Bot) reply(ctx context.Context, userID, chatID int64, text string) {
	m := b.client.NewMessage().SetText(text)
	if chatID != 0 {
		m.SetChat(chatID)
	} else {
		m.SetUser(userID)
	}
	if err := b.client.Send(ctx, m); err != nil {
		log.Printf("reply to %d: %v", userID, err)
	}
}

// replyKeyboard sends a text message with a keyboard attached.
func (b *Bot) replyKeyboard(ctx context.Context, userID, chatID int64, text string, kb *maxbot.Keyboard) {
	m := b.client.NewMessage().SetText(text).AddKeyboard(kb)
	if chatID != 0 {
		m.SetChat(chatID)
	} else {
		m.SetUser(userID)
	}
	if err := b.client.Send(ctx, m); err != nil {
		log.Printf("replyKeyboard to %d: %v", userID, err)
	}
}

// quickButtons builds a keyboard of message-type buttons (up to 3 per row):
// tapping one sends its text as a message to the bot.
func (b *Bot) quickButtons(labels ...string) *maxbot.Keyboard {
	kb := b.client.NewKeyboard()
	var row = kb.AddRow()
	perRow := 3
	for i, label := range labels {
		if i > 0 && i%perRow == 0 {
			row = kb.AddRow()
		}
		row.AddMessage(label)
	}
	return kb
}

func (b *Bot) getSession(userID int64) *session {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.sessions[userID]
	if !ok || s == nil {
		s = &session{state: stateIdle}
		b.sessions[userID] = s
	}
	return s
}

func (b *Bot) setSession(userID int64, s *session) {
	b.mu.Lock()
	b.sessions[userID] = s
	b.mu.Unlock()
}

func parsePayload(payload string) (action, arg string, err error) {
	for i := 0; i < len(payload); i++ {
		if payload[i] == ':' {
			return payload[:i], payload[i+1:], nil
		}
	}
	return payload, "", nil
}

func parseID(arg string) uint {
	var id uint
	for _, r := range arg {
		if r < '0' || r > '9' {
			return 0
		}
		id = id*10 + uint(r-'0')
	}
	return id
}
