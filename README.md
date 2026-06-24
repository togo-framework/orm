<p align="center"><img src=".github/assets/togo-mark.svg" width="96" alt="togo"></p>

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

---

## 💎 Premium sponsors

togo is proudly sponsored by **ID8 Media** and **One Studio**.

<p align="center">
  <a href="https://id8media.com"><img src=".github/assets/id8media.svg" height="44" alt="ID8 Media" /></a>
  &nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;
  <a href="https://one-studio.co"><img src=".github/assets/one-studio.jpeg" height="44" alt="One Studio" /></a>
</p>
