package handlers_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/transport/http/view"
)

// TestOverviewBoardRendersEveryBlockOnce: the saved layout refers to blocks by
// their id, so each one must be on the page exactly once, with a width the
// board understands. Otherwise app.js would move or lose the wrong block.
func TestOverviewBoardRendersEveryBlockOnce(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/").Body.String()

	sizes := map[string]bool{}
	for _, s := range view.BlockSizes() {
		sizes[strconv.Itoa(s.Cols)] = true
	}

	for _, b := range view.OverviewBlocks() {
		t.Run(b.ID, func(t *testing.T) {
			re := regexp.MustCompile(`<div class="block" data-block="` + b.ID + `" data-size="(\d+)" data-default-size="(\d+)"[^>]*>`)
			found := re.FindAllStringSubmatch(body, -1)
			if len(found) != 1 {
				t.Fatalf("block is rendered %d times, expected once", len(found))
			}
			if !sizes[found[0][1]] || found[0][1] != found[0][2] {
				t.Errorf("width %q / default %q is not one of the board sizes", found[0][1], found[0][2])
			}

			pinned := strings.Contains(found[0][0], "data-pinned")
			if pinned != b.Pinned {
				t.Errorf("data-pinned is %v, expected %v", pinned, b.Pinned)
			}
		})
	}
}

// TestOverallStatusCannotBeHidden: without the overall state the overview no
// longer answers "is something broken?", so the summary has no hide button.
func TestOverallStatusCannotBeHidden(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/").Body.String()

	start := strings.Index(body, `data-block="summary"`)
	end := strings.Index(body[start:], `class="block__body"`)
	if start < 0 || end < 0 {
		t.Fatal("summary block not found")
	}
	bar := body[start : start+end]

	if strings.Contains(bar, "data-layout-hide") {
		t.Error("the overall status can be hidden")
	}
	if strings.Contains(body, `data-layout-show="summary"`) {
		t.Error("the overall status is offered in the hidden tray")
	}

	hide := strings.Count(body, "data-layout-hide")
	if want := len(view.OverviewBlocks()) - 1; hide != want {
		t.Errorf("%d hide buttons, expected %d", hide, want)
	}
}

// TestBoardControlsWaitForJS: without JS nothing can be arranged, so the
// controls are rendered hidden and app.js reveals them.
func TestBoardControlsWaitForJS(t *testing.T) {
	body := get(t, newServer(t, filledUseCase()), "/").Body.String()

	for _, tag := range []string{
		`<button type="button" class="btn btn--ghost btn--customize" data-layout-edit aria-pressed="false" hidden>`,
		`<div class="board__editbar" data-layout-bar hidden>`,
	} {
		if !strings.Contains(body, tag) {
			t.Errorf("not rendered hidden: %s", tag)
		}
	}

	// Other pages have no board and no Customize button.
	history := get(t, newServer(t, filledUseCase()), "/history/alerts").Body.String()
	if strings.Contains(history, "data-layout-edit") {
		t.Error("Customize is offered on a page without a board")
	}
}

// TestUptimeTargetsNeedingAttentionComeFirst: a down target must not sit in the
// middle of the list. Within one status the backend order stays.
func TestUptimeTargetsNeedingAttentionComeFirst(t *testing.T) {
	p := view.Presenter{Now: func() time.Time { return testNow }}
	flaky := &entity.ProbeStats{Checks24h: 10, Successes24h: 9}
	probes := []entity.TargetSummary{
		{ID: "1", Label: "aaa-up", Kind: entity.KindProbe, Status: entity.StatusUp, Probe: &entity.ProbeStats{}},
		{ID: "2", Label: "bbb-unknown", Kind: entity.KindProbe, Status: entity.StatusUnknown},
		{ID: "3", Label: "ccc-flaky", Kind: entity.KindProbe, Status: entity.StatusUp, Probe: flaky},
		{ID: "4", Label: "ddd-down", Kind: entity.KindProbe, Status: entity.StatusDown, Probe: &entity.ProbeStats{}},
		{ID: "5", Label: "eee-up", Kind: entity.KindProbe, Status: entity.StatusUp, Probe: &entity.ProbeStats{}},
	}

	rows := p.Overview(probes, entity.AlertCounts{}, entity.LogMatchReport{}).Targets.Targets

	order := make([]string, 0, len(rows))
	for _, r := range rows {
		order = append(order, r.Name)
	}

	if got := strings.Join(order, ","); got != "ddd-down,ccc-flaky,bbb-unknown,aaa-up,eee-up" {
		t.Errorf("order %s", got)
	}
}

// TestFolderHoldsOnlyQuietContainers: folding the containers block must never
// hide one that is down, crashing or noisy in the logs.
func TestFolderHoldsOnlyQuietContainers(t *testing.T) {
	p := view.Presenter{Now: func() time.Time { return testNow }}
	targets := []entity.TargetSummary{
		{ID: "1", Label: "calm", Kind: entity.KindContainer, Status: entity.StatusUp, Container: &entity.ContainerStats{}},
		{ID: "2", Label: "noisy", Kind: entity.KindContainer, Status: entity.StatusUp, Container: &entity.ContainerStats{}},
		{ID: "3", Label: "down", Kind: entity.KindContainer, Status: entity.StatusDown, Container: &entity.ContainerStats{}},
		{ID: "4", Label: "quiet", Kind: entity.KindContainer, Status: entity.StatusUp, Container: &entity.ContainerStats{}},
	}
	noisy := []entity.LogMatch{{TargetID: "2"}, {TargetID: "2"}, {TargetID: "2"}}
	report := entity.LogMatchReport{Matches: noisy}

	list := p.Overview(targets, entity.AlertCounts{}, report).Containers

	folded := make([]string, 0, len(list.Folded()))
	for _, c := range list.Folded() {
		folded = append(folded, c.Name)
	}

	if strings.Join(folded, ",") != "calm,quiet" {
		t.Errorf("folded %v, wanted calm,quiet", folded)
	}

	if list.FoldedNote() != "2 up" {
		t.Errorf("note %q", list.FoldedNote())
	}

	// Everything needs attention: there is nothing to fold away.
	for _, id := range []string{"1", "4"} {
		report.Matches = append(report.Matches, entity.LogMatch{TargetID: id}, entity.LogMatch{TargetID: id}, entity.LogMatch{TargetID: id})
	}
	if n := len(p.Overview(targets, entity.AlertCounts{}, report).Containers.Folded()); n != 0 {
		t.Errorf("%d containers folded, all need attention", n)
	}
}
