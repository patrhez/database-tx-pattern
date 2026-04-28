# TxManager Transaction Pattern for GORM

This repo demonstrates a transaction pattern for GORM with two goals:

- avoid the main problem of the `tx *gorm.DB` argument approach: the caller can still pass the wrong DB/tx handle
- keep transaction-heavy business logic easy to unit test

It also shows how to use `go-sqlmock` to test repository SQL and the real GORM transaction flow without a real MySQL instance.

## The problem we want to solve

A common repository API looks like this:

```go
func GetByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*Task, error)
func UpdateName(ctx context.Context, tx *gorm.DB, id uint, name string) error
```

This is workable, but it depends on the caller always passing the correct `tx` instance.

That creates a real failure mode:

- the service starts a transaction
- one repository call gets the transaction handle
- another repository call accidentally gets the root `db`
- part of the workflow now runs outside the intended transaction

The compiler cannot prevent this. The callee also cannot enforce that the caller passed the right handle. The transaction boundary is therefore controlled by caller discipline.

## What our `TxManager` approach is

The approach in this repo is:

- the service owns the transaction boundary
- `TxManager` starts the transaction
- the transaction handle is stored in `context.Context`
- the repository implementation reads the current DB handle from the context

The key interfaces and types are:

- [task/service.go](/Users/patrickhe/Projects/database-tx-pattern/task/service.go): `TxManager`, `TaskStore`, `TaskService`
- [task/tx.go](/Users/patrickhe/Projects/database-tx-pattern/task/tx.go): `GormTxManager`, `withTx`, `dbFromContext`
- [task/repository.go](/Users/patrickhe/Projects/database-tx-pattern/task/repository.go): `Repository` and `dbFor(ctx)`
- [main.go](/Users/patrickhe/Projects/database-tx-pattern/main.go): minimal wiring example for repository, `GormTxManager`, and service construction

## How it works

### 1. Service starts the transaction through `TxManager`

The service does not open a GORM transaction directly. It calls the `TxManager` interface:

```go
type TxManager interface {
    InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
```

Example from the service:

```go
return s.tx.InTx(ctx, func(txCtx context.Context) error {
    existing, err := s.store.GetByIDForUpdate(txCtx, id)
    if err != nil {
        return err
    }
    if existing == nil {
        return errors.New("task not found")
    }
    return s.store.UpdateName(txCtx, id, newName)
})
```

The important part is that all repository calls inside the callback use the same `txCtx`.

### 2. `GormTxManager` opens the real transaction and injects it into context

Production transaction handling is implemented in [task/tx.go](/Users/patrickhe/Projects/database-tx-pattern/task/tx.go):

```go
func (m *GormTxManager) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
    return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        return fn(withTx(ctx, tx))
    })
}
```

What happens here:

1. `m.db.Transaction(...)` opens a real GORM transaction
2. GORM provides the transactional `*gorm.DB` as `tx`
3. `withTx(ctx, tx)` stores that transactional handle into the context
4. the callback runs with the new context

The helper used for that is:

```go
func withTx(ctx context.Context, tx *gorm.DB) context.Context {
    return context.WithValue(ctx, txContextKey{}, tx)
}
```

### 3. Repository implementation resolves the DB handle from context

This is the most important repository rule in this pattern:

- repository methods do not accept `tx *gorm.DB` as a parameter
- repository methods always resolve the DB handle from `context.Context`

The repository helper is:

```go
func (r *Repository) dbFor(ctx context.Context) *gorm.DB {
    return dbFromContext(ctx, r.db)
}
```

And the context lookup is:

```go
func dbFromContext(ctx context.Context, fallback *gorm.DB) *gorm.DB {
    tx, ok := ctx.Value(txContextKey{}).(*gorm.DB)
    if !ok || tx == nil {
        return fallback
    }
    return tx
}
```

That means:

- if the context contains a transaction, repository code uses that transaction
- if not, repository code uses the repository's root DB handle

### 4. Repository methods must always go through `dbFor(ctx)`

Repository implementation should always follow this pattern:

```go
func (r *Repository) UpdateName(ctx context.Context, id uint, name string) error {
    return r.dbFor(ctx).WithContext(ctx).
        Model(&Task{}).
        Where("id = ?", id).
        Update("name", name).Error
}
```

The same rule applies to reads, writes, and locking queries:

```go
func (r *Repository) GetByIDForUpdate(ctx context.Context, id uint) (*Task, error) {
    var t Task
    err := r.dbFor(ctx).WithContext(ctx).
        Clauses(clause.Locking{Strength: "UPDATE"}).
        First(&t, id).Error
    if err != nil {
        return nil, err
    }
    return &t, nil
}
```

So the repository contract is:

- accept `context.Context`
- never accept `tx *gorm.DB`
- always call `r.dbFor(ctx)` before issuing GORM operations

## Why this is safer than the `tx` argument approach

This pattern centralizes the transaction decision in one place: `TxManager.InTx(...)`.

Once the service enters the transaction callback:

- all repository calls use the same context
- repository code resolves the same transaction handle from that context
- callers no longer manually choose which `*gorm.DB` to pass to each repository method

This does not remove all possible mistakes, but it removes the specific mistake of repeatedly passing the wrong DB/tx handle across repository calls.

## Why this is unit-test friendly

The service layer depends on two interfaces:

- `TaskStore`
- `TxManager`

Because of that, service tests can replace both with fakes and verify business flow without real GORM behavior.

See [task/service_test.go](/Users/patrickhe/Projects/database-tx-pattern/task/service_test.go).

This makes it easy to test:

- transaction entry and exit
- call order inside the transactional workflow
- rollback behavior when repository logic returns an error

Example:

```go
tx := &fakeTxManager{
    inTxFn: func(ctx context.Context, fn func(ctx context.Context) error) error {
        callOrder = append(callOrder, "begin-tx")
        err := fn(ctx)
        if err != nil {
            callOrder = append(callOrder, "rollback")
            return err
        }
        callOrder = append(callOrder, "commit")
        return nil
    },
}
```

That is the main testing benefit of the pattern: transaction orchestration becomes ordinary unit-test logic.

## `go-sqlmock`

At the repository level, this repo uses `go-sqlmock`.

`go-sqlmock` creates a fake SQL connection. GORM is opened on top of that connection, so the repository still uses real GORM code while the test controls all SQL behavior.

Setup from [task/repository_test.go](/Users/patrickhe/Projects/database-tx-pattern/task/repository_test.go):

```go
sqlDB, mock, err := sqlmock.New()
gormDB, err := gorm.Open(mysql.New(mysql.Config{
    Conn:                      sqlDB,
    SkipInitializeWithVersion: true,
}), &gorm.Config{})
```

With this setup you can verify:

- generated SQL
- SQL arguments
- returned rows
- transaction boundaries such as `BEGIN` and `COMMIT`

Example:

```go
mock.ExpectBegin()
mock.ExpectExec(regexp.QuoteMeta(
    "UPDATE `tasks` SET `name`=?,`updated_at`=? WHERE id = ?",
)).
    WithArgs("new-name", sqlmock.AnyArg(), 10).
    WillReturnResult(sqlmock.NewResult(0, 1))
mock.ExpectCommit()
```

This repo also includes a higher-level test in [task/tx_test.go](/Users/patrickhe/Projects/database-tx-pattern/task/tx_test.go) that combines:

- real `Repository`
- real `GormTxManager`
- mocked SQL underneath

That verifies the full path:

- `BEGIN`
- `SELECT ... FOR UPDATE`
- `UPDATE`
- `COMMIT`

## Summary

The core design is:

- service code starts transactions through `TxManager`
- `GormTxManager` writes the transactional `*gorm.DB` into the context
- repository code reads the DB handle from context through `r.dbFor(ctx)`
- repository methods never accept `tx *gorm.DB` directly

This gives a clearer transaction boundary, reduces the chance of passing the wrong DB handle, and keeps the service layer easy to unit test.

## Is this pattern common?

Yes. The exact interface names differ across projects, but the underlying idea is common in production Go code:

- centralize transaction boundaries
- avoid scattering `Begin` / `Commit` / `Rollback`
- make business logic easier to test
- reduce the risk of mixing transactional and non-transactional DB handles

What varies is how the active transaction is propagated. Our approach uses:

- a `TxManager`
- a callback
- `context.Context` to carry the active `*gorm.DB`

That is a real pattern used by established projects, but it is not the only industry style. Below are several credible examples with similar goals.

### 1. Avito `go-transaction-manager`

Reference:

- [GitHub](https://github.com/avito-tech/go-transaction-manager)
- [pkg.go.dev module docs](https://pkg.go.dev/github.com/avito-tech/go-transaction-manager)
- [context package docs](https://pkg.go.dev/github.com/avito-tech/go-transaction-manager/trm/v2/context)

Why it is relevant:

- explicitly provides a transaction manager abstraction
- supports GORM
- stores and retrieves the active transaction from `context.Context`

Illustrative shape:

```go
func RenameUser(ctx context.Context, manager trm.Manager, repo UserRepo) error {
    return manager.Do(ctx, func(ctx context.Context) error {
        user, err := repo.GetForUpdate(ctx, 1)
        if err != nil {
            return err
        }
        user.Name = "new-name"
        return repo.Update(ctx, user)
    })
}
```

This is very close to the pattern used in this repo: transaction boundary in the service layer, repository methods consume `context.Context`, and the active transaction is resolved from context.

### 2. REL transaction model

Reference:

- [REL transactions docs](https://go-rel.github.io/transactions/)
- [REL introduction](https://go-rel.github.io/introduction/)

Why it is relevant:

- REL says transaction scope is determined by `context.Context`
- database calls inside other functions use the same transaction as long as they share the same context
- REL also includes built-in test tooling

Illustrative shape:

```go
func ProcessOrder(ctx context.Context, repo rel.Repository) error {
    return repo.Transaction(ctx, func(ctx context.Context) error {
        if err := repo.Update(ctx, &order, rel.Set("status", "paid")); err != nil {
            return err
        }
        return repo.Update(ctx, &inventory, rel.Dec("stock"))
    })
}
```

REL is even more opinionated than our approach because transaction scoping through context is a first-class part of the repository API.

### 3. ent `WithTx` pattern

Reference:

- [ent transactions docs](https://entgo.io/docs/transactions/)

Why it is relevant:

- ent recommends a reusable callback helper such as `WithTx`
- transaction handling is centralized
- existing code can often be reused by passing `tx.Client()`

Illustrative shape:

```go
func WithTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
    tx, err := client.Tx(ctx)
    if err != nil {
        return err
    }
    if err := fn(tx); err != nil {
        _ = tx.Rollback()
        return err
    }
    return tx.Commit()
}

func RenameUser(ctx context.Context, client *ent.Client) error {
    return WithTx(ctx, client, func(tx *ent.Tx) error {
        return RenameUserWithClient(ctx, tx.Client())
    })
}
```

This is similar in spirit, but different in mechanism:

- ent centralizes the transaction boundary with a helper
- but usually propagates the transactional client explicitly instead of using context to hide the handle

### 4. Bun `RunInTx` plus `IDB`

Reference:

- [Bun transactions guide](https://bun.uptrace.dev/guide/transactions.html)

Why it is relevant:

- Bun has `RunInTx(...)` for transaction lifecycle management
- Bun has `bun.IDB` so the same function can accept either `*bun.DB` or `bun.Tx`

Illustrative shape:

```go
func InsertUser(ctx context.Context, db bun.IDB, user *User) error {
    _, err := db.NewInsert().Model(user).Exec(ctx)
    return err
}

func CreateTwoUsers(ctx context.Context, db *bun.DB, u1, u2 *User) error {
    return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        if err := InsertUser(ctx, tx, u1); err != nil {
            return err
        }
        return InsertUser(ctx, tx, u2)
    })
}
```

This is a strong alternative when you want explicit propagation without forcing every call site to care about the concrete type.

### Comparison table

| Approach | How transaction is started | How DB/tx is propagated | Main strengths | Main weaknesses |
| --- | --- | --- | --- | --- |
| This repo: `TxManager` + context | `txManager.InTx(ctx, fn)` | Context carries transactional `*gorm.DB`; repository resolves via `dbFor(ctx)` | Centralized boundary, stable repository signatures, hard to accidentally pass the wrong handle, very testable service layer | Active transaction is implicit, context becomes part of transaction plumbing |
| Avito `go-transaction-manager` | Manager callback such as `Do` / manager API | Context carries the active transaction | Very close to this repo, reusable library, works across multiple backends including GORM | Same tradeoff as any context-carried tx approach: less explicit than direct tx arguments |
| REL | `repo.Transaction(ctx, fn)` | Context defines transaction scope | Built-in context-scoped transaction model, nested transactions, built-in testing support | More framework-opinionated; less “plain GORM” than a custom repository layer |
| ent | `WithTx(ctx, client, fn)` or direct `client.Tx(ctx)` | Usually explicit via `tx` or `tx.Client()` | Clear lifecycle, explicit transactional client, strong generated API | Caller still passes transactional client explicitly; signatures can still reflect transaction mechanics |
| Bun | `db.RunInTx(ctx, opts, fn)` | Explicit via `bun.Tx`, often abstracted through `bun.IDB` | Good balance of explicitness and reuse, one function can work with DB or tx | Still relies on passing the correct tx/IDB value through the call chain |

### Practical conclusion

If your main concern is:

- preventing callers from passing the wrong DB handle
- keeping repository signatures stable
- making service-layer transaction orchestration easy to unit test

then the `TxManager` + context approach in this repo is a strong choice.

If your team prefers:

- maximum explicitness
- less hidden state in context

then ent-style or Bun-style explicit propagation may feel more natural, even though they do not solve the "wrong handle passed by caller" problem as directly.

## Minimal usage example

See [main.go](/Users/patrickhe/Projects/database-tx-pattern/main.go) for a small executable example that:

- opens GORM with MySQL
- constructs `Repository`
- constructs `GormTxManager`
- constructs `TaskService`
- performs one transactional call through `service.RenameTaskWithLock(...)`

## Run tests

```bash
go test ./...
```

If local cache directories are needed:

```bash
mkdir -p .gocache .gomodcache
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...
```
