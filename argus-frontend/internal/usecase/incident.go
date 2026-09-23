package usecase

import (
	"context"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// incidentWindow - за какой период дашборд показывает инциденты.
const incidentWindow = 24 * time.Hour

// opensDowntime - событие, с которого начинается простой контейнера.
// "restart" сюда не входит: он простой закрывает, а не открывает.
func opensDowntime(event string) bool {
	switch event {
	case "die", "oom", "kill":
		return true
	default:
		return false
	}
}

// closesDowntime - событие, закрывающее простой.
//
// "start" - основной случай: им argus-backend отмечает возврат контейнера и
// после restart-политики, и после ручного docker start. "destroy" закрывает
// простой тоже, только иначе: контейнера больше нет, и считать дальше не по
// чему.
//
// Пока в этом списке не было "start", любая пара die -> start оставалась
// незакрытой, и дашборд писал «return not recorded» у вернувшегося
// контейнера (TestContainerDieWithStartIsMeasured).
func closesDowntime(event string) bool {
	switch event {
	case "start", "restart", "destroy":
		return true
	default:
		return false
	}
}

// Incidents отдаёт интервалы простоя по всем целям за последние сутки, новые
// сверху, вместе со сводкой.
//
// ponytail: цели, пробы и события тянутся по одной цели за запрос - тот же
// N+1, что уже делает TargetStatus. Партиал инцидентов поллится в три раза
// реже страницы, чтобы это не удваивало трафик к бэкенду; если целей станет
// много, сюда нужен один общий запрос на бэкенде, а не кеш здесь.
func (uc *UseCase) Incidents(ctx context.Context) ([]entity.Incident, entity.IncidentStats, error) {
	targets, err := uc.backend.Targets(ctx)
	if err != nil {
		return nil, entity.IncidentStats{}, notReady(err)
	}

	since := time.Now().Add(-incidentWindow)

	var out []entity.Incident

	for _, t := range targets {
		found, err := uc.incidentsFor(ctx, t)
		if err != nil {
			return nil, entity.IncidentStats{}, err
		}

		out = append(out, withinWindow(found, since)...)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })

	return out, incidentStats(out), nil
}

func (uc *UseCase) incidentsFor(ctx context.Context, t entity.Target) ([]entity.Incident, error) {
	switch t.Kind {
	case entity.KindProbe:
		probes, err := uc.backend.Probes(ctx, t.ID)
		if err != nil {
			return nil, notReady(err)
		}

		return probeIncidents(t, probes), nil

	case entity.KindContainer:
		events, err := uc.backend.Events(ctx, t.ID)
		if err != nil {
			return nil, notReady(err)
		}

		return containerIncidents(t, events), nil
	}

	return nil, nil
}

// probeIncidents собирает интервалы из серий неудачных проб.
//
// Серия считается по всей истории, а не по окну: интервал, начавшийся до
// полуночи и закрытый утром, должен иметь настоящее начало, а не обрезанное
// границей периода.
func probeIncidents(t entity.Target, probes []entity.ProbeOutcome) []entity.Incident {
	probes = sortedByTime(probes, func(p entity.ProbeOutcome) time.Time { return p.CheckedAt })

	var (
		out     []entity.Incident
		current *entity.Incident
	)

	for _, p := range probes {
		if !p.Success {
			if current == nil {
				current = &entity.Incident{
					TargetID:    t.ID,
					TargetLabel: t.Label,
					Kind:        t.Kind,
					StartedAt:   p.CheckedAt,
					Cause:       "probe failed",
					State:       entity.IncidentOpen,
				}
			}

			current.LastKnownAt = p.CheckedAt

			continue
		}

		if current != nil {
			endedAt := p.CheckedAt
			current.EndedAt = &endedAt
			current.State = entity.IncidentClosed
			out = append(out, *current)
			current = nil
		}
	}

	// Серия, дошедшая до последней пробы, продолжается: проверки идут, и
	// длительность считается до последней неудачи - не до "сейчас".
	if current != nil {
		out = append(out, *current)
	}

	return out
}

// containerIncidents собирает интервалы из событий Docker.
//
// Событие падения при уже открытом интервале ничего нового не открывает:
// упасть, не вернувшись, нельзя. Это не мелочь - на одну остановку Docker
// присылает пару kill + die, и считать её двумя простоями значит выдумать
// второй (TestKillAndDiePairIsOneOutage). Новый интервал начинается только
// после записи о возврате.
func containerIncidents(t entity.Target, events []entity.ContainerEvent) []entity.Incident {
	events = sortedByTime(events, func(e entity.ContainerEvent) time.Time { return e.OccurredAt })

	var (
		out     []entity.Incident
		current *entity.Incident
	)

	for _, e := range events {
		switch {
		case opensDowntime(e.Event):
			// Уже лежит: это второе событие той же остановки, у неё остаётся
			// первая причина. Двигаем только последнее наблюдение.
			if current != nil {
				current.LastKnownAt = e.OccurredAt

				continue
			}

			current = &entity.Incident{
				TargetID:    t.ID,
				TargetLabel: t.Label,
				Kind:        t.Kind,
				StartedAt:   e.OccurredAt,
				LastKnownAt: e.OccurredAt,
				Cause:       e.Event,
				State:       entity.IncidentUnclosed,
			}

		case closesDowntime(e.Event) && current != nil:
			endedAt := e.OccurredAt
			current.EndedAt = &endedAt
			current.State = entity.IncidentClosed
			out = append(out, *current)
			current = nil
		}
	}

	if current != nil {
		out = append(out, *current)
	}

	return out
}

// returnsSince считает, сколько раз контейнер лёг и снова поднялся начиная с
// since. Это и есть «перезапустился»: возврат, а не событие с именем restart.
//
// Считается по тем же интервалам, что рисует таймлайн, поэтому карточка и
// таймлайн не могут разойтись. Цель здесь не нужна - в счётчик идут только
// времена, а не подписи, поэтому передаётся пустая.
//
// Упавший и не вернувшийся контейнер в счётчик не попадает: он не
// перезапустился, он лежит. Одиночный restart без предшествующего падения -
// тоже нет: без записи о падении утверждать возврат нечем.
func returnsSince(events []entity.ContainerEvent, since time.Time) int {
	var n int

	for _, i := range containerIncidents(entity.Target{}, events) {
		if i.State != entity.IncidentClosed || i.EndedAt == nil {
			continue
		}

		if !i.EndedAt.Before(since) {
			n++
		}
	}

	return n
}

// withinWindow оставляет интервалы, которые пересекаются с периодом:
// начались в нём или в нём же закончились.
func withinWindow(incidents []entity.Incident, since time.Time) []entity.Incident {
	out := make([]entity.Incident, 0, len(incidents))

	for _, i := range incidents {
		switch {
		case !i.StartedAt.Before(since):
			out = append(out, i)
		case i.EndedAt != nil && !i.EndedAt.Before(since):
			out = append(out, i)
		case i.State == entity.IncidentOpen && !i.LastKnownAt.Before(since):
			out = append(out, i)
		}
	}

	return out
}

// sortedByTime возвращает копию, отсортированную по возрастанию времени.
//
// Копию - потому что тот же слайс событий уже разложен для ленты на странице
// контейнера, и сортировка на месте перетряхнула бы её у вызывающего: лента
// показывала бы самые старые события вместо новых
// (TestIncidentDerivationKeepsEventOrder). Сортировка устойчивая: у событий
// одной остановки метки времени различаются на доли секунды, и переставлять
// совпавшие местами незачем.
func sortedByTime[T any](items []T, at func(T) time.Time) []T {
	out := append([]T(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return at(out[i]).Before(at(out[j])) })

	return out
}

func incidentStats(incidents []entity.Incident) entity.IncidentStats {
	stats := entity.IncidentStats{Count: len(incidents)}

	for _, i := range incidents {
		d, ok := i.Duration()
		if !ok {
			stats.Unclosed++

			continue
		}

		stats.Total += d

		if d > stats.Longest {
			stats.Longest = d
		}
	}

	return stats
}
