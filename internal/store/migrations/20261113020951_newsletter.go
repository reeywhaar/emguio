package migrations

// Whether a kept message says how to unsubscribe. Null for one kept before this was read, which
// the next pass fetches again.
var newsletter = Migration{
	Name: "20261113020951_newsletter",
	Up:   exec(`ALTER TABLE messages ADD COLUMN newsletter INTEGER;`),
}
