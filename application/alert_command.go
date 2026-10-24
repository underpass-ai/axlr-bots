package application

import "time"

// DefaultAlertWindow groups the failures of one unit: alerts for the
// same unit within the window name the same task.
const DefaultAlertWindow = 10 * time.Minute

// AlertCommand says a systemd unit failed on this host and asks the bot
// for a diagnosis. The idempotency key comes from the unit and the
// window the failure falls in, so a flapping unit does not create a task
// per restart.
type AlertCommand struct {
	BotID string
	// Unit is the systemd unit that failed; a name without a type suffix
	// is a .service.
	Unit string
	// Host names the machine in the objective; empty means the hostname
	// is unknown to the caller.
	Host string
	// At is when the failure was seen; zero means now.
	At time.Time
	// Window groups failures; zero means DefaultAlertWindow.
	Window time.Duration
	// UserManager says the unit belongs to a systemd --user manager, so
	// the bot must query it with --user.
	UserManager bool
	// CorrelationID ties the task to upstream work, if any.
	CorrelationID string
}
