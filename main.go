package fiber_tracing

import (
	"errors"
	"io"
	"net/http"
	"time"
	"unsafe"

	"github.com/gofiber/fiber/v2"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/uber/jaeger-client-go/config"
)

const (
	DefaultParentSpanKey = "#defaultTracingParentSpanKey"
	DefaultComponentName = "fiber/v2"
)

var (
	// DefaultTraceConfig is the default Trace middleware config.
	DefaultConfig = Config{
		Modify: func(ctx *fiber.Ctx, span opentracing.Span) {
			ext.HTTPMethod.Set(span, ctx.Method())
			ext.HTTPUrl.Set(span, ctx.OriginalURL())
			ext.Component.Set(span, DefaultComponentName)

			span.SetTag("http.remote_addr", ctx.IP())
			span.SetTag("http.path", ctx.Path())
			span.SetTag("http.host", ctx.Hostname())
		},
		OperationName: func(ctx *fiber.Ctx) string {
			return "HTTP " + ctx.Method() + " URL: " + ctx.Path()
		},
		ComponentName: DefaultComponentName,
		ParentSpanKey: DefaultParentSpanKey,
	}

	getString = func(b []byte) string {
		return *(*string)(unsafe.Pointer(&b))
	}
)

// Config ...
type Config struct {
	// Tracer
	// Default: NoopTracer
	Tracer opentracing.Tracer

	// ParentSpanKey
	// Default: #defaultTracingParentSpanKey
	ParentSpanKey string

	// ComponentName used for describing the tracing component name
	ComponentName string

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

// New returns a Trace middleware.
// Trace middleware traces http requests and reporting errors.
func New(tracer opentracing.Tracer) fiber.Handler {
	c := DefaultConfig
	c.Tracer = tracer

	return NewWithConfig(c)
}

// NewWithJaegerTracer creates an Opentracing tracer and attaches it to Fiber middleware.
// Returns Closer do be added to caller function as `defer closer.Close()`
func NewWithJaegerTracer(f *fiber.App) io.Closer {
	// Add Opentracing instrumentation
	defcfg := config.Configuration{
		ServiceName: "fiber-tracer",
		Sampler: &config.SamplerConfig{
			Type:  "const",
			Param: 1,
		},
		Reporter: &config.ReporterConfig{
			LogSpans:            true,
			BufferFlushInterval: 1 * time.Second,
		},
	}

	cfg, err := defcfg.FromEnv()
	if err != nil {
		panic("Could not parse Jaeger env vars: " + err.Error())
	}

	tracer, closer, err := cfg.NewTracer()
	if err != nil {
		panic("Could not initialize jaeger tracer: " + err.Error())
	}

	opentracing.SetGlobalTracer(tracer)

	f.Use(NewWithConfig(Config{
		Tracer: tracer,
	}))

	return closer
}

// NewWithConfig returns a Trace middleware with config.
func NewWithConfig(config ...Config) fiber.Handler {
	// Init config
	var cfg Config
	if len(config) > 0 {
		cfg = config[0]
	}

	if cfg.Tracer == nil {
		cfg.Tracer = &opentracing.NoopTracer{}
	}

	if cfg.ParentSpanKey == "" {
		cfg.ParentSpanKey = DefaultConfig.ParentSpanKey
	}

	if cfg.ComponentName == "" {
		cfg.ComponentName = DefaultConfig.ComponentName
	}

	if cfg.Modify == nil {
		cfg.Modify = DefaultConfig.Modify
	}

	if cfg.OperationName == nil {
		cfg.OperationName = DefaultConfig.OperationName
	}

	return func(ctx *fiber.Ctx) error {
		// Filter request to skip middleware
		if cfg.Filter != nil && cfg.Filter(ctx) {
			return ctx.Next()
		}

		var span opentracing.Span

		operationName := cfg.OperationName(ctx)
		tr := cfg.Tracer
		hdr := make(http.Header)

		ctx.Request().Header.VisitAll(func(k, v []byte) {
			hdr.Set(getString(k), getString(v))
		})

		if ctx, err := tr.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(hdr)); err != nil {
			span = tr.StartSpan(operationName)
		} else {
			span = tr.StartSpan(operationName, ext.RPCServerOption(ctx))
		}

		cfg.Modify(ctx, span)

		var err error
		defer func() {
			status := ctx.Response().StatusCode()

			if err != nil {
				var httpError *fiber.Error

				if errors.As(err, &httpError) {
					if httpError.Code != 0 {
						status = httpError.Code
					}

					span.SetTag("error.message", httpError.Message)
				} else {
					span.SetTag("error.message", err.Error())
				}

				if status == fiber.StatusOK {
					// this is ugly workaround for cases when httpError.code == 0 or error was not httpError and status
					// in request was 200 (OK). In these cases replace status with something that represents an error
					// it could be that error handlers or middlewares up in chain will output different status code to
					// client. but at least we send something better than 200 to jaeger
					status = fiber.StatusInternalServerError
				}
			}

			ext.HTTPStatusCode.Set(span, uint16(status))

			if status >= fiber.StatusInternalServerError {
				ext.Error.Set(span, true)
			}

			span.Finish()
		}()

		ctx.Locals(cfg.ParentSpanKey, span)

		err = ctx.Next()

		return err
	}
}
