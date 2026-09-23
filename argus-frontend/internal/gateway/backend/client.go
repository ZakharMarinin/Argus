// Package backend - HTTP-клиент к argus-backend (собственному REST API
// коллектора под /v1/api). Дашборд больше не читает Postgres напрямую:
// вся правда о целях, пробах, событиях и алертах приходит отсюда.
//
// Пакет - адаптер: DTO с JSON-тегами живут только внутри него, наружу он
// отдаёт доменные типы entity. Поэтому usecase ничего не знает ни про
// /v1/api, ни про то, что latency бэкенд считает в микросекундах.
//
// DTO ниже - копии полей entity.* из argus-backend, а не импорт самого
// модуля: два бинарника развёртываются независимо (ТЗ §8), и жёсткая
// компиляционная связь через go.mod им не нужна - достаточно совпадения
// JSON-тегов.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"argus-frontend/internal/domain/entity"
)

// listLimit - размер страницы, которой fetch догружает все записи под
// фильтром (alerts, log-matches с order=desc и since).
//
// ponytail: цели по-прежнему одним запросом до listLimit - поднять до
// цикла, если их станет больше.
const listLimit = 5000

// Client - тонкая обёртка над http.Client с базовым адресом бэкенда.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient создаёт клиент. baseURL - адрес вида "http://localhost:8081".
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: timeout},
	}
}

// targetDTO - DTO под entity.Target бэкенда.
type targetDTO struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	TargetType string    `json:"target_type"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// probeDTO - DTO под entity.Probe бэкенда. Latency там в микросекундах.
type probeDTO struct {
	ID        int       `json:"id"`
	TargetID  string    `json:"target_id"`
	Success   bool      `json:"success"`
	Latency   int64     `json:"latency"`
	CreatedAt time.Time `json:"created_at"`
}

// eventDTO - DTO под entity.ContainerEvent бэкенда.
type eventDTO struct {
	ID        int       `json:"id"`
	TargetID  string    `json:"target_id"`
	Event     string    `json:"event"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// alertDTO - DTO под entity.Alert бэкенда. Severity - число
// (0=Critical..4=Debug), как в entity.Severity коллектора.
type alertDTO struct {
	ID        int       `json:"id"`
	TargetID  string    `json:"target_id"`
	Severity  int       `json:"severity"`
	Details   string    `json:"details"`
	Notified  bool      `json:"notified"`
	CreatedAt time.Time `json:"created_at"`
}

type logMatchDTO struct {
	ID        int       `json:"id"`
	TargetID  string    `json:"target_id"`
	Severity  int       `json:"severity"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

func severityLevel(name string) (int, bool) {
	for level := range 5 {
		if severityName(level) == name {
			return level, true
		}
	}

	return 0, false
}

// severityName переводит Severity коллектора (0=Critical..4=Debug) в строку.
// Незнакомое число - info: молча ронять алерт хуже, чем показать его тихим.
func severityName(level int) string {
	switch level {
	case 0:
		return entity.SeverityCritical
	case 1:
		return entity.SeverityError
	case 2:
		return entity.SeverityWarning
	case 4:
		return entity.SeverityDebug
	default:
		return entity.SeverityInfo
	}
}

// Targets отдаёт все цели (до listLimit).
func (c *Client) Targets(ctx context.Context) ([]entity.Target, error) {
	var dto []targetDTO

	err := c.getJSON(ctx, "/v1/api/targets", url.Values{
		"limit": {strconv.Itoa(listLimit)},
	}, &dto)
	if err != nil {
		return nil, err
	}

	out := make([]entity.Target, 0, len(dto))
	for _, t := range dto {
		out = append(out, entity.Target{
			ID:        t.ID,
			Kind:      entity.Kind(t.TargetType),
			Label:     t.Name,
			Status:    entity.Status(t.Status),
			Source:    t.Source,
			UpdatedAt: t.UpdatedAt,
		})
	}

	return out, nil
}

// Probes отдаёт все пробы цели, от старых к новым. Ручка бэкенда не
// постраничная - лимит здесь не нужен.
func (c *Client) Probes(ctx context.Context, targetID string) ([]entity.ProbeOutcome, error) {
	var dto []probeDTO

	err := c.getJSON(ctx, "/v1/api/probes", url.Values{
		"target_id": {targetID},
	}, &dto)
	if err != nil {
		return nil, err
	}

	out := make([]entity.ProbeOutcome, 0, len(dto))
	for _, p := range dto {
		ms := int(p.Latency / 1000) // микросекунды -> миллисекунды
		out = append(out, entity.ProbeOutcome{
			Success:   p.Success,
			LatencyMS: &ms,
			CheckedAt: p.CreatedAt,
		})
	}

	return out, nil
}

// Events отдаёт все события контейнера-цели.
func (c *Client) Events(ctx context.Context, targetID string) ([]entity.ContainerEvent, error) {
	var dto []eventDTO

	err := c.getJSON(ctx, "/v1/api/events", url.Values{
		"target_id": {targetID},
	}, &dto)
	if err != nil {
		return nil, err
	}

	out := make([]entity.ContainerEvent, 0, len(dto))
	for _, e := range dto {
		out = append(out, entity.ContainerEvent{
			Event:      e.Event,
			Detail:     e.Details,
			OccurredAt: e.CreatedAt,
		})
	}

	return out, nil
}

func (c *Client) Alerts(ctx context.Context, q entity.ListQuery) (entity.Page[entity.Alert], error) {
	dto, err := fetch(ctx, c, "/v1/api/alerts", q, func(a alertDTO) int { return a.ID })
	if err != nil {
		return entity.Page[entity.Alert]{}, err
	}

	page := entity.Page[entity.Alert]{Total: dto.Total, Skip: dto.Skip, Items: make([]entity.Alert, 0, len(dto.Items))}
	for _, a := range dto.Items {
		page.Items = append(page.Items, entity.Alert{
			ID:             int64(a.ID),
			TargetID:       a.TargetID,
			Severity:       severityName(a.Severity),
			Message:        a.Details,
			SentToTelegram: a.Notified,
			CreatedAt:      a.CreatedAt,
		})
	}

	return page, nil
}

func (c *Client) LogMatches(ctx context.Context, q entity.ListQuery) (entity.Page[entity.LogMatch], error) {
	dto, err := fetch(ctx, c, "/v1/api/log-matches", q, func(m logMatchDTO) int { return m.ID })
	if err != nil {
		return entity.Page[entity.LogMatch]{}, err
	}

	page := entity.Page[entity.LogMatch]{Total: dto.Total, Skip: dto.Skip, Items: make([]entity.LogMatch, 0, len(dto.Items))}
	for _, m := range dto.Items {
		page.Items = append(page.Items, entity.LogMatch{
			ID:        int64(m.ID),
			TargetID:  m.TargetID,
			Severity:  severityName(m.Severity),
			Message:   m.Message,
			CreatedAt: m.CreatedAt,
		})
	}

	return page, nil
}

func fetch[T any](ctx context.Context, c *Client, path string, q entity.ListQuery, id func(T) int) (entity.Page[T], error) {
	out := entity.Page[T]{Skip: q.Skip}

	limit := q.Limit
	if limit <= 0 {
		limit = listLimit
	}

	skip := q.Skip

	for {
		var items []T

		total, err := c.getPage(ctx, path, listValues(q, skip, limit), &items)
		if err != nil {
			return entity.Page[T]{}, err
		}

		for i := 1; i < len(items); i++ {
			if id(items[i]) >= id(items[i-1]) {
				return entity.Page[T]{}, fmt.Errorf("%w: %s: id идут по возрастанию", entity.ErrBackendTooOld, path)
			}
		}

		out.Items = append(out.Items, items...)
		out.Total = total

		if q.Limit > 0 || len(items) < limit {
			return out, nil
		}

		skip += limit
	}
}

func listValues(q entity.ListQuery, skip, limit int) url.Values {
	v := url.Values{
		"order": {"desc"},
		"limit": {strconv.Itoa(limit)},
		"skip":  {strconv.Itoa(skip)},
	}

	for _, id := range q.TargetIDs {
		v.Add("target_id", id)
	}

	level, ok := severityLevel(q.Severity)
	if ok {
		v.Set("severity", strconv.Itoa(level))
	}

	if !q.Since.IsZero() {
		v.Set("since", q.Since.UTC().Format(time.RFC3339))
	}

	return v
}

func (c *Client) CreateTarget(ctx context.Context, t entity.Target) error {
	const path = "/v1/api/targets"

	body, err := json.Marshal(map[string]string{
		"name":        t.Label,
		"source":      t.Source,
		"target_type": string(t.Kind),
	})
	if err != nil {
		return fmt.Errorf("запрос %s: %w", path, err)
	}

	status, resp, err := c.send(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return err
	}

	switch status {
	case http.StatusCreated:
		return nil
	case http.StatusConflict:
		return &entity.ValidationError{Reason: "This URL is already monitored"}
	case http.StatusBadRequest:
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(resp, &e) != nil || e.Error == "" {
			e.Error = "The collector rejected this target"
		}

		return &entity.ValidationError{Reason: e.Error}
	default:
		return fmt.Errorf("%w: POST %s: %d: %s", ErrUnreachable, path, status, resp)
	}
}

func (c *Client) DeleteTarget(ctx context.Context, id string) error {
	path := "/v1/api/targets/" + url.PathEscape(id)

	status, resp, err := c.send(ctx, http.MethodDelete, path, http.NoBody)
	if err != nil {
		return err
	}

	if status != http.StatusNoContent && status != http.StatusNotFound {
		return fmt.Errorf("%w: DELETE %s: %d: %s", ErrUnreachable, path, status, resp)
	}

	return nil
}

func (c *Client) send(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("запрос %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %s: %w", ErrUnreachable, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %s: %w", ErrUnreachable, path, err)
	}

	return resp.StatusCode, data, nil
}

// Ping проверяет, что бэкенд отвечает.
func (c *Client) Ping(ctx context.Context) error {
	return c.getJSON(ctx, "/v1/api/targets", url.Values{"limit": {"1"}}, &[]targetDTO{})
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	_, err := c.do(ctx, path, query, out)

	return err
}

func (c *Client) getPage(ctx context.Context, path string, query url.Values, out any) (int, error) {
	header, err := c.do(ctx, path, query, out)
	if err != nil {
		return 0, err
	}

	total, err := strconv.Atoi(header.Get("X-Total-Count"))
	if err != nil {
		return 0, fmt.Errorf("%w: %s: нет X-Total-Count", entity.ErrBackendTooOld, path)
	}

	return total, nil
}

func (c *Client) do(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("запрос %s: %w", path, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUnreachable, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

		return nil, fmt.Errorf("%w: %s: %s: %s", ErrUnreachable, path, resp.Status, body)
	}

	err = json.NewDecoder(resp.Body).Decode(out)
	if err != nil {
		return nil, fmt.Errorf("разбор ответа %s: %w", path, err)
	}

	return resp.Header, nil
}
