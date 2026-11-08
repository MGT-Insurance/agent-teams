package dispatcher

import "time"

// SetClientTimeout shortens the request timeout and returns a restore func.
func SetClientTimeout(d time.Duration) func() {
	old := clientTimeout
	clientTimeout = d
	return func() { clientTimeout = old }
}
