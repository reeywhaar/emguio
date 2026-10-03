package migrations

// The folder a user chose for archiving one of their email configs to, where the server names
// none or another is wanted. Null is the one the server names.
var archiveMailbox = Migration{
	Name: "20261003023553_archive_mailbox",
	Up:   exec(`ALTER TABLE email_configs ADD COLUMN archive_mailbox TEXT REFERENCES mailboxes (id) ON DELETE SET NULL;`),
}
