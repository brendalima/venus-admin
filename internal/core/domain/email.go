package domain

// Email is a message to one person, in plain text: the admin only sends
// short transactional mail, which reads fine everywhere without HTML.
type Email struct {
	To      string
	Subject string
	Text    string
}
