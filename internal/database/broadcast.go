package database

import (
	"context"
	"fmt"
	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v4/pgxpool"
	"time"
)

// Статусы для сообщений трансляции
const (
	BroadcastStatusPending = "pending"
	BroadcastStatusSent    = "sent"
	BroadcastStatusFailed  = "failed"
)

// Broadcast представляет структуру записи в таблице трансляций
type Broadcast struct {
	ID      int64     `db:"id"`
	SenderID int64    `db:"sender_id"`
	Message  string   `db:"message"`
	SentAt   time.Time `db:"sent_at"`
	Status   string   `db:"status"`
}

// BroadcastRepository предоставляет методы для работы с таблицей трансляций
type BroadcastRepository struct {
	pool *pgxpool.Pool
}

// NewBroadcastRepository создает новый репозиторий для таблицы трансляций
func NewBroadcastRepository(pool *pgxpool.Pool) *BroadcastRepository {
	return &BroadcastRepository{pool: pool}
}

// Create добавляет новую запись трансляции в базу данных
func (br *BroadcastRepository) Create(ctx context.Context, broadcast *Broadcast) (int64, error) {
	buildInsert := sq.Insert("broadcast").
		Columns("sender_id", "message", "status").
		Values(broadcast.SenderID, broadcast.Message, broadcast.Status).
		Suffix("RETURNING id, sent_at").
		PlaceholderFormat(sq.Dollar)

	sql, args, err := buildInsert.ToSql()
	if err != nil {
		return 0, fmt.Errorf("failed to build insert broadcast query: %w", err)
	}

	var id int64
	var sentAt time.Time
	err = br.pool.QueryRow(ctx, sql, args...).Scan(&id, &sentAt)
	if err != nil {
		return 0, fmt.Errorf("failed to insert broadcast: %w", err)
	}

	broadcast.ID = id
	broadcast.SentAt = sentAt

	return id, nil
}

// UpdateStatus обновляет статус трансляции в базе данных
func (br *BroadcastRepository) UpdateStatus(ctx context.Context, id int64, status string) error {
	buildUpdate := sq.Update("broadcast").
		Set("status", status).
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := buildUpdate.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update broadcast status query: %w", err)
	}

	_, err = br.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to update broadcast status: %w", err)
	}

	return nil
}

// GetAllSubscribers возвращает список всех подписчиков из таблицы customer
func (br *BroadcastRepository) GetAllSubscribers(ctx context.Context, excludeAdminID int64) ([]int64, error) {
	buildSelect := sq.Select("telegram_id").
		From("customer").
		Where(sq.NotEq{"telegram_id": excludeAdminID}).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := buildSelect.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select subscribers query: %w", err)
	}

	rows, err := br.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query subscribers: %w", err)
	}
	defer rows.Close()

	var subscribers []int64
	for rows.Next() {
		var telegramID int64
		if err := rows.Scan(&telegramID); err != nil {
			return nil, fmt.Errorf("failed to scan subscriber row: %w", err)
		}
		subscribers = append(subscribers, telegramID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating subscriber rows: %w", err)
	}

	return subscribers, nil
}