package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

type trendMonth struct {
	year, month int
	net         float64
	hasNet      bool
	ferie       *float64
	par         *float64
	exFs        *float64
}

func (m trendMonth) label() string {
	return fmt.Sprintf("%s '%02d", time.Month(m.month).String()[:3], m.year%100)
}

// trendMonths uses only locally extracted data whose printed PDF period matches
// its payslip key. Missing months remain gaps instead of implying zero values.
func (a *App) trendMonths() []trendMonth {
	byMonth := make(map[[2]int]PayslipData)
	var dates [][2]int
	for _, p := range a.payslips {
		if p.Year < 1 || p.Month < 1 || p.Month > 12 {
			continue
		}
		d, ok := a.details[p.Key]
		if !ok {
			continue
		}
		y, m, ok := d.PeriodYM()
		if !ok || y != p.Year || m != p.Month {
			continue
		}
		key := [2]int{p.Year, p.Month}
		if _, exists := byMonth[key]; !exists {
			dates = append(dates, key)
		}
		byMonth[key] = d
	}
	if len(dates) == 0 {
		return nil
	}
	sort.Slice(dates, func(i, j int) bool {
		if dates[i][0] != dates[j][0] {
			return dates[i][0] < dates[j][0]
		}
		return dates[i][1] < dates[j][1]
	})
	first, last := dates[0], dates[len(dates)-1]
	var months []trendMonth
	for t := time.Date(first[0], time.Month(first[1]), 1, 0, 0, 0, 0, time.UTC); !t.After(time.Date(last[0], time.Month(last[1]), 1, 0, 0, 0, 0, time.UTC)); t = t.AddDate(0, 1, 0) {
		m := trendMonth{year: t.Year(), month: int(t.Month())}
		if d, ok := byMonth[[2]int{m.year, m.month}]; ok {
			m.net, m.hasNet = float64(d.NetCents)/100, d.HasNet
			if d.Ferie != nil {
				v := d.Ferie.Remaining
				m.ferie = &v
			}
			if d.PAR != nil {
				v := d.PAR.Remaining
				m.par = &v
			}
			if d.ExFs != nil {
				v := d.ExFs.Remaining
				m.exFs = &v
			}
		}
		months = append(months, m)
	}
	return months
}

func (a *App) tabTrends(c *ui.Context) {
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(10).Children(func() {
			months := a.trendMonths()
			if len(months) == 0 {
				a.card(c).Padding(16).Children(func() {
					ui.Column(c).Gap(8).Children(func() {
						ui.Text(c, "Build your payslip history").FontSize(14).FontWeight(500).TextColor(ui.Hex("#f2f2f2"))
						ui.Text(c, "Save payslips locally to chart take-home pay and leave balances.").FontSize(12).TextColor(ui.Hex("#7a7a7a")).LineHeight(1.4)
						if a.UndownloadedCount() > 0 {
							a.primaryButton(c, fmt.Sprintf("Download all (%d)", a.UndownloadedCount()), a.DownloadAll)
						}
					})
				})
				return
			}
			a.incomeChart(c, months)
			a.leaveChart(c, months)
		})
	})
}

func bandAxis(months []trendMonth) (chart.Band, []chart.Tick) {
	labels := make([]string, len(months))
	for i, m := range months {
		labels[i] = m.label()
	}
	band := chart.NewBand(labels...).Padding(0.25, 0.08)
	var ticks []chart.Tick
	for i, m := range months {
		if i == 0 || m.month == 1 || m.month == 4 || m.month == 7 || m.month == 10 || i == len(months)-1 {
			ticks = append(ticks, chart.Tick{At: band.At(i), Value: float64(i), Label: m.label()})
		}
	}
	return band, ticks
}

func (a *App) incomeChart(c *ui.Context, months []trendMonth) {
	type point struct {
		index int
		value float64
	}
	var points []point
	var values []float64
	for i, m := range months {
		if m.hasNet {
			points = append(points, point{i, m.net})
			values = append(values, m.net)
		}
	}
	a.card(c).Padding(12).Children(func() {
		ui.Column(c).Gap(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Take-home pay").FontSize(13).FontWeight(500).TextColor(ui.Hex("#f2f2f2")).Grow(1)
				ui.Text(c, "monthly · EUR").FontSize(10).TextColor(ui.Hex("#7a7a7a"))
			})
			if len(points) == 0 {
				ui.Text(c, "No verified net amounts in the saved PDFs yet.").FontSize(11).TextColor(ui.Hex("#7a7a7a"))
				return
			}
			x, bottom := bandAxis(months)
			_, maxY := chart.Extent(values...)
			y := chart.NewLinear(0, maxY).Nice(4)
			y.Format = euroAxisLabel
			theme := c.Theme()
			chart.Plot(c, chart.Axes{Left: y.Ticks(4), Bottom: bottom, Label: chartLabel(c)}, func(area ui.Element) {
				area.Draw(func(p *ui.Painter, r ui.Rect) {
					for _, tick := range y.Ticks(4) {
						p.Line(r.X, chart.Y(r, tick.At), r.X+r.W, chart.Y(r, tick.At), 1, theme.Border)
					}
				})
				for _, v := range points {
					label := fmt.Sprintf("%s · €%s", months[v.index].label(), money(v.value))
					chart.Rect(c, x.Band(v.index), y.Span(0, v.value)).Radius(2).Background(theme.Accent).Tooltip(label)
				}
			}).Height(132).Gap(5).Label("Monthly take-home pay").Description("Net pay extracted from locally saved payslips")
		})
	})
}

func (a *App) leaveChart(c *ui.Context, months []trendMonth) {
	a.card(c).Padding(12).Children(func() {
		ui.Column(c).Gap(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Leave balances").FontSize(13).FontWeight(500).TextColor(ui.Hex("#f2f2f2")).Grow(1)
				ui.Text(c, "hours remaining").FontSize(10).TextColor(ui.Hex("#7a7a7a"))
			})
			x, bottom := bandAxis(months)
			series := []struct {
				name  string
				color ui.Color
				value func(trendMonth) *float64
			}{
				{"Vacation", ui.Hex("#e8830c"), func(m trendMonth) *float64 { return m.ferie }},
				{"PAR", ui.Hex("#6ea8fe"), func(m trendMonth) *float64 { return m.par }},
				{"Ex-holidays", ui.Hex("#2ec4b0"), func(m trendMonth) *float64 { return m.exFs }},
			}
			var all []float64
			for _, s := range series {
				for _, m := range months {
					if v := s.value(m); v != nil {
						all = append(all, *v)
					}
				}
			}
			if len(all) == 0 {
				ui.Text(c, "No verified leave balances in the saved PDFs yet.").FontSize(11).TextColor(ui.Hex("#7a7a7a"))
				return
			}
			_, maxY := chart.Extent(all...)
			y := chart.NewLinear(0, maxY).Nice(4)
			y.Format = func(v float64) string { return fmt.Sprintf("%.0f", v) }
			theme := c.Theme()
			chart.Plot(c, chart.Axes{Left: y.Ticks(4), Bottom: bottom, Label: chartLabel(c)}, func(area ui.Element) {
				area.Draw(func(p *ui.Painter, r ui.Rect) {
					for _, tick := range y.Ticks(4) {
						p.Line(r.X, chart.Y(r, tick.At), r.X+r.W, chart.Y(r, tick.At), 1, theme.Border)
					}
					for _, s := range series {
						ys := make([]float32, len(months))
						for i, m := range months {
							v := s.value(m)
							if v == nil {
								ys[i] = float32(math.NaN())
							} else {
								ys[i] = y.At(*v)
							}
						}
						xs := make([]float32, len(months))
						for i := range months {
							xs[i] = x.At(i)
						}
						p.StrokePath(chart.Line(r, xs, ys, chart.CurveLinear), 2, s.color)
					}
				})
				for _, s := range series {
					for i, m := range months {
						v := s.value(m)
						if v == nil {
							continue
						}
						chart.Point(c, x.At(i), y.At(*v), 6, 6).Radius(3).Background(s.color).
							Tooltip(fmt.Sprintf("%s · %s: %.1f h", m.label(), s.name, *v))
					}
				}
			}).Height(132).Gap(5).Label("Monthly leave balances").Description("Vacation, PAR and ex-holiday hours remaining on payslips")
			ui.Row(c).Gap(12).Children(func() {
				for _, s := range series {
					has := false
					for _, m := range months {
						if s.value(m) != nil {
							has = true
							break
						}
					}
					if has {
						ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
							ui.Box(c).Size(7, 7).Radius(4).Background(s.color)
							ui.Text(c, s.name).FontSize(10).TextColor(ui.Hex("#a8a8a8"))
						})
					}
				}
			})
		})
	})
}

func chartLabel(c *ui.Context) func(chart.Side, chart.Tick) {
	return func(_ chart.Side, t chart.Tick) {
		ui.Text(c, t.Label).FontSize(9).TextColor(ui.Hex("#7a7a7a")).NoWrap()
	}
}

func euroAxisLabel(v float64) string {
	switch {
	case v >= 1000:
		return fmt.Sprintf("€%.1fk", v/1000)
	default:
		return fmt.Sprintf("€%.0f", v)
	}
}

func money(v float64) string {
	whole := int64(math.Round(v * 100))
	return PayslipData{NetCents: whole}.Net()
}
