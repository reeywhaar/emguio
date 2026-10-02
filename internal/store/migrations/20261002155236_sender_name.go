package migrations

// The name mail sent from an email config goes out under, beside its address.
var senderName = Migration{
	Name: "20261002155236_sender_name",
	Up:   exec(`ALTER TABLE email_configs ADD COLUMN sender_name TEXT NOT NULL DEFAULT '';`),
}
