package maxbot

import (
	"context"
	"fmt"
	"io"
	"os"

	maxbotlib "github.com/max-messenger/max-bot-api-client-go"
	"github.com/max-messenger/max-bot-api-client-go/schemes"
)

type Client struct {
	api *maxbotlib.Api
}

type Keyboard = maxbotlib.Keyboard

func NewClient(opts ...maxbotlib.Option) (*Client, error) {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("BOT_TOKEN env is required")
	}

	api, err := maxbotlib.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("init api: %w", err)
	}

	return &Client{api: api}, nil
}

func (c *Client) GetBot(ctx context.Context) (*schemes.BotInfo, error) {
	return c.api.Bots.GetBot(ctx)
}

func (c *Client) GetUpdates(ctx context.Context) <-chan schemes.UpdateInterface {
	return c.api.GetUpdates(ctx)
}

func (c *Client) GetErrors() <-chan error {
	return c.api.GetErrors()
}

func (c *Client) Send(ctx context.Context, m *maxbotlib.Message) error {
	return c.api.Messages.Send(ctx, m)
}

func (c *Client) SendWithResult(ctx context.Context, m *maxbotlib.Message) (*schemes.Message, error) {
	return c.api.Messages.SendWithResult(ctx, m)
}

func (c *Client) EditMessage(ctx context.Context, messageID string, m *maxbotlib.Message) error {
	return c.api.Messages.EditMessage(ctx, messageID, m)
}

func (c *Client) DeleteMessage(ctx context.Context, messageID string) error {
	_, err := c.api.Messages.DeleteMessage(ctx, messageID)
	return err
}

func (c *Client) GetMessage(ctx context.Context, messageID string) (*schemes.Message, error) {
	return c.api.Messages.GetMessage(ctx, messageID)
}

func (c *Client) GetMessages(ctx context.Context, chatID int64, messageIDs []string, from, to, count int) (*schemes.MessageList, error) {
	return c.api.Messages.GetMessages(ctx, chatID, messageIDs, from, to, count)
}

func (c *Client) AnswerOnCallback(ctx context.Context, callbackID string, answer *schemes.CallbackAnswer) error {
	_, err := c.api.Messages.AnswerOnCallback(ctx, callbackID, answer)
	return err
}

func (c *Client) GetChat(ctx context.Context, chatID int64) (*schemes.Chat, error) {
	return c.api.Chats.GetChat(ctx, chatID)
}

func (c *Client) GetChatMembers(ctx context.Context, chatID, count, marker int64) (*schemes.ChatMembersList, error) {
	return c.api.Chats.GetChatMembers(ctx, chatID, count, marker)
}

func (c *Client) LeaveChat(ctx context.Context, chatID int64) error {
	_, err := c.api.Chats.LeaveChat(ctx, chatID)
	return err
}

func (c *Client) EditChat(ctx context.Context, chatID int64, patch *schemes.ChatPatch) (*schemes.Chat, error) {
	return c.api.Chats.EditChat(ctx, chatID, patch)
}

func (c *Client) SendAction(ctx context.Context, chatID int64, action schemes.SenderAction) error {
	_, err := c.api.Chats.SendAction(ctx, chatID, action)
	return err
}

func (c *Client) PinMessage(ctx context.Context, chatID int64, body schemes.PinMessageBody) error {
	_, err := c.api.Chats.PinMessage(ctx, chatID, body)
	return err
}

func (c *Client) UploadPhotoFromFile(ctx context.Context, filePath string) (*schemes.PhotoTokens, error) {
	return c.api.Uploads.UploadPhotoFromFile(ctx, filePath)
}

func (c *Client) UploadPhotoFromUrl(ctx context.Context, url string) (*schemes.PhotoTokens, error) {
	return c.api.Uploads.UploadPhotoFromUrl(ctx, url)
}

func (c *Client) UploadPhotoFromReader(ctx context.Context, reader io.Reader) (*schemes.PhotoTokens, error) {
	return c.api.Uploads.UploadPhotoFromReader(ctx, reader)
}

func (c *Client) UploadMediaFromFile(ctx context.Context, uploadType schemes.UploadType, filePath string) (*schemes.UploadedInfo, error) {
	return c.api.Uploads.UploadMediaFromFile(ctx, uploadType, filePath)
}

func (c *Client) UploadMediaFromUrl(ctx context.Context, uploadType schemes.UploadType, url string) (*schemes.UploadedInfo, error) {
	return c.api.Uploads.UploadMediaFromUrl(ctx, uploadType, url)
}

func (c *Client) UploadMediaFromReader(ctx context.Context, uploadType schemes.UploadType, reader io.Reader) (*schemes.UploadedInfo, error) {
	return c.api.Uploads.UploadMediaFromReader(ctx, uploadType, reader)
}

func (c *Client) NewMessage() *maxbotlib.Message {
	return maxbotlib.NewMessage()
}

func (c *Client) NewKeyboard() *maxbotlib.Keyboard {
	return c.api.Messages.NewKeyboardBuilder()
}
