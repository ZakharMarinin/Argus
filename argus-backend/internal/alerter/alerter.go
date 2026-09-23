package alerter

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"gopkg.in/telebot.v4"
)

const (
	pollerTimeout        = 10 * time.Second
	alertsPerTick        = 50
	maxListedAlerts      = 10
	maxDetailsLength     = 300
	telegramMessageLimit = 4096
)

type notification struct {
	Alerts []*entity.Alert
	Target entity.Target
}

type UseCase interface {
	GetAlertsByNotification(ctx context.Context, flag bool, limit uint64) ([]*entity.Alert, error)
	GetTargetsByIDs(ctx context.Context, ids []uuid.UUID) ([]*entity.Target, error)
	MarkAlertsNotified(ctx context.Context, alerts []*entity.Alert) error
}

type alertKey struct {
	targetID uuid.UUID
	details  string
}

type Alerter struct {
	cfg         *config.Config
	log         *slog.Logger
	bot         *telebot.Bot
	uc          UseCase
	lastSent    map[alertKey]time.Time
	suppressed  map[uuid.UUID]int
	pausedUntil time.Time
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
		cfg:        cfg,
		log:        log,
		bot:        b,
		uc:         uc,
		lastSent:   make(map[alertKey]time.Time),
		suppressed: make(map[uuid.UUID]int),
	}, nil
}

func (a *Alerter) StartBot() {
	a.bot.Start()
}

func (a *Alerter) StopBot() {
	a.bot.Stop()
}

func formatMessage(n *notification, suppressed int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[%s] %s - %s\n", maxSeverity(n.Alerts), n.Target.Name, n.Target.Status)

	if len(n.Alerts) <= maxListedAlerts {
		for _, alert := range n.Alerts {
			fmt.Fprintf(&b, "\n  - [%s] %s", alert.Severity, truncate(alert.Details, maxDetailsLength))
		}
	} else {
		fmt.Fprintf(&b, "\n%d alerts: %s\nlatest: %s",
			len(n.Alerts), severityCounts(n.Alerts), truncate(n.Alerts[len(n.Alerts)-1].Details, maxDetailsLength))
	}

	if suppressed > 0 {
		fmt.Fprintf(&b, "\n\n+%d suppressed", suppressed)
	}

	return truncate(b.String(), telegramMessageLimit)
}

func maxSeverity(alerts []*entity.Alert) entity.Severity {
	top := entity.SeverityDebug
	for _, alert := range alerts {
		top = min(top, alert.Severity)
	}

	return top
}

func severityCounts(alerts []*entity.Alert) string {
	counts := make(map[entity.Severity]int, len(alerts))
	for _, alert := range alerts {
		counts[alert.Severity]++
	}

	parts := make([]string, 0, len(counts))
	for _, severity := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, fmt.Sprintf("%s ×%d", severity, counts[severity]))
	}

	return strings.Join(parts, ", ")
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	units, cut := 0, -1

	for i, r := range s {
		if cut < 0 && units+utf16.RuneLen(r) > limit-1 {
			cut = i
		}

		units += utf16.RuneLen(r)
		if units > limit {
			return s[:cut] + "…"
		}
	}

	return s
}

func (a *Alerter) targetsByID(ctx context.Context, alerts []*entity.Alert) ([]*notification, error) {
	seen := make(map[uuid.UUID][]*entity.Alert, len(alerts))

	for _, alert := range alerts {
		seen[alert.TargetID] = append(seen[alert.TargetID], alert)
	}

	targets, err := a.uc.GetTargetsByIDs(ctx, slices.Collect(maps.Keys(seen)))
	if err != nil {
		return nil, err
	}

	notifs := make([]*notification, 0, len(targets))
	for _, target := range targets {
		notifs = append(notifs, &notification{Alerts: seen[target.ID], Target: *target})
	}

	return notifs, nil
}

func (a *Alerter) AlertWorker(ctx context.Context) {
	const op = "Alerter.AlertWorker"

	log := a.log.With("op", op)

	now := time.Now()
	if now.Before(a.pausedUntil) {
		return
	}

	alerts, err := a.uc.GetAlertsByNotification(ctx, false, alertsPerTick)
	if err != nil {
		log.Error("failed to get alerts", "error", err)

		return
	}

	if len(alerts) == 0 {
		return
	}

	notifications, err := a.targetsByID(ctx, alerts)
	if err != nil {
		log.Error("failed to get notifications", "error", err)

		return
	}

	maps.DeleteFunc(a.lastSent, func(_ alertKey, sent time.Time) bool {
		return now.Sub(sent) >= a.cfg.Telegram.Cooldown
	})

	for _, notif := range notifications {
		if !a.notify(ctx, log, notif, now) {
			return
		}
	}
}

func (a *Alerter) notify(ctx context.Context, log *slog.Logger, n *notification, now time.Time) bool {
	send, muted := a.split(n, now)

	if len(send) == 0 {
		err := a.uc.MarkAlertsNotified(ctx, muted)
		if err != nil {
			log.Error("failed to mark alerts notified", "error", err)

			return true
		}

		a.suppressed[n.Target.ID] += len(muted)

		return true
	}

	msg := formatMessage(&notification{Alerts: send, Target: n.Target}, a.suppressed[n.Target.ID]+len(muted))

	_, err := a.bot.Send(telebot.ChatID(a.cfg.Telegram.ChatID), msg)

	if flood, ok := errors.AsType[telebot.FloodError](err); ok {
		a.pausedUntil = now.Add(time.Duration(flood.RetryAfter) * time.Second)
		log.Warn("telegram rate limit hit, pausing alerter", "retry_after_sec", flood.RetryAfter)

		return false
	}

	if err != nil {
		log.Error("failed to send notification", "id", n.Target.ID, "error", err)

		return true
	}

	for _, alert := range send {
		a.lastSent[alertKey{alert.TargetID, alert.Details}] = now
	}

	delete(a.suppressed, n.Target.ID)

	err = a.uc.MarkAlertsNotified(ctx, n.Alerts)
	if err != nil {
		log.Error("failed to mark alerts notified", "error", err)
	}

	return true
}

func (a *Alerter) split(n *notification, now time.Time) ([]*entity.Alert, []*entity.Alert) {
	var send, muted []*entity.Alert

	for _, alert := range n.Alerts {
		sent, ok := a.lastSent[alertKey{alert.TargetID, alert.Details}]
		if ok && now.Sub(sent) < a.cfg.Telegram.Cooldown {
			muted = append(muted, alert)

			continue
		}

		send = append(send, alert)
	}

	return send, muted
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
