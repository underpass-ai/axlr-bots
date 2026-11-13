package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// scrapeTimeout bounds reading the overview at one scrape.
const scrapeTimeout = 10 * time.Second

// OverviewCollector reads the overview at each scrape and turns it into
// gauges: how many nodes, bots and tasks are in each state now, and
// what today has seen so far. The "today" gauges go back to zero at
// midnight on the control plane's clock, so they are gauges, not
// counters.
type OverviewCollector struct {
	source          OverviewSource
	nodes           *prometheus.Desc
	bots            *prometheus.Desc
	tasks           *prometheus.Desc
	tasksToday      *prometheus.Desc
	executionsToday *prometheus.Desc
	costToday       *prometheus.Desc
	costUnknown     *prometheus.Desc
	up              *prometheus.Desc
}

// NewOverviewCollector reads the source at each scrape.
func NewOverviewCollector(source OverviewSource) *OverviewCollector {
	state := []string{"state"}
	return &OverviewCollector{
		source:          source,
		nodes:           prometheus.NewDesc("axlr_bots_nodes", "Nodes in each state.", state, nil),
		bots:            prometheus.NewDesc("axlr_bots_bots", "Bots in each state.", state, nil),
		tasks:           prometheus.NewDesc("axlr_bots_tasks", "Tasks in each state.", state, nil),
		tasksToday:      prometheus.NewDesc("axlr_bots_tasks_today", "Tasks created today, by the state they are in now.", state, nil),
		executionsToday: prometheus.NewDesc("axlr_bots_executions_today", "Executions started today.", nil, nil),
		costToday:       prometheus.NewDesc("axlr_bots_cost_usd_today", "Cost in US dollars of the executions started today.", nil, nil),
		costUnknown:     prometheus.NewDesc("axlr_bots_executions_cost_unknown_today", "Executions started today whose cost is unknown.", nil, nil),
		up:              prometheus.NewDesc("axlr_bots_overview_up", "1 when the overview could be read at this scrape, 0 when it could not.", nil, nil),
	}
}

// Describe sends every description.
func (c *OverviewCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.nodes, c.bots, c.tasks, c.tasksToday, c.executionsToday, c.costToday, c.costUnknown, c.up} {
		ch <- desc
	}
}

// Collect reads the overview once and sends its counts; when it cannot
// be read it sends axlr_bots_overview_up 0 and nothing else.
func (c *OverviewCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	overview, err := c.source.GetOverview(ctx, time.Time{})
	if err != nil {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	sendCounts(ch, c.nodes, overview.Nodes)
	sendCounts(ch, c.bots, overview.Bots)
	sendCounts(ch, c.tasks, overview.Tasks)
	sendCounts(ch, c.tasksToday, overview.TasksSince)
	ch <- prometheus.MustNewConstMetric(c.executionsToday, prometheus.GaugeValue, float64(overview.ExecutionsSince))
	ch <- prometheus.MustNewConstMetric(c.costToday, prometheus.GaugeValue, overview.CostUSDSince)
	ch <- prometheus.MustNewConstMetric(c.costUnknown, prometheus.GaugeValue, float64(overview.CostUnknownSince))
}

func sendCounts(ch chan<- prometheus.Metric, desc *prometheus.Desc, counts map[string]int) {
	for state, count := range counts {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(count), state)
	}
}
