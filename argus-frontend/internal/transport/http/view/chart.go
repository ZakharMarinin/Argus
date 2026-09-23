package view

// ChartData - ответ для графика.
//
// Точки отдаются JSON'ом, а не HTML: карточка с графиком не участвует в
// htmx-поллинге, иначе Chart.js пересоздавался бы каждые 10 секунд.
//
// Формат намеренно общий. На сводке это latency и доступность uptime-таргетов,
// на странице контейнера - события Docker и совпадения в логах, на сводке
// ещё и совпадения по важности: контейнер
// никто не пингует, и рисовать ему latency было бы выдумкой.
type ChartData struct {
	Labels []string      `json:"labels"`
	Series []ChartSeries `json:"series"`
	// Empty говорит фронтенду, что данных за период нет вовсе, и вместо
	// пустых осей надо показать сообщение.
	Empty   bool `json:"empty"`
	Stacked bool `json:"stacked"`
}

// ChartSeries - один ряд графика.
//
// Values допускает null: час без проверок - это разрыв линии, а не ноль.
type ChartSeries struct {
	Label string `json:"label"`
	// Color - имя токена из палитры (accent, up, warn, down), а не сам цвет:
	// график должен перекрашиваться вместе с темой.
	Color string `json:"color"`
	// Axis - "left" или "right"; второй ряд обычно живёт на своей шкале.
	Axis string `json:"axis"`
	// Style - "area" для заливки под линией, "line" для простой линии,
	// "bar" для счётчика событий: дискретную величину линией рисовать нельзя.
	Style string `json:"style"`
	// Unit подписывает деления шкалы: " ms", "%", "".
	Unit   string     `json:"unit"`
	Values []*float64 `json:"values"`
}
