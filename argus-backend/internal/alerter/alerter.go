package alerter

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gopkg.in/telebot.v4"
)

const pollerTimeout = 10 * time.Second

type UseCase interface {
	GetAlertsByNotification(ctx context.Context, flag bool) ([]*entity.Alert, error)
	GetTargetsByIDs(ctx context.Context, ids []uuid.UUID) ([]*entity.Target, error)
	MarkAlertNotified(ctx context.Context, id int) error
}

type Alerter struct {
	cfg *config.Config
	log *slog.Logger
	bot *telebot.Bot
	uc  UseCase
}

func NewAlerter(cfg *config.Config, log *slog.Logger, uc UseCase) (*Alerter, error) {
	b, err := telebot.NewBot(telebot.Settings{
		Token:  cfg.Telegram.BotToken,
		Poller: &telebot.LongPoller{Timeout: pollerTimeout},
	})
	if err != nil {
		return nil, err
	}

	b.Handle("/start", func(c telebot.Context) error {
		return c.Send(fmt.Sprintf(
			"Hi, Argus is here to notify you about the status of your targets.\n\nchat id: %d",
			c.Chat().ID,
		))
	})

	return &Alerter{
		cfg: cfg,
		log: log,
		bot: b,
		uc:  uc,
	}, nil
}

func (a *Alerter) StartBot() {
	a.bot.Start()
}

func (a *Alerter) StopBot() {
	a.bot.Stop()
}

func severityLabel(severity entity.Severity) string {
	switch severity {
	case entity.SeverityCritical:
		return "CRITICAL"
	case entity.SeverityError:
		return "ERROR"
	case entity.SeverityWarning:
		return "WARNING"
	case entity.SeverityInfo:
		return "INFO"
	case entity.SeverityDebug:
		return "DEBUG"
	default:
		return "UNKNOWN"
	}
}

func formatMessage(alert *entity.Alert, target *entity.Target) string {
	if target == nil {
		return fmt.Sprintf("[%s] unknown target\n\n%s\n\ntarget id: %s\ntime: %s",
			severityLabel(alert.Severity),
			alert.Details,
			alert.TargetID,
			alert.CreatedAt.Format(time.RFC3339),
		)
	}

	return fmt.Sprintf("[%s] %s\n\n%s\n\ntype: %s\nsource: %s\ntime: %s",
		severityLabel(alert.Severity),
		target.Name,
		alert.Details,
		target.TargetType,
		target.Source,
		alert.CreatedAt.Format(time.RFC3339),
	)
}

func (a *Alerter) targetsByID(ctx context.Context, alerts []*entity.Alert) (map[uuid.UUID]*entity.Target, error) {
	ids := make([]uuid.UUID, 0, len(alerts))
	seen := make(map[uuid.UUID]struct{}, len(alerts))

	for _, alert := range alerts {
		if _, ok := seen[alert.TargetID]; ok {
			continue
		}

		seen[alert.TargetID] = struct{}{}

		ids = append(ids, alert.TargetID)
	}

	targets, err := a.uc.GetTargetsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	byID := make(map[uuid.UUID]*entity.Target, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}

	return byID, nil
}

func (a *Alerter) AlertWorker(ctx context.Context) {
	const op = "Alerter.AlertWorker"

	log := a.log.With("op", op)

	alerts, err := a.uc.GetAlertsByNotification(ctx, false)
	if err != nil {
		log.Error("failed to get alerts", "error", err)

		return
	}

	if len(alerts) == 0 {
		return
	}

	targets, err := a.targetsByID(ctx, alerts)
	if err != nil {
		log.Error("failed to get alert targets", "error", err)
	}

	for _, alert := range alerts {
		_, err = a.bot.Send(telebot.ChatID(a.cfg.Telegram.ChatID), formatMessage(alert, targets[alert.TargetID]))
		if err != nil {
			log.Error("failed to send alert", "id", alert.ID, "error", err)

			continue
		}

		err = a.uc.MarkAlertNotified(ctx, alert.ID)
		if err != nil {
			log.Error("failed to mark alert notified", "id", alert.ID, "error", err)
		}
	}
}

func (a *Alerter) StartAlerter(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.Telegram.NotifyInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.AlertWorker(ctx)
		case <-ctx.Done():
			return
		}
	}
}
