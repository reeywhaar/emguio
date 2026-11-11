package migrations

// Where a user put an email config among their others. Null is never placed: after the placed
// ones, oldest first.
var emailConfigPosition = Migration{
	Name: "20261111022909_email_config_position",
	Up:   exec(`ALTER TABLE email_configs ADD COLUMN position INTEGER;`),
}
