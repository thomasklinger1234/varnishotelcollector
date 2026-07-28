package varnishcachestatreceiver

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachestatreceiver/internal/metadata"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"

	varnishstats "gitlab.com/uplex/varnish/varnishapi/pkg/stats"
)

var (
	// (prefix:)<uuid>.<name>
	BackendPatternUUID = regexp.MustCompile(`([[0-9A-Za-z]{8}-[0-9A-Za-z]{4}-[0-9A-Za-z]{4}-[89ABab][0-9A-Za-z]{3}-[0-9A-Za-z]{12})(.*)`)

	// <name>(<ip>,(<something>),<port>)
	BackendPatternParen = regexp.MustCompile(`(.*)\((.*)\)`)
)

type varnishstatCounter struct {
	Description string `json:"description"`
	Flag        string `json:"flag"`
	Format      string `json:"format"`
	Value       uint64 `json:"value"`
	Level       string `json:"level"`
}
type varnishstatCounters struct {
	Timestamp string                        `json:"timestamp"`
	Counters  map[string]varnishstatCounter `json:"counters"`
}

type varnishcachestatScraper struct {
	cfg       *Config
	set       receiver.Settings
	startTime pcommon.Timestamp
}

func (v *varnishcachestatScraper) scrape(ctx context.Context) (pmetric.Metrics, error) {
	metrics := pmetric.NewMetrics()

	vsc := varnishstats.New()
	if err := vsc.Timeout(v.cfg.Timeout); err != nil {
		return metrics, fmt.Errorf("error setting vsc timeout: %w", err)
	}
	defer func() {
		vsc.Release()
	}()

	for _, x := range v.cfg.IncludeTags {
		if err := vsc.Include(x); err != nil {
			return metrics, fmt.Errorf("error including tags: %w", err)
		}
	}

	for _, x := range v.cfg.ExcludeTags {
		if err := vsc.Exclude(x); err != nil {
			return metrics, fmt.Errorf("error excluding tags: %w", err)
		}
	}

	if err := vsc.Attach(v.cfg.WorkingDirectory); err != nil {
		return metrics, fmt.Errorf("failed to attach to vsc: %w", err)
	}

	statsTimestamp := time.Now()
	stats := &varnishstatCounters{
		Timestamp: statsTimestamp.Format(time.RFC3339),
		Counters:  make(map[string]varnishstatCounter),
	}

	if err := vsc.Read(func(name string, val uint64) bool {
		mDesc, err := vsc.D9n(name)
		if err != nil {
			return false
		}

		stats.Counters[name] = varnishstatCounter{
			Description: mDesc.String(),
			Flag:        mDesc.Semantics.String(),
			Format:      mDesc.Format.String(),
			Value:       val,
			Level:       mDesc.Level.String(),
		}
		return true
	}); err != nil {
		return metrics, fmt.Errorf("error getting stats: %w", err)
	}

	// find latest revision for VBE.* metrics
	vbeReload := "VBE.reload_"
	vbeMostRecentPrefix := "VBE.boot"
	vbeIsOutdated := func(name string) bool {
		return len(vbeMostRecentPrefix) > 0 && strings.HasPrefix(name, "VBE.") && !strings.HasPrefix(name, vbeMostRecentPrefix)
	}

	for name, _ := range stats.Counters {
		if strings.HasPrefix(name, vbeReload) && strings.HasSuffix(name, ".happy") {
			dotAfterPrefixIndex := len(vbeReload) + strings.Index(name[len(vbeReload):], ".")
			vbeReloadPrefix := name[:dotAfterPrefixIndex]
			if strings.Compare(vbeReloadPrefix, vbeMostRecentPrefix) > 0 {
				vbeMostRecentPrefix = vbeReloadPrefix
			}
		}
	}

	for name, stat := range stats.Counters {
		if name == "" {
			continue
		}

		if vbeIsOutdated(name) {
			v.set.Logger.Debug("Skipping counter for outdated VBE revision",
				zap.String("counter", name),
				zap.String("current", vbeMostRecentPrefix))
			continue
		}

		resourceMetrics := metrics.ResourceMetrics().AppendEmpty()
		resource := resourceMetrics.Resource()

		scopeMetrics := resourceMetrics.ScopeMetrics().AppendEmpty()
		scope := scopeMetrics.Scope()
		scope.SetName(metadata.ScopeName)
		scope.SetVersion(v.set.BuildInfo.Version)

		metric := scopeMetrics.Metrics().AppendEmpty()
		metric.SetName(name)
		metric.SetDescription(stat.Description)
		metric.SetUnit(stat.Format)

		if isVBE := strings.HasPrefix(name, "VBE."); isVBE {
			vIdent := strings.ReplaceAll(name, vbeMostRecentPrefix+".", "")

			if hits := BackendPatternUUID.FindAllStringSubmatch(vIdent, -1); len(hits) > 0 && len(hits[0]) >= 3 {
				resource.Attributes().PutStr("backend", extractBackendNameFromCounter(hits[0][2]))
				resource.Attributes().PutStr("server", extractBackendNameFromCounter(hits[0][1]))
			} else if hits := BackendPatternParen.FindAllStringSubmatch(vIdent, -1); len(hits) > 0 && len(hits[0]) >= 3 {
				resource.Attributes().PutStr("backend", extractBackendNameFromCounter(hits[0][1]))
				resource.Attributes().PutStr("server", strings.Replace(hits[0][2], ",,", ":", 1))
			} else {
				resource.Attributes().PutStr("backend", strings.Split(vIdent, ".")[0])
				resource.Attributes().PutStr("server", "unknown")
			}

			vIdentParts := strings.Split(name, ".")
			if len(vIdentParts) > 0 {
				metric.SetName("backend_" + vIdentParts[len(vIdentParts)-1])
			}
		}

		if hits := regexp.MustCompile("LCK.(.*).dbg_busy").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("lck_dbg_busy")
		}

		if hits := regexp.MustCompile("LCK.(.*).dbg_try").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("lck_dbg_try_fail")
		}

		if hits := regexp.MustCompile("LCK.(.*).creat").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("target", hits[0][1])
			metric.SetName("lock_created")
		}

		if hits := regexp.MustCompile("LCK.(.*).destroy").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("target", hits[0][1])
			metric.SetName("lock_destroyed")
		}

		if hits := regexp.MustCompile("LCK.(.*).locks").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("target", hits[0][1])
			metric.SetName("lock_operations")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).allocs").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_allocs")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).frees").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_frees")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).live").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_live")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).pool").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_pool")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).randry").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_randry")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).recycle").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_recycle")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).surplus").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_surplus")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).sz_actual").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_sz_actual")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).sz_wanted").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_sz_wanted")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).timeout").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_timeout")
		}

		if hits := regexp.MustCompile("MEMPOOL.(.*).toosmall").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			metric.SetName("mempool_toosmall")
		}

		if hits := regexp.MustCompile("SMA.(.*).c_req").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("type", strings.ToLower(hits[0][1]))
			metric.SetName("sma_c_req")
		}

		if hits := regexp.MustCompile("SMA.(.*).c_fail").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("type", strings.ToLower(hits[0][1]))
			metric.SetName("sma_c_fail")
		}

		if hits := regexp.MustCompile("SMA.(.*).c_bytes").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("type", strings.ToLower(hits[0][1]))
			metric.SetName("sma_c_bytes")
		}

		if hits := regexp.MustCompile("SMA.(.*).c_freed").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("type", strings.ToLower(hits[0][1]))
			metric.SetName("sma_c_freed")
		}

		if hits := regexp.MustCompile("SMA.(.*).g_alloc").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("type", strings.ToLower(hits[0][1]))
			metric.SetName("sma_g_alloc")
		}

		if hits := regexp.MustCompile("SMA.(.*).g_bytes").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("type", strings.ToLower(hits[0][1]))
			metric.SetName("sma_g_bytes")
		}

		if hits := regexp.MustCompile("SMA.(.*).g_space").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("type", strings.ToLower(hits[0][1]))
			metric.SetName("sma_g_space")
		}

		if hits := regexp.MustCompile("WAITER.(.*).conns").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			if metadata.ReceiverVarnishcachestatBreakPrometheusExporterCompatFeatureGate.IsEnabled() {
				metric.SetName("waiter_conns") // TODO(thomasklinger1234): [BREAKING] prometheus_varnish_exporter does not rename
			}
		}

		if hits := regexp.MustCompile("WAITER.(.*).remclose").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			if metadata.ReceiverVarnishcachestatBreakPrometheusExporterCompatFeatureGate.IsEnabled() {
				metric.SetName("waiter_remclose") // TODO(thomasklinger1234): [BREAKING] prometheus_varnish_exporter does not rename
			}
		}

		if hits := regexp.MustCompile("WAITER.(.*).timeout").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			if metadata.ReceiverVarnishcachestatBreakPrometheusExporterCompatFeatureGate.IsEnabled() {
				metric.SetName("waiter_timeout") // TODO(thomasklinger1234): [BREAKING] prometheus_varnish_exporter does not rename
			}
		}

		if hits := regexp.MustCompile("WAITER.(.*).action").FindAllStringSubmatch(name, -1); len(hits) > 0 && len(hits[0]) > 0 {
			resource.Attributes().PutStr("id", hits[0][1])
			if metadata.ReceiverVarnishcachestatBreakPrometheusExporterCompatFeatureGate.IsEnabled() {
				metric.SetName("waiter_action") // TODO(thomasklinger1234): [BREAKING] prometheus_varnish_exporter does not rename
			}
		}

		switch stat.Flag {
		case varnishstats.Counter.String():
			mSum := metric.SetEmptySum()
			mSum.SetIsMonotonic(true)
			mSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
			mSumDp := mSum.DataPoints().AppendEmpty()
			mSumDp.SetStartTimestamp(v.startTime)
			mSumDp.SetTimestamp(pcommon.NewTimestampFromTime(statsTimestamp))
			mSumDp.SetIntValue(int64(stat.Value))
		case varnishstats.Gauge.String():
			mGauge := metric.SetEmptyGauge()
			mGaugeDp := mGauge.DataPoints().AppendEmpty()
			mGaugeDp.SetTimestamp(pcommon.NewTimestampFromTime(statsTimestamp))
			mGaugeDp.SetIntValue(int64(stat.Value))
		case varnishstats.S7sBitmap.String():
			if strings.HasSuffix(name, ".happy") {
				upValue := 0.0
				if stat.Value > 0 && (stat.Value&uint64(1)) > 0 {
					upValue = 1.0
				}
				mGauge := metric.SetEmptyGauge()
				mGaugeDp := mGauge.DataPoints().AppendEmpty()
				mGaugeDp.SetStartTimestamp(v.startTime)
				mGaugeDp.SetTimestamp(pcommon.NewTimestampFromTime(statsTimestamp))
				mGaugeDp.SetDoubleValue(upValue)

				metric.SetName("backend_up")
				metric.SetDescription("Backend up as per the latest health probe")
			} else {
				v.set.Logger.Warn("Unsupported bitmap counter", zap.String("name", name))
			}
		case varnishstats.S7sUnknown.String():
			v.set.Logger.Warn("Unsupported metric type: "+stat.Flag, zap.String("name", name))
			continue
		default:
			v.set.Logger.Warn("Unsupported metric type: "+stat.Flag, zap.String("name", name))
			continue
		}

		// name sanitization for unmapped metrics
		metric.SetName(normalizeMetricName(name))
	}

	return metrics, nil
}

func newVarnishcachestatScraper(settings receiver.Settings, cfg *Config) (*varnishcachestatScraper, error) {
	return &varnishcachestatScraper{
		cfg:       cfg,
		set:       settings,
		startTime: pcommon.NewTimestampFromTime(time.Now()),
	}, nil
}

func normalizeMetricName(name string) string {
	norm := strings.ToLower(name)
	norm = strings.ReplaceAll(norm, ".(", "_")
	norm = strings.ReplaceAll(norm, ").", "_")
	norm = strings.ReplaceAll(norm, "(", "_")
	norm = strings.ReplaceAll(norm, ")", "_")
	norm = strings.ReplaceAll(norm, ".", "_")
	norm = strings.ReplaceAll(norm, ":", "_")
	return norm
}

func extractBackendNameFromCounter(name string) string {
	name = strings.Trim(name, ".")
	for _, prefix := range []string{"boot.", "root:"} {
		if strings.HasPrefix(name, prefix) {
			name = name[len(prefix):]
		}
	}

	// reload_2019-08-29T100458.<name> as by varnish_reload_vcl in 4.x
	// reload_20191014_091124_78599.<name> as by varnishreload in 6+
	if strings.HasPrefix(name, "reload_") {
		dot := strings.Index(name, ".")
		if dot != -1 {
			name = name[dot+1:]
		}
	}

	return name
}
