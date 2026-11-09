package main

import (
	"fmt"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/pprof"
)

// graphTools are the controls above the profile graph: a search, a
// focus on one function, hiding the runtime and the least share a
// function needs to be drawn.
type graphTools struct {
	Find    widget.Editor
	query   string
	matches []string
	match   int

	FocusButton widget.Clickable
	// focus keeps only the samples whose stacks pass through it.
	focus string

	HideRuntime widget.Bool

	// Flame draws the pane as a flame graph instead of a call graph.
	Flame bool

	Threshold gui.Select
	// share is the least share of the samples; 0 is the default.
	share float64

	// diff is the profile less the base profile.
	diff, diffOf, diffBase *pprof.Profile
	// view is the profile filtered by focus and HideRuntime.
	view      *pprof.Profile
	viewOf    *pprof.Profile
	viewFocus string
	viewHide  bool
}

// graphShares are the choices of the least share of the samples a
// function in the whole graph needs.
var graphShares = []struct {
	label string
	share float64
}{
	{"≥ 0.1%", 0.001},
	{"≥ 0.5%", fullNodeShare},
	{"≥ 1%", 0.01},
	{"≥ 5%", 0.05},
}

// isRuntime reports whether name is in the Go runtime, which Hide
// runtime leaves out so its time goes to the code that called it.
func isRuntime(name string) bool {
	for _, prefix := range []string{"runtime.", "runtime/", "internal/runtime/"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// graphProfile is the profile the graphs draw: the loaded one with the
// focus and the runtime hiding applied.
func (ui *FileUI) graphProfile() *pprof.Profile {
	t := &ui.GraphTools
	if ui.Profile == nil {
		return nil
	}
	prof := ui.Profile
	if ui.BaseProfile != nil {
		if t.diffOf != ui.Profile || t.diffBase != ui.BaseProfile {
			t.diff, _ = ui.Profile.Minus(ui.BaseProfile)
			t.diffOf, t.diffBase = ui.Profile, ui.BaseProfile
		}
		if t.diff != nil {
			prof = t.diff
		}
	}
	hide := t.HideRuntime.Value
	if t.focus == "" && !hide {
		return prof
	}
	if t.view == nil || t.viewOf != prof || t.viewFocus != t.focus || t.viewHide != hide {
		var hidden func(string) bool
		if hide {
			hidden = isRuntime
		}
		t.view, t.viewOf, t.viewFocus, t.viewHide = prof.Filter(t.focus, hidden), prof, t.focus, hide
	}
	return t.view
}

// layoutGraphTools draws the controls above g, the whole graph.
func (ui *FileUI) layoutGraphTools(gtx layout.Context, g *callGraph) layout.Dimensions {
	th := ui.Theme
	t := &ui.GraphTools
	t.Find.SingleLine, t.Find.Submit = true, true

	// The search marks the functions in the graph whose names contain
	// it; Enter scrolls to the next.
	for {
		ev, ok := t.Find.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok && len(t.matches) > 0 {
			t.match = (t.match + 1) % len(t.matches)
			g.target = t.matches[t.match]
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &t.Find, Name: key.NameEscape})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			t.Find.SetText("")
		}
	}
	if query := strings.ToLower(strings.TrimSpace(t.Find.Text())); query != t.query || g.matches == nil {
		t.query, t.matches, t.match = query, nil, 0
		g.matches = map[string]bool{}
		if query != "" && g.graph != nil {
			for _, n := range g.graph.Nodes {
				if strings.Contains(strings.ToLower(n.ID), query) {
					t.matches = append(t.matches, n.ID)
					g.matches[n.ID] = true
				}
			}
			if len(t.matches) > 0 {
				g.target = t.matches[0]
			}
		}
	}

	focusText := "Focus"
	for t.FocusButton.Clicked(gtx) {
		if t.focus != "" {
			t.focus = ""
		} else if tab := ui.activeTab(); tab != nil {
			t.focus = tab.Name
		}
	}
	if t.focus != "" {
		focusText = "Focused on " + t.focus + " ×"
	}

	if i, ok := t.Threshold.Update(gtx); ok {
		t.share = graphShares[i].share
	}
	current := 0
	for i, s := range graphShares {
		if s.share == g.share() {
			current = i
		}
	}
	options := make([]string, len(graphShares))
	for i, s := range graphShares {
		options[i] = s.label
	}

	icon, description := FullscreenIcon, "Fill the window"
	if ui.GraphFull {
		icon, description = FullscreenExitIcon, "Back beside the code"
	}
	gap := layout.Spacer{Width: 8}.Layout
	search := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(260))
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return gui.SearchField(gtx, th, gtx.Focused(&t.Find), gui.Editor(th, &t.Find, "Find in graph"))
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if t.query == "" {
					return layout.Dimensions{}
				}
				text := "none"
				if len(t.matches) > 0 {
					text = fmt.Sprintf("%d of %d", t.match+1, len(t.matches))
				}
				return layout.Inset{Left: 6}.Layout(gtx, th.Muted(text, 0.85).Layout)
			}),
		)
	}
	controls := []layout.FlexChild{
		layout.Rigid(gui.Button{Theme: th, Clickable: &t.FocusButton, Text: focusText}.Layout),
		layout.Rigid(gap),
		layout.Rigid(gui.Checkbox{Theme: th, Value: &t.HideRuntime, Label: "Hide runtime"}.Layout),
		layout.Rigid(gap),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if t.Flame {
				return layout.Dimensions{}
			}
			t.Threshold.Width = 80
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(90))
			return t.Threshold.Layout(gtx, th, options, current)
		}),
		layout.Flexed(1, gap),
		layout.Rigid(gui.IconButton{Theme: th, Clickable: &ui.GraphFullBtn, Tip: &ui.GraphFullTip, Icon: icon, Description: description}.Layout),
	}
	// Each row keeps one line high however narrow the pane, cutting off
	// what doesn't fit; a narrow pane puts the search on its own row.
	row := func(gtx layout.Context, children ...layout.FlexChild) layout.Dimensions {
		gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(32))
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	}
	return layout.Inset{Top: 8, Bottom: 6, Left: 14, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if gtx.Constraints.Max.X >= gtx.Dp(560) {
			return row(gtx, append([]layout.FlexChild{layout.Flexed(1, search), layout.Rigid(gap)}, controls...)...)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, layout.Flexed(1, search))
			}),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return row(gtx, controls...)
			}),
		)
	})
}
