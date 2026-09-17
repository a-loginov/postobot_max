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
	stateIdle            sessionState = "idle"
	stateAwaitingMedia   sessionState = "awaiting_media"
	stateAwaitingClass   sessionState = "awaiting_class"
	stateAwaitingArchive sessionState = "awaiting_archive"

	stateBetaName    sessionState = "beta_name"
	stateBetaSurname sessionState = "beta_surname"
	stateBetaClass   sessionState = "beta_class"
	stateBetaReason  sessionState = "beta_reason"
)

type session struct {
	state       sessionState
	reqType     db.RequestType
	description string
	class       string
	photo       *photoInfo
	name        string
	surname     string
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
		moderation:   moderation.New(cfg.MatWords, cfg.SpamWords),
		sessions:     make(map[int64]*session),
		adminPending: make(map[int64]bool),
		adminAuth:    make(map[int64]bool),
	}
}

const menuText = "ПостоБот — сервис по приёму заявок и предложений по улучшению среды образовательного квартала 1409.\n\n" +
	"📝 Заявка — быстрое реагирование на проблему (сломанный стул, разбитое окно, отсутствие воды).\n" +
	"💡 Предложение — идея по улучшению школьной среды.\n\n" +
	"Выбери, что нужно, — как только вопрос будет решён, пришлём уведомление."

const requestIntro = "Вы можете оставить заявку — быстрое реагирование на проблему.\n\n" +
	"Чаще всего просят: сломанный стул, разбитое окно, отсутствие воды.\n\n" +
	"Обязательно прикрепите фотографию и оставьте комментарий."

const proposalIntro = "Вы можете сформировать предложение по улучшению среды образовательного квартала 1409.\n\n" +
	"Обязательно прикрепите фотографию и оставьте комментарий."

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
		b.setSession(userID, &session{reqType: db.TypeRequest, state: stateIdle})
		b.replyKeyboard(ctx, userID, 0, requestIntro, b.mediaButtons())
	case "proposal":
		b.setSession(userID, &session{reqType: db.TypeProposal, state: stateIdle})
		b.replyKeyboard(ctx, userID, 0, proposalIntro, b.mediaButtons())
	case "media":
		s := b.getSession(userID)
		if s.reqType == "" {
			s.reqType = db.TypeRequest
		}
		s.state = stateAwaitingMedia
		b.reply(ctx, userID, 0, "Прикрепи фото и напиши комментарий — что именно нужно сделать 📸")
	case "cancel":
		b.setSession(userID, &session{state: stateIdle})
		b.reply(ctx, userID, 0, "Отменено.")
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

	// Profanity and spam guard on every free-text input, no matter the wizard step.
	if text != "" {
		mod := b.moderation.Check(text)
		if mod.HasMat {
			b.reply(ctx, userID, chatID,
				"В сообщении нецензурные выражения. Перепиши, пожалуйста, без мата 🙏")
			return
		}
		if mod.HasSpam {
			b.reply(ctx, userID, chatID,
				"Это сообщение распознано как спам. Если это ошибка — напиши иначе.")
			return
		}
	}

	// Any free text from an idle student that mentions a month is treated as an archive query.
	if s.state == stateIdle && text != "" && monthHint(text) != "" {
		b.archiveQuery(ctx, userID, student, text)
		return
	}

	switch s.state {
	case stateIdle:
		if text == "" && len(photos) == 0 {
			b.reply(ctx, userID, chatID, "Выбери, что нужно: 📝 Заявка или 💡 Предложение 👇")
			b.sendMenu(ctx, userID)
			return
		}
		// Plain text or photo at idle starts a request without asking for a menu tap.
		if s.reqType == "" {
			s.reqType = db.TypeRequest
		}
		s.state = stateAwaitingMedia
		fallthrough
	case stateAwaitingMedia:
		if len(photos) > 0 {
			p := photos[0]
			if p.Token == "" && p.Url == "" {
				b.reply(ctx, userID, chatID, "Не удалось получить фото. Пришли фотографию ещё раз.")
				return
			}
			s.photo = &p
		}
		if text != "" {
			s.description = text
		}
		if s.description == "" {
			b.reply(ctx, userID, chatID, "Напиши комментарий — что именно нужно сделать 📝")
			return
		}
		if s.photo == nil {
			b.reply(ctx, userID, chatID, "Прикрепи фото 📸 — без фото заявку не примем.")
			return
		}
		s.state = stateAwaitingClass
		b.reply(ctx, userID, chatID, "Из какого ты класса? Например: 9А.")

	case stateAwaitingClass:
		if text == "" {
			b.reply(ctx, userID, chatID, "Напиши класс текстом. Например: 9А.")
			return
		}
		s.class = text
		b.submitRequest(ctx, student, s)

	case stateAwaitingArchive:
		b.archiveQuery(ctx, userID, student, text)
	}
}

func (b *Bot) submitRequest(ctx context.Context, student *db.Student, s *session) {
	reqType := s.reqType
	if reqType == "" {
		reqType = db.TypeRequest
	}

	if err := b.database.UpdateStudentClass(ctx, student.ID, s.class); err != nil {
		log.Printf("UpdateStudentClass: %v", err)
	}

	photo := *s.photo

	req := &db.Request{
		StudentID:   student.ID,
		Type:        reqType,
		Description: s.description,
		Normalized:  moderation.Normalize(s.description),
		PhotoToken:  photo.Token,
		PhotoURL:    photo.Url,
	}

	result := b.moderation.Check(s.description)

	// Auto-moderation: profanity, spam and near-duplicates.
	if result.HasMat {
		req.Status = db.StatusRejected
		req.RejectReason = "мат"
		if err := b.database.CreateRequest(ctx, req); err != nil {
			log.Printf("CreateRequest rejected: %v", err)
		}
		if req.Type == db.TypeProposal {
			b.reply(ctx, student.MaxUserID, 0,
				"Предложение отклонено: сообщение содержит недопустимые выражения. Перепиши по-нормальному.")
		} else {
			b.reply(ctx, student.MaxUserID, 0,
				"Заявка отклонена: сообщение содержит недопустимые выражения. Перепиши по-нормальному.")
		}
		return
	}

	if result.HasSpam {
		req.Status = db.StatusRejected
		req.RejectReason = "спам"
		if err := b.database.CreateRequest(ctx, req); err != nil {
			log.Printf("CreateRequest rejected spam: %v", err)
		}
		if req.Type == db.TypeProposal {
			b.reply(ctx, student.MaxUserID, 0, "Предложение отклонено: сообщение распознано как спам.")
		} else {
			b.reply(ctx, student.MaxUserID, 0, "Заявка отклонена: сообщение распознано как спам.")
		}
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
		if req.Type == db.TypeProposal {
			b.reply(ctx, student.MaxUserID, 0,
				"Похожее предложение уже отправлено и принято в работу. Дубликат не нужен.")
		} else {
			b.reply(ctx, student.MaxUserID, 0,
				"Похожая заявка уже отправлена и принята в работу. Дубликат не нужен.")
		}
		return
	}

	req.Status = db.StatusActive
	if err := b.database.CreateRequest(ctx, req); err != nil {
		log.Printf("CreateRequest: %v", err)
		b.reply(ctx, student.MaxUserID, 0, "Не удалось сохранить заявку, попробуй ещё раз.")
		return
	}

	if req.Type == db.TypeProposal {
		b.replyKeyboard(ctx, student.MaxUserID, 0,
			fmt.Sprintf("✅ Предложение №%d принято!\n👤 Класс: %s\n💡 Идея: %s",
				req.ID, s.class, s.description),
			b.doneButtons())
	} else {
		b.replyKeyboard(ctx, student.MaxUserID, 0,
			fmt.Sprintf("✅ Заявка №%d принята!\n👤 Класс: %s\n🔧 Проблема: %s",
				req.ID, s.class, s.description),
			b.doneButtons())
	}

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

func typeNoun(t db.RequestType) string {
	if t == db.TypeProposal {
		return "предложение"
	}
	return "заявка"
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
	kind := "🧾 Заявка №%d"
	if r.Type == db.TypeProposal {
		kind = "💡 Предложение №%d"
	}
	return fmt.Sprintf(
		kind+"\n👤 Кто: %s класс\n%s: %s\n📸 Фото: %s\nСтатус: %s",
		r.ID, r.Student.Class, typeNoun(r.Type), r.Description, photo, status)
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
		icon := "🧾"
		if r.Type == db.TypeProposal {
			icon = "💡"
		}
		msg += fmt.Sprintf("%s #%d • приоритет %d • %s класс • %s\n",
			icon, r.ID, r.Priority, r.Student.Class, r.Description)
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
				fmt.Sprintf("✅ Выполнено #%d: %s", req.ID, req.Description))
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

	msg := "Твои заявки и предложения:\n\n"
	for _, r := range requests {
		icon := "🧾"
		if r.Type == db.TypeProposal {
			icon = "💡"
		}
		msg += fmt.Sprintf("%s #%d • %s • %s\n", icon, r.ID, statusLabel(r.Status), r.Description)
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
	row.AddCallback("📝 Заявка", schemes.DEFAULT, "new")
	row.AddCallback("💡 Предложение", schemes.DEFAULT, "proposal")

	row2 := kb.AddRow()
	row2.AddCallback("📋 Мои заявки", schemes.DEFAULT, "mylist")
	row2.AddCallback("🗄 Архив", schemes.DEFAULT, "archive")

	m := b.client.NewMessage().SetText(menuText).AddKeyboard(kb)
	m.SetUser(userID)
	if err := b.client.Send(ctx, m); err != nil {
		log.Printf("sendMenu: %v", err)
	}
}

// mediaButtons starts the media input of the request/proposal wizard.
func (b *Bot) mediaButtons() *maxbot.Keyboard {
	kb := b.client.NewKeyboard()
	row := kb.AddRow()
	row.AddCallback("📷 Фото и комментарий", schemes.DEFAULT, "media")
	row.AddCallback("❌ Отмена", schemes.DEFAULT, "cancel")
	return kb
}

// doneButtons offers the next steps after a request/proposal was submitted.
func (b *Bot) doneButtons() *maxbot.Keyboard {
	kb := b.client.NewKeyboard()
	row := kb.AddRow()
	row.AddCallback("📋 Мои заявки", schemes.DEFAULT, "mylist")
	row.AddCallback("🗄 Архив", schemes.DEFAULT, "archive")
	return kb
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
