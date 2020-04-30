## fiber_tracing is middleware for fiber framework 

fiber_tracing Middleware trace requests on [Fiber framework](https://gofiber.io/) with OpenTracing API.
You can use every tracer that implement OpenTracing interface

### Install
```
go get -u github.com/gofiber/fiber
go get -u github.com/shareed2k/fiber_tracing
```

### Config
| Property | Type | Description | Default |
| :--- | :--- | :--- | :--- |
| Tracer | `opentracing.Tracer` | initializes an opentracing tracer., possible values: `jaeger`, `lightstep`, `instana`, `basictracer-go`, ... | `"&opentracing.NoopTracer{}"` |
| OperationName | `func(*fiber.Ctx) string` | Span operation name | `"HTTP " + ctx.Method() + " URL: " + ctx.Path()` |
| Filter | `func(*fiber.Ctx) bool` | Defines a function to skip middleware. | `nil` |
| Modify | `func(*fiber.Ctx, opentracing.Span)` | Defines a function to edit span like add tags or logs... | `span.SetTag("http.remote_addr", ctx.IP()) ...` |

### Example
```go
package main

import (
	"github.com/gofiber/fiber"
	"github.com/shareed2k/fiber_tracing"
	"github.com/uber/jaeger-client-go/config"
)

func main() {
	app := fiber.New()

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

	defer closer.Close()

	app.Use(fiber_tracing.New(fiber_tracing.Config{
		Tracer: tracer,
	}))

	app.Get("/", func(c *fiber.Ctx) {
		c.Send("Welcome!")
	})

	app.Listen(3000)
}
```
