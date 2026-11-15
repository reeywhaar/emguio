package migrations

// Whether an email config is turned off: kept, with its passwords, but neither read nor told
// about. See docs/email-configs.md.
var emailConfigOff = Migration{
	Name: "20261115120000_email_config_off",
	Up:   exec(`ALTER TABLE email_configs ADD COLUMN off INTEGER NOT NULL DEFAULT 0;`),
}
