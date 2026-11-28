package migrations

// The address a session was last used from, for its user to recognize it by. Empty for one from
// before. See docs/api.md.
var sessionIP = Migration{
	Name: "20261128095114_session_ip",
	Up: exec(`
ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';
`),
}
