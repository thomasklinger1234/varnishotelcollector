package varnishcachelogreceiver

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	varnishlog "gitlab.com/uplex/varnish/varnishapi/pkg/log"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
)

var _ receiver.Traces = &varnishcachelogReceiver{}

type varnishcachelogReceiver struct {
	set          receiver.Settings
	cfg          *Config
	nextConsumer consumer.Traces
	wg           sync.WaitGroup
}

func (v varnishcachelogReceiver) Start(ctx context.Context, host component.Host) error {
	vsm := varnishlog.New()
	if err := vsm.Timeout(v.cfg.Timeout); err != nil {
		return fmt.Errorf("failed to set vsm timeout: %w", err)
	}

	if err := vsm.Attach(v.cfg.WorkingDirectory); err != nil {
		return fmt.Errorf("failed to attach to vsm: %w", err)
	}

	vsmCursor, err := vsm.NewCursor()
	if err != nil {
		return fmt.Errorf("failed to create vsm cursor: %w", err)
	}

	vsmQuery, err := vsmCursor.NewQuery(varnishlog.Request, v.cfg.VSLQuery)
	if err != nil {
		return fmt.Errorf("failed to create vsm query: %w", err)
	}

	v.wg.Go(func() {
		defer func() {
			vsmCursor.Delete()
			vsm.Release()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				txGrp, txStatus := vsmQuery.NextTxGroup()
				switch txStatus {
				case varnishlog.EOL:
					continue
				case varnishlog.EOF:
					v.set.Logger.Error("VSL EOF", zap.String("error", vsm.Error()))
					return
				case varnishlog.Abandoned:
					v.set.Logger.Error("VSL abandoned", zap.String("error", vsm.Error()))
					return
				case varnishlog.IOErr:
					v.set.Logger.Error("VSL IOErr", zap.String("error", vsm.Error()))
					return
				case varnishlog.WriteErr:
					v.set.Logger.Error("VSL WriteErr", zap.String("error", vsm.Error()))
					return
				case varnishlog.Overrun:
					v.set.Logger.Error("VSL overrun", zap.String("error", vsm.Error()))
					return
				default:
					traces := ptrace.NewTraces()

					resourceSpans := traces.ResourceSpans().AppendEmpty()
					v.set.Resource.CopyTo(resourceSpans.Resource())

					scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
					scopeSpans.Scope().SetName(metadata.ScopeName)

					txTraceID := pcommon.NewTraceIDEmpty()
					txCache := map[uint64]*varnishTransaction{}

					for _, tx := range txGrp {
						vtx := &varnishTransaction{
							VXID: uint64(tx.VXID),
							Req: varnishTransactionReq{
								Headers: make(map[string]string),
							},
							Resp: varnishTransactionResp{
								Headers: make(map[string]string),
							},
							Events: make([]varnishTransactionEvent, 0),
							Errors: make([]string, 0),
							Logs:   make([]string, 0),
							Links:  make([]varnishTransactionLink, 0),
							Side:   tx.Type.String(),
							Reason: tx.Reason.String(),
						}

						for _, txRec := range tx.Records {
							switch txRec.Type {
							case varnishlog.Client:
								vtx.Side = "client"
							case varnishlog.Backend:
								vtx.Side = "backend"
							}

							if trans, ok := transformFuncs[txRec.Tag.String()]; ok {
								if err := trans(vtx, txRec); err != nil {
									v.set.Logger.Error("failed to translate record", zap.String("record", txRec.Tag.String()), zap.Error(err))
								}
							}
						}

						txCache[vtx.VXID] = vtx

						// initial sess
						if tx.ParentVXID == 0 {
							txTraceID = generateTraceID(uint64(tx.VXID))
						}

						span := scopeSpans.Spans().AppendEmpty()
						if tx.VXID > 0 {
							span.Attributes().PutInt("varnish.vxid", int64(vtx.VXID))
						}
						if tx.ParentVXID > 0 {
							span.Attributes().PutInt("varnish.vxid_parent", int64(vtx.VXIDParent))
						}
						span.Attributes().PutStr("varnish.tx.type", vtx.Type)
						span.Attributes().PutStr("varnish.tx.reason", vtx.Reason)
						span.Status().SetCode(ptrace.StatusCodeOk)

						if err := updateSpan(span, vtx); err != nil {
							v.set.Logger.Error("failed to update span", zap.String("span", txTraceID.String()), zap.Error(err))
						}

						for _, link := range vtx.Links {
							if vtxLinked, ok := txCache[link.VXID]; ok {
								spanLink := span.Links().AppendEmpty()
								spanLink.SetSpanID(generateSpanID(vtxLinked.VXID))
								spanLink.SetTraceID(txTraceID)
								spanLink.Attributes().PutStr("varnish.reason", link.Reason)
								spanLink.Attributes().PutStr("varnish.type", link.Type)
							}
						}

						// TODO(thomasklinger1234): set ids based on traceparent header (if exists)
						span.SetTraceID(txTraceID)
						span.SetSpanID(generateSpanID(uint64(tx.VXID)))
						if tx.ParentVXID > 0 {
							span.SetParentSpanID(generateSpanID(uint64(tx.ParentVXID)))
						}
					}

					if err := v.nextConsumer.ConsumeTraces(ctx, traces); err != nil {
						v.set.Logger.Error("failed to consume traces", zap.Error(err))
					}
				}
			}
		}

	})
	return nil
}

func (v varnishcachelogReceiver) Shutdown(_ context.Context) error {
	v.wg.Wait()
	return nil
}

func newVarnishcacheLogReceiver(set receiver.Settings, config *Config, nextConsumer consumer.Traces) receiver.Traces {
	return &varnishcachelogReceiver{
		set:          set,
		cfg:          config,
		nextConsumer: nextConsumer,
	}
}

func generateTraceID(vxid uint64) pcommon.TraceID {
	var id [16]byte
	binary.BigEndian.PutUint64(id[0:16], vxid)
	return pcommon.TraceID(id)
}

func generateSpanID(vxid uint64) pcommon.SpanID {
	var id [8]byte
	binary.BigEndian.PutUint64(id[0:8], vxid)
	return pcommon.SpanID(id)
}
