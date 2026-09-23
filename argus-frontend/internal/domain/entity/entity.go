// Package entity - доменные типы дашборда Argus.
//
// Слой ничего не знает ни про HTTP API бэкенда, ни про HTML: usecase считает
// из них агрегаты, transport раскладывает по представлению. Зависимостей
// наружу у пакета нет - только стандартная библиотека.
package entity

import "time"

// Kind - вид цели. Значения совпадают с target_type бэкенда (ТЗ §7).
type Kind string

const (
	KindProbe     Kind = "probe"
	KindContainer Kind = "container"
	KindHost      Kind = "host"
)

// Status - состояние цели, как его держит бэкенд.
//
// StatusUnknown - это не то же самое, что down: так помечаются цели, о которых
// коллектор сейчас ничего не знает, например при недоступном Docker (ТЗ §9).
type Status string

const (
	StatusUp      Status = "up"
	StatusDown    Status = "down"
	StatusUnknown Status = "unknown"
)

// Target - цель как её отдаёт бэкенд, без агрегатов.
type Target struct {
	ID        string
	Kind      Kind
	Label     string
	Status    Status
	Source    string
	UpdatedAt time.Time
}

// TargetSummary - цель вместе с посчитанными
// агрегатами. Probe заполнен только для uptime-таргетов, Container - только
// для контейнеров; у второго вида поле всегда nil.
type TargetSummary struct {
	ID     string
	Kind   Kind
	Label  string
	Status Status
	// Source - адрес, который опрашивает пробер (для Kind == KindProbe).
	// У контейнеров это то же самое, что Label (имя контейнера).
	Source    string
	UpdatedAt time.Time

	Probe     *ProbeStats
	Container *ContainerStats
}

// ProbeStats - агрегаты по результатам проверок uptime-таргета.
type ProbeStats struct {
	// LastLatencyMS и LastCheckedAt - последняя проверка. nil, если проверок
	// ещё не было: цель уже в конфиге, но коллектор до неё не дошёл.
	LastLatencyMS *int
	LastCheckedAt *time.Time

	// Checks24h и Successes24h считаются за последние сутки. Ноль проверок -
	// это ноль, а не 100% аптайма.
	Checks24h    int
	Successes24h int

	// Recent - последние проверки от старых к новым, для полоски на карточке.
	Recent []ProbeOutcome
}

// Uptime24h возвращает долю успешных проверок за сутки и признак того, что
// считать было из чего.
func (p ProbeStats) Uptime24h() (float64, bool) {
	if p.Checks24h == 0 {
		return 0, false
	}

	return 100 * float64(p.Successes24h) / float64(p.Checks24h), true
}

// ProbeOutcome - исход одной проверки.
type ProbeOutcome struct {
	Success   bool
	LatencyMS *int
	CheckedAt time.Time
}

// ContainerStats - агрегаты по событиям контейнера.
type ContainerStats struct {
	Crashes        int
	CrashesClearAt *time.Time

	// Последнее событие: чем именно занят контейнер прямо сейчас.
	LastEvent       string
	LastEventDetail string
	LastEventAt     *time.Time
}

// Severity - пять уровней entity.Severity бэкенда как строки, от самого
// срочного к самому тихому. AlertSeverities в web/present.go задаёт порядок
// фильтра теми же значениями.
const (
	SeverityCritical = "critical"
	SeverityError    = "error"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
	SeverityDebug    = "debug"
)

// Alert - алерт бэкенда, дополненный данными о цели.
//
// У алерта нет ни типа, ни жизненного цикла: тип выводится из вида цели, а
// вместо статуса «активен/закрыт» есть только факт отправки в Telegram
// (Notified). Ничего из этого не додумывается - показывается ровно то, что
// записано.
type Alert struct {
	ID             int64
	TargetID       string
	TargetLabel    string
	TargetKind     Kind
	Severity       string // одно из значений Severity*
	Message        string
	SentToTelegram bool
	CreatedAt      time.Time
}

// AlertCounts - счётчики алертов за последние сутки.
// Critical объединяет SeverityCritical и SeverityError - оба «срочно, смотри
// сейчас» с точки зрения дашборда; Warning - только SeverityWarning.
type AlertCounts struct {
	Day      int
	Critical int
	Warning  int
}

// ContainerDetail - страница одного контейнера.
type ContainerDetail struct {
	Target     TargetSummary
	Events     []ContainerEvent
	Activity   []ActivityPoint
	LogMatches []LogMatch

	// Incidents - интервалы простоя за сутки, выведенные из тех же событий,
	// что лежат в Events. Незакрытые (без записи о возврате) тоже здесь:
	// пробел в данных надо видеть, а не прятать.
	Incidents []Incident
}

// ContainerEvent - событие Docker по контейнеру.
type ContainerEvent struct {
	Event      string
	Detail     string
	OccurredAt time.Time
}

// ActivityPoint - почасовая активность контейнера.
//
// Для контейнера нет ни latency, ни аптайма в процентах: его никто не пингует.
type ActivityPoint struct {
	Hour       time.Time
	Events     int
	LogMatches int
}

type LogMatch struct {
	ID          int64
	TargetID    string
	TargetLabel string
	TargetKind  Kind
	Severity    string
	Message     string
	CreatedAt   time.Time
}

type LogMatchPoint struct {
	Hour    time.Time
	Urgent  int
	Warning int
	Quiet   int
}

type LogMatchReport struct {
	Matches []LogMatch
	Hourly  []LogMatchPoint
	Future  int
}

// LatencyPoint - почасовая точка графика на сводке.
// Оба поля nil, если за этот час проверок не было: разрыв в графике честнее
// нарисованного нуля.
type LatencyPoint struct {
	Hour      time.Time
	P95MS     *int
	UptimePct *float64
}

type ListQuery struct {
	TargetIDs []string
	Host      bool
	Severity  string
	Since     time.Time
	Skip      int
	Limit     int
}

type Page[T any] struct {
	Items []T
	Total int
	Skip  int
}

type HistoryQuery struct {
	Kind     Kind
	Severity string
	Target   string
	Page     int
}

type Recent struct {
	Alerts     []Alert
	LogMatches []LogMatch
	Incidents  []Incident
}

const (
	HostLevelOK       = "ok"
	HostLevelWarning  = "warning"
	HostLevelCritical = "critical"
)

type HostMetric struct {
	Percent float64
	Used    uint64
	Total   uint64
	Level   string
}

type Host struct {
	Hostname  string
	SampledAt *time.Time
	CPU       *HostMetric
	Memory    *HostMetric
	Disk      *HostMetric
}
