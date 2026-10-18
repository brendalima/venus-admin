package domain

// SiteStatus is the state of the public site as the admin shows it next
// to the publish button.
type SiteStatus struct {
	// Pending reports that the catalogue changed after the last publish.
	Pending bool
	// CanPublish is false until the site's deploy hook is configured: the
	// button would otherwise promise an update that never happens.
	CanPublish bool
}
