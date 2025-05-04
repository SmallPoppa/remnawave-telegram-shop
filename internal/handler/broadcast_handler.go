package handler

import (
	"context"
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	"remnawave-tg-shop-bot/internal/broadcast"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"strings"
	"time"
)

// PMCommandHandler обрабатывает команду /pm для рассылки сообщений всем пользователям
func (h Handler) PMCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Проверяем, что команда отправлена администратором
	if update.Message.From.ID != config.GetAdminTelegramId() {
		// Пользователь не является администратором, игнорируем команду
		slog.Info("Unauthorized PM command attempt", "userId", update.Message.From.ID)
		return
	}

	// Парсим текст сообщения, убирая /pm
	messageText := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/pm"))
	
	// Проверяем, что после команды есть текст для отправки
	if messageText == "" {
		// Отправляем инструкцию по использованию
		_, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Использование: `/pm текст сообщения`\nТекст будет отправлен всем пользователям бота.",
			ParseMode: models.ParseModeMarkdown,
		})
		if err != nil {
			slog.Error("Error sending PM usage instructions", err)
		}
		return
	}

	// Создаем запись о рассылке в базе данных
	broadcast := &broadcast.Broadcast{
		SenderID: update.Message.From.ID,
		Message:  messageText,
		Status:   broadcast.BroadcastStatusPending,
	}

	// Сохраняем запись в базу
	broadcastRepo := broadcast.NewBroadcastRepository(h.customerRepository.GetPool())
	broadcastId, err := broadcastRepo.Create(ctx, broadcast)
	
	if err != nil {
		slog.Error("Error creating broadcast record", err)
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Произошла ошибка при создании рассылки.",
		})
		return
	}

	// Отправляем подтверждение о начале рассылки
	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Начинаю рассылку сообщения всем пользователям...",
	})
	if err != nil {
		slog.Error("Error sending broadcast confirmation", err)
	}

	// Получаем всех подписчиков, кроме админа
	subscribers, err := broadcastRepo.GetAllSubscribers(ctx, update.Message.From.ID)
	if err != nil {
		slog.Error("Error getting subscribers", err)
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Ошибка при получении списка подписчиков.",
		})
		return
	}

	// Запускаем асинхронную рассылку
	go h.processBroadcast(context.Background(), b, broadcastId, messageText, subscribers, update.Message.From.ID)
}

// processBroadcast асинхронно отправляет сообщение всем подписчикам
func (h *Handler) processBroadcast(ctx context.Context, b *bot.Bot, broadcastId int64, message string, subscribers []int64, adminId int64) {
	broadcastRepo := broadcast.NewBroadcastRepository(h.customerRepository.GetPool())
	
	totalCount := len(subscribers)
	successCount := 0
	failCount := 0
	
	// Ограничение скорости отправки (29 сообщений в секунду максимум)
	rateLimitDelay := time.Millisecond * 35

	for _, subscriberId := range subscribers {
		_, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: subscriberId,
			Text:   message,
			ParseMode: models.ParseModeHTML, // Поддержка HTML форматирования
		})

		if err != nil {
			slog.Error("Error sending broadcast to user", 
				"userId", subscriberId, 
				"error", err)
			failCount++
		} else {
			successCount++
		}

		// Задержка для соблюдения ограничений API Telegram
		time.Sleep(rateLimitDelay)
	}

	// Обновляем статус рассылки
	status := broadcast.BroadcastStatusSent
	if failCount > 0 && failCount == totalCount {
		status = broadcast.BroadcastStatusFailed
	}
	
	err := broadcastRepo.UpdateStatus(ctx, broadcastId, status)
	if err != nil {
		slog.Error("Error updating broadcast status", err)
	}

	// Отправляем отчет администратору
	summary := fmt.Sprintf(
		"📊 Отчет о рассылке:\n\n"+
		"✅ Успешно отправлено: %d\n"+
		"❌ Ошибок при отправке: %d\n"+
		"📨 Всего получателей: %d",
		successCount, failCount, totalCount)

	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: adminId,
		Text:   summary,
	})
	if err != nil {
		slog.Error("Error sending broadcast summary", err)
	}
}