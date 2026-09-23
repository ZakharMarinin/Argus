package entity

import "time"

// IncidentState - что именно известно про интервал простоя.
//
// Состояний три, а не два, потому что «вернулся» и «неизвестно, вернулся ли» -
// разные факты, и путать их нельзя: по одним данным простой можно измерить,
// по другим только зафиксировать начало.
type IncidentState string

const (
	// IncidentClosed - известны и начало, и конец: простой измерен.
	IncidentClosed IncidentState = "closed"

	// IncidentOpen - простой продолжается, и это подтверждено свежими
	// наблюдениями (неудачные пробы идут до последней записи).
	IncidentOpen IncidentState = "open"

	// IncidentUnclosed - начало известно, записи о возврате нет: контейнер
	// упал, а start/restart/destroy после этого не записан. Длительность
	// такого интервала неизвестна, и выдумывать её нельзя.
	IncidentUnclosed IncidentState = "unclosed"
)

// Incident - один интервал, в котором цель не работала.
//
// Инциденты не хранятся - они выводятся из проб и событий Docker. Поэтому у
// них нет ни id, ни жизненного цикла: это срез того, что записал collector.
type Incident struct {
	TargetID    string
	TargetLabel string
	Kind        Kind

	// StartedAt - первое наблюдение простоя: неудачная проба или событие
	// die/oom/kill. Настоящий момент падения лежит где-то между этим
	// наблюдением и предыдущим удачным - точнее данных нет.
	StartedAt time.Time

	// EndedAt - наблюдение, закрывшее простой: удачная проба или
	// start/restart/destroy. nil у open и unclosed.
	EndedAt *time.Time

	// LastKnownAt - последнее наблюдение, подтверждающее простой. У open это
	// последняя неудачная проба; по ней и считается длительность, чтобы
	// счётчик не рос сам по себе, когда collector перестал проверять.
	LastKnownAt time.Time

	// Cause - чем простой начался: die, oom, kill, probe failed.
	Cause string

	State IncidentState
}

// Duration возвращает измеренную длительность простоя и признак того, что её
// было из чего посчитать. У IncidentUnclosed возвращается false: конца
// интервала в данных нет, и любое число здесь было бы выдумкой.
func (i Incident) Duration() (time.Duration, bool) {
	if i.State == IncidentUnclosed {
		return 0, false
	}

	end := i.LastKnownAt
	if i.EndedAt != nil {
		end = *i.EndedAt
	}

	if end.Before(i.StartedAt) {
		return 0, false
	}

	return end.Sub(i.StartedAt), true
}

// IncidentStats - сводка по инцидентам за период.
type IncidentStats struct {
	// Count - сколько интервалов простоя попало в период, включая незакрытые.
	Count int

	// Total - суммарный измеримый простой. Незакрытые интервалы в сумму не
	// входят: их длительность неизвестна, и прибавлять к сумме ноль значило
	// бы занижать её молча.
	Total time.Duration

	// Longest - самый долгий измеримый простой.
	Longest time.Duration

	// Unclosed - сколько интервалов остались без записи о возврате. Это
	// счётчик пробела в данных, и он выводится рядом с суммой.
	Unclosed int
}
