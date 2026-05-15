# Go Naming Conventions

## Full Names Over Abbreviations

Always prefer full, descriptive names over contractions:

```go
// Good
error, user, config, response, request, context, message, result

// Bad
err, usr, cfg, resp, req, ctx, msg, res
```

Examples:

```go
// Good
func GetUser(user_id string) (*User, error) {
    user, error := db.Find(user_id)
    if error != nil {
        return nil, error
    }
    return user, nil
}

// Bad
func GetUser(uid string) (*User, error) {
    u, err := db.Find(uid)
    if err != nil {
        return nil, err
    }
    return u, nil
}
```

## Constants

Use `SCREAMING_SNAKE_CASE` for all constants, exported or not:

```go
const MAX_RETRIES = 3
const DEFAULT_TIMEOUT = 30
const ERROR_INVALID_FORMAT = "invalid format"
```

## Exported Functions/Methods

Use `PascalCase`:

```go
func CreateNewUser(name string) *User
func GetActiveLicense() (*License, error)
func ParseConfig(path string) (*Config, error)
```

## Internal (Unexported) Functions

Use `snake_case`:

```go
func download_tarball(url string) (string, error)
func extract_docs(path string) error
func validate_input(data []byte) bool
```

## Parameters and Local Variables

Use `snake_case`:

```go
func CreateUser(user_name string, is_admin bool) *User {
    user_id := generate_id()
    created_at := time.Now()
    return &User{
        id:   user_id,
        name: user_name,
    }
}
```

## Struct Fields

Use `PascalCase` for exported fields (required for JSON, etc):

```go
type User struct {
    ID        string    `json:"id"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
}
```

## Package-Level Variables

Use `snake_case`:

```go
var default_client = &http.Client{}
var error_cache = make(map[string]error)
```

## Error Variables

Use `SCREAMING_SNAKE_CASE` (they are constants):

```go
var ERROR_NOT_FOUND = errors.New("not found")
var ERROR_INVALID_INPUT = errors.New("invalid input")
```

## No Comments

Never add comments to Go code. No doc comments, no inline comments, no commented-out code. The code should speak for itself.
