package view

// ChartData is the response for a chart.
//
// Points are returned as JSON, not HTML: the chart card doesn't take part in
// htmx polling, otherwise Chart.js would be recreated every 10 seconds.
//
// The format is deliberately generic. On the overview it is latency and
// availability of uptime targets, on the container page it is Docker events
// and log matches, and on the overview also matches by severity: nothing
// pings a container, and drawing latency for it would be made up.
type ChartData struct {
	Labels []string      `json:"labels"`
	Series []ChartSeries `json:"series"`
	// Empty tells the frontend that there is no data for the period at all, and
	// a message should be shown instead of empty axes.
	Empty   bool `json:"empty"`
	Stacked bool `json:"stacked"`
}

// ChartSeries is a single chart series.
//
// Values may contain null: an hour without checks is a gap in the line, not zero.
type ChartSeries struct {
	Label string `json:"label"`
	// Color is the name of a palette token (accent, up, warn, down), not the colour
	// itself: the chart must change colour together with the theme.
	Color string `json:"color"`
	// Axis is "left" or "right"; the second series usually lives on its own scale.
	Axis string `json:"axis"`
	// Style is "area" for a fill under the line, "line" for a plain line,
	// "bar" for an event counter: a discrete quantity must not be drawn as a line.
	Style string `json:"style"`
	// Unit labels the scale ticks: " ms", "%", "".
	Unit   string     `json:"unit"`
	Values []*float64 `json:"values"`
}
