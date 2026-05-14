package notify

// Notifier sends native OS notifications to the user.
type Notifier interface {
	// Send displays a notification with the given title and body.
	Send(title, body string)
}
