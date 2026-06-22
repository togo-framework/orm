# orm

Driver-agnostic, Eloquent-style query builder for [togo](https://github.com/togo-framework/togo).
It uses the dialect from the kernel (`togo.Dialect`); column/operator/ORDER BY inputs are
validated against an allowlist while values are always parameterized.

```bash
togo install togo-framework/orm
```

```go
models.Posts(app).Where("title", "ILIKE", "%go%").Order("created_at DESC").Get(ctx)
```
