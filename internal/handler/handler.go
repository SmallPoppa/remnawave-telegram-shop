package handler

import (
	"context"
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/cryptopay"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/payment"
	"remnawave-tg-shop-bot/internal/sync"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/internal/utils"
	"remnawave-tg-shop-bot/internal/yookasa"
	"strconv"
	"strings"
	"time"
)

// ─────────── WALLET‑PAY state ────────────────────────────────────────────
var waitingScreenshot = make(map[int64]int64) // userID → purchaseID
var replyMap          = make(map[int64]int64) // adminMsgID → userID
// ────────────────────────────────────────────────────────────────────────

type Handler struct {
	customerRepository *database.CustomerRepository
	purchaseRepository *database.PurchaseRepository
	cryptoPayClient    *cryptopay.Client
	yookasaClient      *yookasa.Client
	translation        *translation.Manager
	paymentService     *payment.PaymentService
	syncService        *sync.SyncService
}

func NewHandler(
	syncService *sync.SyncService,
	paymentService *payment.PaymentService,
	translation *translation.Manager,
	customerRepository *database.CustomerRepository,
	purchaseRepository *database.PurchaseRepository,
	cryptoPayClient *cryptopay.Client,
	yookasaClient *yookasa.Client,
) *Handler {
	return &Handler{
		syncService:        syncService,
		paymentService:     paymentService,
		customerRepository: customerRepository,
		purchaseRepository: purchaseRepository,
		cryptoPayClient:    cryptoPayClient,
		yookasaClient:      yookasaClient,
		translation:        translation,
	}
}

const (
	CallbackBuy           = "buy"
	CallbackSell          = "sell"
	CallbackStart         = "start"
	CallbackConnect       = "connect"
	CallbackPayment       = "payment"
	CallbackTrial         = "trial"
	CallbackActivateTrial = "activate_trial"
)

// --------------------------- /start command ---------------------------------

func (h Handler) StartCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	ctxWithTime, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	langCode := update.Message.From.LanguageCode
	existingCustomer, err := h.customerRepository.FindByTelegramId(ctx, update.Message.Chat.ID)
	if err != nil {
		slog.Error("error finding customer by telegram id", err)
	}

	if existingCustomer == nil {
		existingCustomer, err = h.customerRepository.Create(ctxWithTime, &database.Customer{
			TelegramID: update.Message.Chat.ID,
			Language:   langCode,
		})
		if err != nil {
			slog.Error("error creating customer", err)
			return
		}
		slog.Info("user created", "telegramId", update.Message.Chat.ID)
	} else {
		updates := map[string]interface{}{
			"language": langCode,
		}
		if err = h.customerRepository.UpdateFields(ctx, existingCustomer.ID, updates); err != nil {
			slog.Error("Error updating customer", err)
			return
		}
	}

	var inlineKeyboard [][]models.InlineKeyboardButton
	if existingCustomer.SubscriptionLink == nil {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "trial_button"), CallbackData: CallbackTrial},
		})
	}

	inlineKeyboard = append(inlineKeyboard, [][]models.InlineKeyboardButton{
		{{Text: h.translation.GetText(langCode, "buy_button"), CallbackData: CallbackBuy}},
		{{Text: h.translation.GetText(langCode, "connect_button"), CallbackData: CallbackConnect}},
	}...)

	if url := config.ServerStatusURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "server_status_button"), URL: url},
		})
	}
	if url := config.SupportURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "support_button"), URL: url},
		})
	}
	if url := config.FeedbackURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "feedback_button"), URL: url},
		})
	}
	if url := config.ChannelURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "channel_button"), URL: url},
		})
	}
	if url := config.TosURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "tos_button"), URL: url},
		})
	}

	m, _ := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "🧹",
		ReplyMarkup: models.ReplyKeyboardRemove{
			RemoveKeyboard: true,
		},
	})
	_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    update.Message.Chat.ID,
		MessageID: m.ID,
	})

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		ParseMode: models.ParseModeMarkdown,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: inlineKeyboard,
		},
		Text: fmt.Sprintf(h.translation.GetText(langCode, "greeting"), bot.EscapeMarkdown(utils.BuildAvailableCountriesLists(langCode))),
	})
}

// --------------------------- TRIAL handlers ---------------------------------

func (h Handler) TrialCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	callback := update.CallbackQuery.Message.Message
	langCode := update.CallbackQuery.From.LanguageCode
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    callback.Chat.ID,
		MessageID: callback.ID,
		Text:      h.translation.GetText(langCode, "trial_text"),
		ParseMode: models.ParseModeMarkdown,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: h.translation.GetText(langCode, "activate_trial_button"), CallbackData: CallbackActivateTrial}},
				{{Text: h.translation.GetText(langCode, "back_button"), CallbackData: CallbackStart}},
			},
		},
	})
}

func (h Handler) ActivateTrialCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	callback := update.CallbackQuery.Message.Message
	_, _ = h.paymentService.ActivateTrial(ctx, update.CallbackQuery.From.ID)
	langCode := update.CallbackQuery.From.LanguageCode
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    callback.Chat.ID,
		MessageID: callback.ID,
		Text:      h.translation.GetText(langCode, "trial_activated"),
		ParseMode: models.ParseModeMarkdown,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: h.translation.GetText(langCode, "connect_button"), CallbackData: CallbackConnect}},
				{{Text: h.translation.GetText(langCode, "back_button"), CallbackData: CallbackStart}},
			},
		},
	})
}

// --------------------------- START callback ---------------------------------

func (h Handler) StartCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	ctxWithTime, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cb := update.CallbackQuery
	langCode := cb.From.LanguageCode

	existingCustomer, _ := h.customerRepository.FindByTelegramId(ctxWithTime, cb.From.ID)
	if existingCustomer == nil {
		existingCustomer, _ = h.customerRepository.Create(ctxWithTime, &database.Customer{TelegramID: cb.From.ID, Language: langCode})
	}

	var inlineKeyboard [][]models.InlineKeyboardButton
	if existingCustomer.SubscriptionLink == nil {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "trial_button"), CallbackData: CallbackTrial},
		})
	}
	inlineKeyboard = append(inlineKeyboard, [][]models.InlineKeyboardButton{
		{{Text: h.translation.GetText(langCode, "buy_button"), CallbackData: CallbackBuy}},
		{{Text: h.translation.GetText(langCode, "connect_button"), CallbackData: CallbackConnect}},
	}...)

	if url := config.ServerStatusURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "server_status_button"), URL: url},
		})
	}
	if url := config.SupportURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "support_button"), URL: url},
		})
	}
	if url := config.FeedbackURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "feedback_button"), URL: url},
		})
	}
	if url := config.ChannelURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "channel_button"), URL: url},
		})
	}
	if url := config.TosURL(); url != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "tos_button"), URL: url},
		})
	}

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    cb.Message.Message.Chat.ID,
		MessageID: cb.Message.Message.ID,
		ParseMode: models.ParseModeMarkdown,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: inlineKeyboard,
		},
		Text: fmt.Sprintf(h.translation.GetText(langCode, "greeting"), bot.EscapeMarkdown(utils.BuildAvailableCountriesLists(langCode))),
	})
}

// --------------------------- BUY callback -----------------------------------

func (h Handler) BuyCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery.Message.Message
	langCode := update.CallbackQuery.From.LanguageCode

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    cb.Chat.ID,
		MessageID: cb.ID,
		ParseMode: models.ParseModeMarkdown,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: h.translation.GetText(langCode, "month_1"), CallbackData: fmt.Sprintf("%s?month=%d&amount=%d", CallbackSell, 1, config.Price1())},
					{Text: h.translation.GetText(langCode, "month_3"), CallbackData: fmt.Sprintf("%s?month=%d&amount=%d", CallbackSell, 3, config.Price3())},
					{Text: h.translation.GetText(langCode, "month_6"), CallbackData: fmt.Sprintf("%s?month=%d&amount=%d", CallbackSell, 6, config.Price6())},
					{Text: h.translation.GetText(langCode, "month_12"), CallbackData: fmt.Sprintf("%s?month=%d&amount=%d", CallbackSell, 12, config.Price12())},
				},
				{
					{Text: h.translation.GetText(langCode, "back_button"), CallbackData: CallbackStart},
				},
			},
		},
		Text: fmt.Sprintf(h.translation.GetText(langCode, "pricing_info"),
			config.Price1(), config.Price3(), config.Price6(), config.Price12()),
	})
}

// -------------------------- SELL callback -----------------------------------

func (h Handler) SellCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery.Message.Message
	params := parseCallbackData(update.CallbackQuery.Data)
	langCode := update.CallbackQuery.From.LanguageCode
	month := params["month"]
	amount := params["amount"]

	var keyboard [][]models.InlineKeyboardButton
	if config.IsCryptoPayEnabled() {
		keyboard = append(keyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "crypto_button"), CallbackData: fmt.Sprintf("%s?month=%s&invoiceType=%s&amount=%s", CallbackPayment, month, database.InvoiceTypeCrypto, amount)},
		})
	}
	if config.UsdtWallet() != "" {
		keyboard = append(keyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "to_wallet"), CallbackData: fmt.Sprintf("%s?month=%s&invoiceType=%s&amount=%s", CallbackPayment, month, database.InvoiceTypeWallet, amount)},
		})
	}
	if config.IsYookasaEnabled() {
		keyboard = append(keyboard, []models.InlineKeyboardButton{
			{Text: h.translation.GetText(langCode, "card_button"), CallbackData: fmt.Sprintf("%s?month=%s&invoiceType=%s&amount=%s", CallbackPayment, month, database.InvoiceTypeYookasa, amount)},
		})
	}
	if config.IsTelegramStarsEnabled() {
		keyboard = append(keyboard, []models.InlineKeyboardButton{
			{Text: "⭐Telegram Stars", CallbackData: fmt.Sprintf("%s?month=%s&invoiceType=%s&amount=%s", CallbackPayment, month, database.InvoiceTypeTelegram, amount)},
		})
	}
	keyboard = append(keyboard, []models.InlineKeyboardButton{
		{Text: h.translation.GetText(langCode, "back_button"), CallbackData: CallbackStart},
	})

	_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
		ChatID:    cb.Chat.ID,
		MessageID: cb.ID,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: keyboard,
		},
	})
}

// -------------------------- PAYMENT callback ------------------------------

func (h Handler) PaymentCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery.Message.Message
	params := parseCallbackData(update.CallbackQuery.Data)

	month, _ := strconv.Atoi(params["month"])
	price, _ := strconv.Atoi(params["amount"])
	invoiceType := database.InvoiceType(params["invoiceType"])

	ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	customer, _ := h.customerRepository.FindByTelegramId(ctx2, cb.Chat.ID)
	if customer == nil {
		slog.Error("customer not exist", "chatID", cb.Chat.ID)
		return
	}

	// — ручная оплата на USDT‑кошелёк —
	if invoiceType == database.InvoiceTypeWallet {
		idStr, _ := h.paymentService.CreatePurchase(ctx2, price, month, customer, invoiceType)
		pid, _ := strconv.ParseInt(idStr, 10, 64)
		lang := update.CallbackQuery.From.LanguageCode
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    cb.Chat.ID,
			MessageID: cb.ID,
			ParseMode: models.ParseModeMarkdown,
			Text:      fmt.Sprintf(h.translation.GetText(lang, "wallet_send"), price) + "\n`" + config.UsdtWallet() + "`",
			ReplyMarkup: models.InlineKeyboardMarkup{
				InlineKeyboard: [][]models.InlineKeyboardButton{{
					{
						Text:         h.translation.GetText(lang, "send_payment_screenshot"),
						CallbackData: fmt.Sprintf("send_ss_%d", pid),
					},
					{
						Text:         h.translation.GetText(lang, "back_button"),
						CallbackData: fmt.Sprintf("%s?month=%d&amount=%d", CallbackSell, month, price),
					},
				}},
			},
		})
		return
	}

	// — все остальные способы оплаты —
	paymentURL, _ := h.paymentService.CreatePurchase(ctx2, price, month, customer, invoiceType)
	lang := update.CallbackQuery.From.LanguageCode
	_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
		ChatID:    cb.Chat.ID,
		MessageID: cb.ID,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{{
				{Text: h.translation.GetText(lang, "pay_button"), URL: paymentURL},
				{Text: h.translation.GetText(lang, "back_button"), CallbackData: fmt.Sprintf("%s?month=%d&amount=%d", CallbackSell, month, price)},
			}},
		},
	})
}

// ——————— кнопка “Send payment screenshot” —————————————————

func (h Handler) SendScreenshotCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	pidStr := strings.TrimPrefix(update.CallbackQuery.Data, "send_ss_")
	pid, _ := strconv.ParseInt(pidStr, 10, 64)
	waitingScreenshot[update.CallbackQuery.From.ID] = pid

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.CallbackQuery.From.ID,
		Text:   "Please attach the transaction screenshot now.",
	})
}

// ——————— приём фото → пересылаем админу —————————————————

func (h Handler) PhotoMessageHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.Photo == nil {
		return
	}
	uid := msg.From.ID
	pid, ok := waitingScreenshot[uid]
	if !ok {
		return
	}
	delete(waitingScreenshot, uid)

	purchase, _ := h.purchaseRepository.FindById(ctx, pid)
	tariff := ""
	if purchase != nil {
		tariff = fmt.Sprintf("\nTariff: %d months", purchase.Month)
	}

	copied, _ := b.CopyMessage(ctx, &bot.CopyMessageParams{
		ChatID:     config.GetAdminTelegramId(),
		FromChatID: msg.Chat.ID,
		MessageID:  msg.ID,
		Caption: fmt.Sprintf(
			"USDT wallet payment%s\nPurchase ID: %d\nUser: %d (@%s)",
			tariff, pid, uid, msg.From.Username,
		),
	})
	replyMap[int64(copied.ID)] = uid

	lang := msg.From.LanguageCode
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: uid,
		Text:   h.translation.GetText(lang, "payment_received_wait"),
	})
}

// ——————— админ отвечает → пересылаем юзеру —————————————————

func (h Handler) AdminReplyHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.ReplyToMessage == nil {
		return
	}
	if msg.From.ID != config.GetAdminTelegramId() {
		return
	}
	userID, ok := replyMap[int64(msg.ReplyToMessage.ID)]
	if !ok {
		return
	}

	if msg.Text != "" {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userID,
			Text:   msg.Text,
		})
	}
	if msg.Photo != nil {
		_, _ = b.CopyMessage(ctx, &bot.CopyMessageParams{
			ChatID:     userID,
			FromChatID: msg.Chat.ID,
			MessageID:  msg.ID,
		})
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   "✅ Отправлено пользователю",
	})
}

// --------------------------- /connect command -------------------------------

func (h Handler) ConnectCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	customer, _ := h.customerRepository.FindByTelegramId(ctx, update.Message.Chat.ID)
	langCode := update.Message.From.LanguageCode
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   buildConnectText(customer, langCode),
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: h.translation.GetText(langCode, "back_button"), CallbackData: CallbackStart}}},
		},
	})
}

// -------------------------- CONNECT callback --------------------------------

func (h Handler) ConnectCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery.Message.Message
	customer, _ := h.customerRepository.FindByTelegramId(ctx, cb.Chat.ID)
	langCode := update.CallbackQuery.From.LanguageCode
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    cb.Chat.ID,
		MessageID: cb.ID,
		Text:      buildConnectText(customer, langCode),
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: h.translation.GetText(langCode, "back_button"), CallbackData: CallbackStart}}},
		},
	})
}

// ------------------------ other telegram handlers ---------------------------

func (h Handler) PreCheckoutCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	_, _ = b.AnswerPreCheckoutQuery(ctx, &bot.AnswerPreCheckoutQueryParams{
		PreCheckoutQueryID: update.PreCheckoutQuery.ID,
		OK:                 true,
	})
}

func (h Handler) SuccessPaymentHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	purchaseID, _ := strconv.Atoi(update.Message.SuccessfulPayment.InvoicePayload)
	_ = h.paymentService.ProcessPurchaseById(int64(purchaseID))
}

func (h Handler) SyncUsersCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.syncService.Sync()
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Users synced",
	})
}

// --------------------------- helper functions -------------------------------

func buildConnectText(customer *database.Customer, langCode string) string {
	var sb strings.Builder
	tm := translation.GetInstance()
	if customer.ExpireAt != nil && time.Now().Before(*customer.ExpireAt) {
		sb.WriteString(fmt.Sprintf(tm.GetText(langCode, "subscription_active"), customer.ExpireAt.Format("02.01.2006 15:04")))
		if customer.SubscriptionLink != nil && *customer.SubscriptionLink != "" {
			sb.WriteString(fmt.Sprintf(tm.GetText(langCode, "subscription_link"), *customer.SubscriptionLink))
		}
	} else {
		sb.WriteString(tm.GetText(langCode, "no_subscription"))
	}
	return sb.String()
}

func parseCallbackData(data string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(data, "?")
	if len(parts) < 2 {
		return result
	}
	for _, param := range strings.Split(parts[1], "&") {
		if kv := strings.SplitN(param, "=", 2); len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}
