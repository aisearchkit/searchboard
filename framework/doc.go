// Package framework is a small HTTP/1.1 server framework modeled on
// cloudwego/hertz.
//
// Its design follows Hertz: a layered stack (network transport, protocol,
// routing, application) where the transport is pluggable, handlers receive a
// pooled *Context, and middleware wraps handlers in a chain.
//
//	s := framework.New(framework.WithHostPorts(":8080"))
//	s.Use(middleware.Recovery(logger), middleware.RequestID())
//	s.GET("/users/:id", func(ctx context.Context, c *framework.Context) {
//		c.JSON(200, map[string]string{"id": c.Param("id")})
//	})
//	log.Fatal(s.Spin())
//
// The default transport is cloudwego/netpoll on Linux and the BSDs and the
// standard library elsewhere. JSON uses bytedance/sonic.
//
// Request and Response objects are pooled. A handler must not keep references
// to the Context, the request, its body or its headers after it returns.
package framework
