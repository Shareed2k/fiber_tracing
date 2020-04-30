package fiber_tracing

import (
	"net/http"
	"unsafe"

	"github.com/gofiber/fiber"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

var getString = func(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

// Config ...
type Config struct {
	// Tracer
	// Default: NoopTracer
	Tracer opentracing.Tracer

	// OperationName
	// Default: func(ctx *fiber.Ctx) string {
	//	 return "HTTP " + ctx.Method() + " URL: " + ctx.Path()
	// }
	OperationName func(*fiber.Ctx) string

	// Filter defines a function to skip middleware.
	// Optional. Default: nil
	Filter func(*fiber.Ctx) bool

	// Modify
	Modify func(*fiber.Ctx, opentracing.Span)
}

func New(config ...Config) func(*fiber.Ctx) {
	// Init config
	var cfg Config
	if len(config) > 0 {
		cfg = config[0]
	}

	if cfg.Tracer == nil {
		cfg.Tracer = &opentracing.NoopTracer{}
	}

	if cfg.Modify == nil {
		cfg.Modify = func(ctx *fiber.Ctx, span opentracing.Span) {
			span.SetTag("http.remote_addr", ctx.IP())
			span.SetTag("http.path", ctx.Path())
			span.SetTag("http.host", ctx.Hostname())
			span.SetTag("http.method", ctx.Method())
			span.SetTag("http.url", ctx.OriginalURL())
		}
	}

	if cfg.OperationName == nil {
		cfg.OperationName = func(ctx *fiber.Ctx) string {
			return "HTTP " + ctx.Method() + " URL: " + ctx.Path()
		}
	}

	return func(ctx *fiber.Ctx) {
		// Filter request to skip middleware
		if cfg.Filter != nil && cfg.Filter(ctx) {
			ctx.Next()

			return
		}

		var span opentracing.Span

		opname := cfg.OperationName(ctx)
		tr := cfg.Tracer
		hdr := make(http.Header)

		ctx.Fasthttp.Request.Header.VisitAll(func(k, v []byte) {
			hdr.Set(getString(k), getString(v))
		})

		if ctx, err := tr.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(hdr)); err != nil {
			span = tr.StartSpan(opname)
		} else {
			span = tr.StartSpan(opname, ext.RPCServerOption(ctx))
		}

		cfg.Modify(ctx, span)

		defer func() {
			status := ctx.Fasthttp.Response.StatusCode()

			ext.HTTPStatusCode.Set(span, uint16(status))

			if status >= fiber.StatusInternalServerError {
				ext.Error.Set(span, true)
			}

			span.Finish()
		}()

		ctx.Next()
	}
}
