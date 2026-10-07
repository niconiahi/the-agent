package session

// DEFAULT_CEILING is the token ceiling of a session when none is configured.
const DEFAULT_CEILING = 200_000

// Ceiling is the token ceiling for a session: configured, or DEFAULT_CEILING
// when configured is not positive, and never above the model's context window.
func Ceiling(configured int, context_window int) int {
	ceiling := configured
	if ceiling <= 0 {
		ceiling = DEFAULT_CEILING
	}
	return min(ceiling, context_window)
}
