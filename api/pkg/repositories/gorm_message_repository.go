package repositories

import (
	"context"
	"fmt"

	"gorm.io/gorm/clause"

	"github.com/NdoleStudio/http-sms-manager/pkg/entities"
	"github.com/NdoleStudio/http-sms-manager/pkg/telemetry"
	"github.com/cockroachdb/cockroach-go/v2/crdb/crdbgorm"
	"github.com/google/uuid"
	"github.com/palantir/stacktrace"
	"gorm.io/gorm"
)

// gormMessageRepository is responsible for persisting entities.Message
type gormMessageRepository struct {
	logger telemetry.Logger
	tracer telemetry.Tracer
	db     *gorm.DB
}

// NewGormMessageRepository creates the GORM version of the MessageRepository
func NewGormMessageRepository(
	logger telemetry.Logger,
	tracer telemetry.Tracer,
	db *gorm.DB,
) MessageRepository {
	return &gormMessageRepository{
		logger: logger.WithService(fmt.Sprintf("%T", &gormEventRepository{})),
		tracer: tracer,
		db:     db,
	}
}

// Index entities.Message between 2 parties
func (repository *gormMessageRepository) Index(ctx context.Context, from string, to string, params IndexParams) (*[]entities.Message, error) {
	ctx, span := repository.tracer.Start(ctx)
	defer span.End()

	query := repository.db.Where("\"from\" = ?", from).Where("\"to\" = ?", to)
	if len(params.Query) > 0 {
		query = query.Where("content ILIKE ?", params.Query)
	}

	messages := new([]entities.Message)
	if err := query.Order("order_timestamp DESC").Limit(params.Limit).Offset(params.Skip).Find(&messages).Error; err != nil {
		msg := fmt.Sprintf("cannot fetch messges from [%s] to [%s] and params [%+#v]", from, to, params)
		return nil, repository.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	return messages, nil
}

// Store a new entities.Message
func (repository *gormMessageRepository) Store(ctx context.Context, message *entities.Message) error {
	ctx, span := repository.tracer.Start(ctx)
	defer span.End()

	if err := repository.db.Create(message).Error; err != nil {
		msg := fmt.Sprintf("cannot save message with ID [%s]", message.ID)
		return repository.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	return nil
}

// Load an entities.Message by ID
func (repository *gormMessageRepository) Load(ctx context.Context, messageID uuid.UUID) (*entities.Message, error) {
	ctx, span := repository.tracer.Start(ctx)
	defer span.End()

	message := new(entities.Message)
	if err := repository.db.First(message, messageID).Error; err != nil {
		msg := fmt.Sprintf("cannot load message with ID [%s]", messageID)
		return nil, repository.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	return message, nil
}

// Update an entities.Message
func (repository *gormMessageRepository) Update(ctx context.Context, message *entities.Message) error {
	ctx, span := repository.tracer.Start(ctx)
	defer span.End()

	if err := repository.db.Save(message).Error; err != nil {
		msg := fmt.Sprintf("cannot update message with ID [%s]", message.ID)
		return repository.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	return nil
}

// GetOutstanding fetches messages that still to be sent to the phone
func (repository *gormMessageRepository) GetOutstanding(ctx context.Context, take int) (*[]entities.Message, error) {
	ctx, span := repository.tracer.Start(ctx)
	defer span.End()

	messages := new([]entities.Message)
	err := crdbgorm.ExecuteTx(ctx, repository.db, nil,
		func(tx *gorm.DB) error {
			return tx.Model(messages).
				Clauses(clause.Returning{}).
				Where(
					"id IN (?)",
					tx.Model(&entities.Message{}).
						Where("status = ?", entities.MessageStatusPending).
						Order("request_received_at ASC").
						Select("id").
						Limit(take),
				).
				Update("status", "sending").Error
		},
	)
	if err != nil {
		msg := fmt.Sprintf("cannot fetch [%d] outstanding messages", take)
		return nil, repository.tracer.WrapErrorSpan(span, stacktrace.Propagate(err, msg))
	}

	return messages, nil
}
